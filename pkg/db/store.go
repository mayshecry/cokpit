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

const orderCols = `id, order_number, status, target_completion_at, created_at, updated_at, assigned_to, paused_seconds`

func scanOrder(row *sql.Row) (order.Order, error) {
	var o order.Order
	var tgt, created, updated int64
	err := row.Scan(&o.ID, &o.OrderNumber, &o.Status, &tgt, &created, &updated, &o.Assignee, &o.PausedSeconds)
	if err != nil {
		return order.Order{}, err
	}
	o.TargetCompletion = fromMillis(tgt)
	o.CreatedAt = fromMillis(created)
	o.UpdatedAt = fromMillis(updated)
	return o, nil
}

func (s *Store) CreateOrder(ctx context.Context, number string, target time.Time, now time.Time, performedBy string) (order.Order, error) {
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
		`INSERT INTO orders (order_number, status, target_completion_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		number, string(order.StatusReceived), millis(target), millis(now), millis(now))
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
		ID:               id,
		OrderNumber:      number,
		Status:           order.StatusReceived,
		TargetCompletion: target.UTC(),
		CreatedAt:        now.UTC(),
		UpdatedAt:        now.UTC(),
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

func (s *Store) SubmitQC(ctx context.Context, id int64, status order.QCStatus, inspector, notes, performedBy string, now time.Time) (order.QCCheck, error) {
	if performedBy == "" {
		performedBy = inspector
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.QCCheck{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var currentStatus order.Status
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM orders WHERE id = ?`, id).Scan(&currentStatus)
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
		`INSERT INTO qc_checks (order_id, status, inspector_id, notes, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, string(status), inspector, notes, millis(now))
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
		var o order.Order
		var tgt, created, updated int64
		if err := rows.Scan(&o.ID, &o.OrderNumber, &o.Status, &tgt, &created, &updated, &o.Assignee, &o.PausedSeconds); err != nil {
			return nil, 0, fmt.Errorf("scan order: %w", err)
		}
		o.TargetCompletion = fromMillis(tgt)
		o.CreatedAt = fromMillis(created)
		o.UpdatedAt = fromMillis(updated)
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
