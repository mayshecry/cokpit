package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"cockpit/pkg/order"
)

var ErrNotFound = errors.New("record not found")

var ErrUnknownDepartment = errors.New("target department does not exist")

var ErrDuplicateOrderNumber = errors.New("order number already exists")

var ErrHoldActive = errors.New("order already has an active hold")

var ErrHoldResolved = errors.New("hold is already resolved")

var ErrNoHold = errors.New("order has no active hold")

var ErrStaleWrite = errors.New("order was modified since you loaded it; refresh and try again")

type Store struct {
	db *sql.DB
}

func New(conn *sql.DB) *Store { return &Store{db: conn} }

func (s *Store) DB() *sql.DB { return s.db }

const orderCols = `id, order_number, status, target_completion_at, created_at, updated_at, assigned_to, paused_seconds,
		debit_number, customer_name, omnitracker_ticket, device, asset_number, configuration,
		COALESCE(NULLIF(workflow_profile_id, ''), 0) AS workflow_profile_id, workflow_profile_name, si_id, barcode`

type orderScanner interface {
	Scan(dest ...any) error
}

func orderScanDest(o *order.Order, tgt, created, updated *int64, siID, wpID *sql.NullInt64) []any {
	return []any{
		&o.ID,
		&o.OrderNumber,
		&o.Status,
		tgt,
		created,
		updated,
		&o.Assignee,
		&o.PausedSeconds,
		&o.DebitNumber,
		&o.CustomerName,
		&o.OmnitrackerTicket,
		&o.Device,
		&o.AssetNumber,
		&o.Configuration,
		wpID,
		&o.WorkflowProfileName,
		siID,
		&o.Barcode,
	}
}

func scanOrderRow(sc orderScanner) (order.Order, error) {
	var o order.Order
	var tgt, created, updated int64
	var siID, wpID sql.NullInt64
	err := sc.Scan(orderScanDest(&o, &tgt, &created, &updated, &siID, &wpID)...)
	if err != nil {
		return order.Order{}, err
	}
	o.TargetCompletion = fromMillis(tgt)
	o.CreatedAt = fromMillis(created)
	o.UpdatedAt = fromMillis(updated)
	if siID.Valid {
		o.SIID = &siID.Int64
	}
	if wpID.Valid {
		o.WorkflowProfileID = wpID.Int64
	}
	return o, nil
}

func scanOrder(row *sql.Row) (order.Order, error) {
	return scanOrderRow(row)
}

var ErrSchemaMismatch = errors.New("database schema does not match this build")

func orderScanCount() int {
	var probe order.Order
	var tgt, created, updated int64
	var siID, wpID sql.NullInt64
	return len(orderScanDest(&probe, &tgt, &created, &updated, &siID, &wpID))
}

func countOrderColumns(ctx context.Context, db *sql.DB) (int, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+orderCols+" FROM orders LIMIT 0")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	return len(cols), nil
}

func (s *Store) VerifyOrderColumns(ctx context.Context) error {
	scanned := orderScanCount()
	rows, err := s.db.QueryContext(ctx, "SELECT "+orderCols+" FROM orders LIMIT 0")
	if err != nil {
		return fmt.Errorf("%w: running the order query failed: %v", ErrSchemaMismatch, err)
	}
	cols, err := rows.Columns()
	rows.Close()
	if err != nil {
		return fmt.Errorf("%w: reading order columns failed: %v", ErrSchemaMismatch, err)
	}
	if selected := len(cols); scanned != selected {
		return fmt.Errorf("%w: the order query selects %d columns but the scanner reads %d",
			ErrSchemaMismatch, selected, scanned)
	}
	return nil
}

func (s *Store) CreateOrder(ctx context.Context, number string, target time.Time, now time.Time, performedBy string) (order.Order, error) {
	return s.CreateOrderWithWorkflow(ctx, number, target, now, performedBy, "", 0, "")
}

func (s *Store) CreateOrderWithWorkflow(ctx context.Context, number string, target time.Time, now time.Time, performedBy, customerName string, profileID int64, profileName string) (order.Order, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Order{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if performedBy == "" {
		performedBy = "system"
	}

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE order_number = ?`, number).Scan(&exists)
	if err != nil {
		return order.Order{}, fmt.Errorf("check duplicate: %w", err)
	}
	if exists > 0 {
		return order.Order{}, ErrDuplicateOrderNumber
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO orders (order_number, status, target_completion_at, created_at, updated_at, customer_name, workflow_profile_id, workflow_profile_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		number, string(order.StatusReceived), millis(target), millis(now), millis(now), customerName, profileID, profileName)
	if err != nil {
		return order.Order{}, fmt.Errorf("insert order: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return order.Order{}, fmt.Errorf("last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		id, "created order "+number, performedBy, millis(now)); err != nil {
		return order.Order{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.Order{}, fmt.Errorf("commit: %w", err)
	}

	return order.Order{
		ID:                  id,
		OrderNumber:         number,
		Status:              order.StatusReceived,
		TargetCompletion:    target.UTC(),
		CreatedAt:           now.UTC(),
		UpdatedAt:           now.UTC(),
		CustomerName:        customerName,
		WorkflowProfileID:   profileID,
		WorkflowProfileName: profileName,
	}, nil
}

func (s *Store) GetOrder(ctx context.Context, id int64) (order.Order, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE id = ?`, id)
	o, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("get order: %w", err)
	}
	return o, nil
}

func (s *Store) ListOrders(ctx context.Context, status *order.Status) ([]order.Order, error) {
	orders, _, err := s.ListOrdersFiltered(ctx, ListQuery{Status: status})
	return orders, err
}

var ErrNoQCPass = errors.New("order has no passing QC check; cannot complete")

func stringToStatus(s string) order.Status { return order.Status(s) }

func (s *Store) TransitionOrder(ctx context.Context, id int64, target string, performedBy string, now time.Time) (order.Order, error) {
	return s.TransitionOrderGuarded(ctx, id, target, performedBy, nil, now)
}

func (s *Store) TransitionOrderGuarded(ctx context.Context, id int64, target string, performedBy string, expectedUpdatedAt *time.Time, now time.Time) (order.Order, error) {
	if performedBy == "" {
		performedBy = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Order{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	current, err := scanOrder(tx.QueryRowContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("get order: %w", err)
	}

	if current.Status == order.StatusCompleted {
		return order.Order{}, order.ErrCompletedTransition
	}

	if expectedUpdatedAt != nil && !current.UpdatedAt.Equal(*expectedUpdatedAt) {
		return order.Order{}, ErrStaleWrite
	}

	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM holds WHERE order_id = ? AND resolved_at IS NULL`, id).Scan(&active); err != nil {
		return order.Order{}, fmt.Errorf("count active holds: %w", err)
	}
	if active > 0 {
		return order.Order{}, fmt.Errorf("%w: %v", order.ErrActiveHold, current.Status)
	}

	targetStatus := stringToStatus(target)
	newStatus, err := order.DetermineTransition(current.Status, false, targetStatus)
	if err != nil {
		return order.Order{}, err
	}

	if newStatus == order.StatusCompleted {
		var latest string
		err := tx.QueryRowContext(ctx,
			`SELECT status FROM qc_checks WHERE order_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, id).Scan(&latest)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && latest != string(order.QCPass)) {
			return order.Order{}, ErrNoQCPass
		}
		if err != nil {
			return order.Order{}, fmt.Errorf("read latest qc: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE orders SET status = ?, updated_at = ? WHERE id = ?`,
		string(newStatus), millis(now), id); err != nil {
		return order.Order{}, fmt.Errorf("update order: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		id, "transition "+string(current.Status)+" -> "+string(newStatus), performedBy, millis(now)); err != nil {
		return order.Order{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.Order{}, fmt.Errorf("commit: %w", err)
	}

	current.Status = newStatus
	current.UpdatedAt = now.UTC()
	return current, nil
}

func (s *Store) PlaceHold(ctx context.Context, id int64, reason, createdBy string, now time.Time) (order.Hold, order.Order, error) {
	return s.PlaceHoldGuarded(ctx, id, reason, createdBy, nil, now)
}

func (s *Store) PlaceHoldGuarded(ctx context.Context, id int64, reason, createdBy string, expectedUpdatedAt *time.Time, now time.Time) (order.Hold, order.Order, error) {
	if createdBy == "" {
		createdBy = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	current, err := scanOrder(tx.QueryRowContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return order.Hold{}, order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("get order: %w", err)
	}

	base := order.NormalizeHeld(current.Status)
	if current.Status != base {
		return order.Hold{}, order.Order{}, ErrHoldActive
	}
	if expectedUpdatedAt != nil && !current.UpdatedAt.Equal(*expectedUpdatedAt) {
		return order.Hold{}, order.Order{}, ErrStaleWrite
	}
	if base == order.StatusCompleted {
		return order.Hold{}, order.Order{}, order.ErrCompletedTransition
	}

	held := order.Status(order.StatusOnHoldPrefix + string(base))
	if _, err := tx.ExecContext(ctx,
		`UPDATE orders SET status = ?, updated_at = ? WHERE id = ?`,
		string(held), millis(now), id); err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("update order: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO holds (order_id, reason, created_by, created_at) VALUES (?, ?, ?, ?)`,
		id, reason, createdBy, millis(now))
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("insert hold: %w", err)
	}
	holdID, err := res.LastInsertId()
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		id, "hold placed: "+reason, createdBy, millis(now)); err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("commit: %w", err)
	}

	return order.Hold{
			ID:        holdID,
			OrderID:   id,
			Reason:    reason,
			CreatedBy: createdBy,
			CreatedAt: now.UTC(),
		}, order.Order{
			ID:               current.ID,
			OrderNumber:      current.OrderNumber,
			Status:           held,
			TargetCompletion: current.TargetCompletion,
			CreatedAt:        current.CreatedAt,
			UpdatedAt:        now.UTC(),
		}, nil
}

func (s *Store) ResolveHold(ctx context.Context, holdID int64, resolvedBy string, now time.Time) (order.Hold, order.Order, error) {
	if resolvedBy == "" {
		resolvedBy = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var orderID int64
	var reason, createdBy string
	var holdCreated int64
	var resolvedAt sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT order_id, reason, created_by, created_at, resolved_at FROM holds WHERE id = ?`, holdID).
		Scan(&orderID, &reason, &createdBy, &holdCreated, &resolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Hold{}, order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("get hold: %w", err)
	}
	if resolvedAt.Valid {
		return order.Hold{}, order.Order{}, ErrHoldResolved
	}

	var heldStatus order.Status
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM orders WHERE id = ?`, orderID).Scan(&heldStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Hold{}, order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("get order: %w", err)
	}

	base := order.NormalizeHeld(heldStatus)
	if _, err := tx.ExecContext(ctx,
		`UPDATE orders SET status = ?, updated_at = ?, paused_seconds = paused_seconds + ?
		 WHERE id = ?`,
		string(base), millis(now), (now.UnixMilli()-holdCreated)/1000, orderID); err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("update order: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE holds SET resolved_at = ? WHERE id = ?`, millis(now), holdID); err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("resolve hold: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, "hold resolved", resolvedBy, millis(now)); err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("commit: %w", err)
	}

	restored, err := s.GetOrder(context.Background(), orderID)
	if err != nil {
		return order.Hold{}, order.Order{}, fmt.Errorf("reload order: %w", err)
	}

	resolved := now.UTC()
	return order.Hold{
		ID:         holdID,
		OrderID:    orderID,
		Reason:     reason,
		CreatedBy:  createdBy,
		ResolvedAt: &resolved,
		CreatedAt:  fromMillis(holdCreated),
	}, restored, nil
}

func (s *Store) SubmitQC(ctx context.Context, id int64, status order.QCStatus, inspector, notes, failReason, performedBy string, now time.Time) (order.QCCheck, error) {
	if performedBy == "" {
		performedBy = inspector
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.QCCheck{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var currentStatus order.Status
	var tech string
	err = tx.QueryRowContext(ctx,
		`SELECT status, assigned_to FROM orders WHERE id = ?`, id).Scan(&currentStatus, &tech)
	if errors.Is(err, sql.ErrNoRows) {
		return order.QCCheck{}, ErrNotFound
	}
	if err != nil {
		return order.QCCheck{}, fmt.Errorf("get order: %w", err)
	}
	if currentStatus == order.StatusCompleted {
		return order.QCCheck{}, order.ErrCompletedTransition
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO qc_checks (order_id, status, inspector_id, notes, created_at, tech, fail_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, string(status), inspector, notes, millis(now), tech, failReason)
	if err != nil {
		return order.QCCheck{}, fmt.Errorf("insert qc: %w", err)
	}
	checkID, err := res.LastInsertId()
	if err != nil {
		return order.QCCheck{}, fmt.Errorf("last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		id, "qc submitted: "+string(status), performedBy, millis(now)); err != nil {
		return order.QCCheck{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.QCCheck{}, fmt.Errorf("commit: %w", err)
	}

	return order.QCCheck{
		ID:          checkID,
		OrderID:     id,
		Status:      status,
		InspectorID: inspector,
		Notes:       notes,
		CreatedAt:   now.UTC(),
	}, nil
}

func (s *Store) ActiveHolds(ctx context.Context, orderID int64) ([]order.Hold, error) {
	return s.holds(ctx, orderID, true)
}

func (s *Store) Holds(ctx context.Context, orderID int64) ([]order.Hold, error) {
	return s.holds(ctx, orderID, false)
}

func (s *Store) holds(ctx context.Context, orderID int64, activeOnly bool) ([]order.Hold, error) {
	query := `SELECT id, order_id, reason, created_by, resolved_at, created_at FROM holds WHERE order_id = ?`
	if activeOnly {
		query += ` AND resolved_at IS NULL`
	}
	query += ` ORDER BY created_at DESC, id DESC`

	rows, err := s.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("list holds: %w", err)
	}
	defer rows.Close()

	holds := []order.Hold{}
	for rows.Next() {
		var h order.Hold
		var resolvedAt sql.NullInt64
		var created int64
		if err := rows.Scan(&h.ID, &h.OrderID, &h.Reason, &h.CreatedBy, &resolvedAt, &created); err != nil {
			return nil, fmt.Errorf("scan hold: %w", err)
		}
		h.ResolvedAt = nullableTime(resolvedAt)
		h.CreatedAt = fromMillis(created)
		holds = append(holds, h)
	}
	return holds, rows.Err()
}

func (s *Store) QCChecks(ctx context.Context, orderID int64) ([]order.QCCheck, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, status, inspector_id, notes, created_at
		 FROM qc_checks WHERE order_id = ? ORDER BY created_at DESC, id DESC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list qc checks: %w", err)
	}
	defer rows.Close()

	checks := []order.QCCheck{}
	for rows.Next() {
		var c order.QCCheck
		var created int64
		if err := rows.Scan(&c.ID, &c.OrderID, &c.Status, &c.InspectorID, &c.Notes, &created); err != nil {
			return nil, fmt.Errorf("scan qc: %w", err)
		}
		c.CreatedAt = fromMillis(created)
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

func (s *Store) AuditLog(ctx context.Context, orderID int64) ([]order.AuditLog, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE id = ?`, orderID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check order: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, action, performed_by, timestamp
		 FROM audit_logs WHERE order_id = ? ORDER BY timestamp DESC, id DESC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	logs := []order.AuditLog{}
	for rows.Next() {
		var l order.AuditLog
		var ts int64
		if err := rows.Scan(&l.ID, &l.OrderID, &l.Action, &l.PerformedBy, &ts); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		l.Timestamp = fromMillis(ts)
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

type ListQuery struct {
	Status         *order.Status
	Assignee       string
	OnlyUnassigned bool
	Limit          int
	Offset         int
}

func (s *Store) ListOrdersFiltered(ctx context.Context, q ListQuery) ([]order.Order, int64, error) {
	where := []string{}
	args := []any{}

	if q.Status != nil && *q.Status != "" {
		variants := order.ExpandHeld(*q.Status)
		placeholders := make([]string, len(variants))
		for i, v := range variants {
			placeholders[i] = "?"
			args = append(args, string(v))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	switch {
	case q.OnlyUnassigned:
		where = append(where, "assigned_to = ''")
	case q.Assignee != "":
		where = append(where, "assigned_to = ?")
		args = append(args, q.Assignee)
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}

	query := `SELECT ` + orderCols + ` FROM orders` + whereSQL +
		` ORDER BY created_at DESC, id DESC`
	if q.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", q.Limit, q.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	orders := []order.Order{}
	for rows.Next() {
		o, err := scanOrderRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan order: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate orders: %w", err)
	}
	return orders, total, nil
}

func (s *Store) SetAssignee(ctx context.Context, id int64, assignee, performedBy string, now time.Time) (order.Order, error) {
	if performedBy == "" {
		performedBy = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Order{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	current, err := scanOrder(tx.QueryRowContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("get order: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE orders SET assigned_to = ?, updated_at = ? WHERE id = ?`,
		assignee, millis(now), id); err != nil {
		return order.Order{}, fmt.Errorf("update order: %w", err)
	}
	action := "unassigned"
	if assignee != "" {
		action = "assigned to " + assignee
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		id, action, performedBy, millis(now)); err != nil {
		return order.Order{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.Order{}, fmt.Errorf("commit: %w", err)
	}

	current.Assignee = assignee
	current.UpdatedAt = now.UTC()
	return current, nil
}

func (s *Store) ActiveHoldStarts(ctx context.Context) (map[int64]time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT order_id, MIN(created_at) FROM holds WHERE resolved_at IS NULL GROUP BY order_id`)
	if err != nil {
		return nil, fmt.Errorf("active hold starts: %w", err)
	}
	defer rows.Close()

	out := map[int64]time.Time{}
	for rows.Next() {
		var id, ms int64
		if err := rows.Scan(&id, &ms); err != nil {
			return nil, fmt.Errorf("scan hold start: %w", err)
		}
		out[id] = fromMillis(ms)
	}
	return out, rows.Err()
}

func (s *Store) UpdateOmnitrackerInfo(ctx context.Context, id int64, req order.OmnitrackerInfoRequest, now time.Time) (order.Order, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Order{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var siID sql.NullInt64
	if req.SIID != nil {
		siID = sql.NullInt64{Int64: *req.SIID, Valid: true}
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE orders SET debit_number = ?, customer_name = ?, omnitracker_ticket = ?, device = ?, asset_number = ?, configuration = ?, si_id = ?, updated_at = ? WHERE id = ?`,
		req.DebitNumber, req.CustomerName, req.OmnitrackerTicket, req.Device, req.AssetNumber, req.Configuration, siID, millis(now), id)
	if err != nil {
		return order.Order{}, fmt.Errorf("update omnitracker info: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.Order{}, fmt.Errorf("commit: %w", err)
	}

	return s.GetOrder(ctx, id)
}

func (s *Store) GenerateBarcode(ctx context.Context, id int64, regenerate bool, now time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var existing string
	err = tx.QueryRowContext(ctx, `SELECT barcode FROM orders WHERE id = ?`, id).Scan(&existing)
	if err != nil {
		return "", fmt.Errorf("check barcode: %w", err)
	}
	if existing != "" && !regenerate {
		return existing, nil
	}

	barcode := fmt.Sprintf("CPT-%d-%d", id, now.UnixNano()%1000000)

	_, err = tx.ExecContext(ctx,
		`UPDATE orders SET barcode = ?, updated_at = ? WHERE id = ?`,
		barcode, millis(now), id)
	if err != nil {
		return "", fmt.Errorf("set barcode: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	return barcode, nil
}

func (s *Store) OrderByBarcode(ctx context.Context, barcode string) (order.Order, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE barcode = ?`, barcode)
	o, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("get order by barcode: %w", err)
	}
	return o, nil
}

func (s *Store) RecordBarcodeScan(ctx context.Context, orderID int64, barcode, scannedBy, scanType, deviceInfo string, now time.Time) (order.BarcodeScanEvent, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO barcode_scans (order_id, barcode, scanned_by, scan_type, device_info, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		orderID, barcode, scannedBy, scanType, deviceInfo, millis(now))
	if err != nil {
		return order.BarcodeScanEvent{}, fmt.Errorf("insert scan: %w", err)
	}
	id, _ := res.LastInsertId()
	return order.BarcodeScanEvent{
		ID:         id,
		OrderID:    orderID,
		Barcode:    barcode,
		ScannedBy:  scannedBy,
		ScanType:   scanType,
		DeviceInfo: deviceInfo,
		CreatedAt:  now.UTC(),
	}, nil
}

func (s *Store) BarcodeScanHistory(ctx context.Context, orderID int64) ([]order.BarcodeScanEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, barcode, scanned_by, scan_type, device_info, created_at FROM barcode_scans WHERE order_id = ? ORDER BY created_at DESC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query scans: %w", err)
	}
	defer rows.Close()

	var events []order.BarcodeScanEvent
	for rows.Next() {
		var e order.BarcodeScanEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.OrderID, &e.Barcode, &e.ScannedBy, &e.ScanType, &e.DeviceInfo, &created); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.CreatedAt = fromMillis(created)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) CreateOrderSplit(ctx context.Context, originalOrderID int64, req order.SplitRequest, now time.Time) (order.OrderSplit, error) {
	if req.RequestedBy == "" {
		req.RequestedBy = "system"
	}
	if req.TargetUser == "" {
		return order.OrderSplit{}, fmt.Errorf("target user is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, req.TargetUser).Scan(&exists)
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("check target user: %w", err)
	}
	if exists == 0 {
		return order.OrderSplit{}, fmt.Errorf("target user %q does not exist", req.TargetUser)
	}

	var origStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, originalOrderID).Scan(&origStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return order.OrderSplit{}, ErrNotFound
	}
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("get original order: %w", err)
	}
	if origStatus == string(order.StatusCompleted) {
		return order.OrderSplit{}, fmt.Errorf("cannot split a completed order")
	}

	var origProfileID sql.NullInt64
	var origProfileName string
	var origTargetAt int64
	err = tx.QueryRowContext(ctx,
		`SELECT workflow_profile_id, workflow_profile_name, target_completion_at FROM orders WHERE id = ?`,
		originalOrderID).Scan(&origProfileID, &origProfileName, &origTargetAt)
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("get original order profile: %w", err)
	}
	var origProfileIDInt int64
	if origProfileID.Valid {
		origProfileIDInt = origProfileID.Int64
	}

	ms := now.UTC().UnixMilli()
	baseNumber := fmt.Sprintf("%d-SPLIT-%d", originalOrderID, now.UnixNano())

	var splitID int64
	for i := 0; i < 5; i++ {
		candidate := baseNumber
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", baseNumber, i)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO orders (order_number, status, target_completion_at, created_at, updated_at, assigned_to, split_from_order_id, split_reason, workflow_profile_id, workflow_profile_name)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			candidate, string(order.StatusReceived), origTargetAt, ms, ms, ms, req.TargetUser, originalOrderID, req.Reason, origProfileIDInt, origProfileName)
		if err == nil {
			splitID, err = res.LastInsertId()
			if err != nil {
				return order.OrderSplit{}, fmt.Errorf("last insert id: %w", err)
			}
			break
		}
		if i == 4 {
			return order.OrderSplit{}, fmt.Errorf("insert split order: %w", err)
		}
	}
	if splitID == 0 {
		return order.OrderSplit{}, fmt.Errorf("insert split order: unable to generate unique order number")
	}

	// Copy workflow progress: latest work session step/checks to the split order (target user)
	var prevStep int
	var prevChecks string
	err = tx.QueryRowContext(ctx,
		`SELECT step, checks FROM work_sessions
		WHERE order_id = ? AND ended_at IS NOT NULL AND completed = 0
		ORDER BY id DESC LIMIT 1`, originalOrderID).Scan(&prevStep, &prevChecks)
	if err == nil {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO work_sessions (order_id, username, started_at, step, checks, completed)
			VALUES (?, ?, ?, ?, ?, 0)`,
			splitID, req.TargetUser, ms, prevStep, prevChecks)
		if err != nil {
			return order.OrderSplit{}, fmt.Errorf("copy work session: %w", err)
		}
	}

	// Copy checklist items from original to split order (unchecked, unassigned)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO checklist_items (order_id, seq, artikel, omschrijving, locatie, aantal, checked_by, checked_at, created_at)
		SELECT ?, seq, artikel, omschrijving, locatie, aantal, NULL, NULL, created_at
		FROM checklist_items WHERE order_id = ? ORDER BY seq ASC, id ASC`,
		splitID, originalOrderID)
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("copy checklist items: %w", err)
	}

	// Copy order manuals and their blocks from original to split order
	manualRows, err := tx.QueryContext(ctx,
		`SELECT id, product_id, product_code, product_name, created_by, created_at FROM order_manuals WHERE order_id = ? ORDER BY id ASC`, originalOrderID)
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("query order manuals: %w", err)
	}
	type manualCopy struct {
		id          int64
		productID   int
		productCode string
		productName string
		createdBy   string
		createdAt   int64
	}
	var manuals []manualCopy
	for manualRows.Next() {
		var m manualCopy
		if err := manualRows.Scan(&m.id, &m.productID, &m.productCode, &m.productName, &m.createdBy, &m.createdAt); err != nil {
			manualRows.Close()
			return order.OrderSplit{}, fmt.Errorf("scan order manual: %w", err)
		}
		manuals = append(manuals, m)
	}
	manualRows.Close()

	for _, m := range manuals {
		res2, err := tx.ExecContext(ctx,
			`INSERT INTO order_manuals (order_id, product_id, product_code, product_name, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			splitID, m.productID, m.productCode, m.productName, req.TargetUser, m.createdAt)
		if err != nil {
			return order.OrderSplit{}, fmt.Errorf("insert order manual: %w", err)
		}
		newManualID, _ := res2.LastInsertId()

		blockRows, err := tx.QueryContext(ctx,
			`SELECT seq, title, body, assignee, answer, answered_by, answered_at, flagged, flagged_by, flagged_at, flag_reason, created_at FROM order_manual_blocks WHERE manual_id = ? ORDER BY seq ASC`, m.id)
		if err != nil {
			return order.OrderSplit{}, fmt.Errorf("query manual blocks: %w", err)
		}
		type blockCopy struct {
			seq         int
			title       string
			body        string
			assignee    string
			answer      string
			answeredBy  string
			answeredAt  int64
			flagged     int
			flaggedBy   string
			flaggedAt   int64
			flagReason  string
			createdAt   int64
		}
		var blocks []blockCopy
		for blockRows.Next() {
			var b blockCopy
			if err := blockRows.Scan(&b.seq, &b.title, &b.body, &b.assignee, &b.answer, &b.answeredBy, &b.answeredAt, &b.flagged, &b.flaggedBy, &b.flaggedAt, &b.flagReason, &b.createdAt); err != nil {
				blockRows.Close()
				return order.OrderSplit{}, fmt.Errorf("scan manual block: %w", err)
			}
			blocks = append(blocks, b)
		}
		blockRows.Close()

		for _, b := range blocks {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO order_manual_blocks (manual_id, seq, title, body, assignee, answer, answered_by, answered_at, flagged, flagged_by, flagged_at, flag_reason, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				newManualID, b.seq, b.title, b.body, b.assignee, b.answer, b.answeredBy, b.answeredAt, b.flagged, b.flaggedBy, b.flaggedAt, b.flagReason, b.createdAt)
			if err != nil {
				return order.OrderSplit{}, fmt.Errorf("insert manual block: %w", err)
			}
		}
	}

	res2, err := tx.ExecContext(ctx,
		`INSERT INTO order_splits (original_order_id, requested_by, reason, status, created_at)
		VALUES (?, ?, ?, 'pending', ?)`,
		originalOrderID, req.RequestedBy, req.Reason, millis(now))
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("insert split record: %w", err)
	}
	splitRecordID, _ := res2.LastInsertId()

	if err := tx.Commit(); err != nil {
		return order.OrderSplit{}, fmt.Errorf("commit: %w", err)
	}

	return order.OrderSplit{
		ID:              splitRecordID,
		OriginalOrderID: originalOrderID,
		SplitOrderID:    splitID,
		RequestedBy:     req.RequestedBy,
		Status:          "pending",
		Reason:          req.Reason,
		CreatedAt:       now.UTC(),
	}, nil
}

func (s *Store) AcceptOrderSplit(ctx context.Context, splitID int64, req order.SplitAcceptRequest, now time.Time) (order.OrderSplit, error) {
	if req.AcceptedBy == "" {
		req.AcceptedBy = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var originalOrderID, splitOrderID int64
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT original_order_id, split_order_id, status FROM order_splits WHERE id = ?`, splitID).Scan(&originalOrderID, &splitOrderID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return order.OrderSplit{}, ErrNotFound
	}
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("get split: %w", err)
	}
	if status != "pending" {
		return order.OrderSplit{}, fmt.Errorf("split is already %s", status)
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE order_splits SET accepted_by = ?, status = 'accepted', accepted_at = ? WHERE id = ?`,
		req.AcceptedBy, millis(now), splitID)
	if err != nil {
		return order.OrderSplit{}, fmt.Errorf("update split: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE orders SET assigned_to = ?, updated_at = ? WHERE id = ?`,
		req.AcceptedBy, millis(now), splitOrderID); err != nil {
		return order.OrderSplit{}, fmt.Errorf("assign split order: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		splitOrderID, "accepted split from order "+fmt.Sprintf("%d", originalOrderID), req.AcceptedBy, millis(now)); err != nil {
		return order.OrderSplit{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.OrderSplit{}, fmt.Errorf("commit: %w", err)
	}

	return order.OrderSplit{
		ID:              splitID,
		OriginalOrderID: originalOrderID,
		SplitOrderID:    splitOrderID,
		AcceptedBy:      req.AcceptedBy,
		Status:          "accepted",
		AcceptedAt:      &now,
	}, nil
}

func (s *Store) ListOrderSplits(ctx context.Context, status string) ([]order.OrderSplit, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, original_order_id, split_order_id, requested_by, accepted_by, status, reason, created_at, accepted_at FROM order_splits WHERE status = ? ORDER BY created_at DESC`, status)
	if err != nil {
		return nil, fmt.Errorf("query splits: %w", err)
	}
	defer rows.Close()

	var splits []order.OrderSplit
	for rows.Next() {
		var s order.OrderSplit
		var acceptedAt sql.NullInt64
		var created int64
		if err := rows.Scan(&s.ID, &s.OriginalOrderID, &s.SplitOrderID, &s.RequestedBy, &s.AcceptedBy, &s.Status, &s.Reason, &created, &acceptedAt); err != nil {
			return nil, fmt.Errorf("scan split: %w", err)
		}
		s.CreatedAt = fromMillis(created)
		if acceptedAt.Valid {
			t := fromMillis(acceptedAt.Int64)
			s.AcceptedAt = &t
		}
		splits = append(splits, s)
	}
	return splits, rows.Err()
}

func (s *Store) GetOrderSplits(ctx context.Context, orderID int64) ([]order.OrderSplit, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, original_order_id, split_order_id, requested_by, accepted_by, status, reason, created_at, accepted_at FROM order_splits WHERE original_order_id = ? OR split_order_id = ? ORDER BY created_at DESC`, orderID, orderID)
	if err != nil {
		return nil, fmt.Errorf("query order splits: %w", err)
	}
	defer rows.Close()

	var splits []order.OrderSplit
	for rows.Next() {
		var s order.OrderSplit
		var acceptedAt sql.NullInt64
		var created int64
		if err := rows.Scan(&s.ID, &s.OriginalOrderID, &s.SplitOrderID, &s.RequestedBy, &s.AcceptedBy, &s.Status, &s.Reason, &created, &acceptedAt); err != nil {
			return nil, fmt.Errorf("scan split: %w", err)
		}
		s.CreatedAt = fromMillis(created)
		if acceptedAt.Valid {
			t := fromMillis(acceptedAt.Int64)
			s.AcceptedAt = &t
		}
		splits = append(splits, s)
	}
	return splits, rows.Err()
}

func (s *Store) RecordWorkflowStepResult(ctx context.Context, req order.WorkflowStepResultRequest, now time.Time) (order.WorkflowStepResult, error) {
	if req.PerformedBy == "" {
		req.PerformedBy = "system"
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_step_results (order_id, step_key, step_index, performed_by, correct, customer_impact, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.OrderID, req.StepKey, req.StepIndex, req.PerformedBy, boolInt(req.Correct), boolInt(req.CustomerImpact), req.Notes, millis(now))
	if err != nil {
		return order.WorkflowStepResult{}, fmt.Errorf("insert step result: %w", err)
	}
	id, _ := res.LastInsertId()

	return order.WorkflowStepResult{
		ID:            id,
		OrderID:       req.OrderID,
		StepKey:       req.StepKey,
		StepIndex:     req.StepIndex,
		PerformedBy:   req.PerformedBy,
		Correct:       req.Correct,
		CustomerImpact: req.CustomerImpact,
		Notes:         req.Notes,
		CreatedAt:     now.UTC(),
	}, nil
}

func (s *Store) GetWorkflowStepResults(ctx context.Context, orderID int64) ([]order.WorkflowStepResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, step_key, step_index, performed_by, correct, customer_impact, notes, created_at FROM workflow_step_results WHERE order_id = ? ORDER BY step_index, created_at`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query step results: %w", err)
	}
	defer rows.Close()

	var results []order.WorkflowStepResult
	for rows.Next() {
		var r order.WorkflowStepResult
		var created int64
		var correct, ci int
		if err := rows.Scan(&r.ID, &r.OrderID, &r.StepKey, &r.StepIndex, &r.PerformedBy, &correct, &ci, &r.Notes, &created); err != nil {
			return nil, fmt.Errorf("scan step result: %w", err)
		}
		r.Correct = correct == 1
		r.CustomerImpact = ci == 1
		r.CreatedAt = fromMillis(created)
		results = append(results, r)
	}
	return results, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
