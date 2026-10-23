package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"cockpit/pkg/capa"
)

const capaCols = `c.id, c.order_id, o.order_number, c.qc_check_id, c.title, c.fail_reason,
	c.owner, c.due_at, c.root_cause, c.action_plan, c.resolution, c.status, c.created_by,
	c.created_at, c.updated_at, c.verified_by, c.verified_at, c.verification_note`

func scanCAPA(row interface{ Scan(...any) error }) (capa.CorrectiveAction, error) {
	var a capa.CorrectiveAction
	var qcID, verifiedAt sql.NullInt64
	var due, created, updated int64
	if err := row.Scan(&a.ID, &a.OrderID, &a.OrderNumber, &qcID, &a.Title, &a.FailReason,
		&a.Owner, &due, &a.RootCause, &a.ActionPlan, &a.Resolution, &a.Status, &a.CreatedBy,
		&created, &updated, &a.VerifiedBy, &verifiedAt, &a.VerificationNote); err != nil {
		return capa.CorrectiveAction{}, err
	}
	if qcID.Valid {
		a.QCCheckID = &qcID.Int64
	}
	a.DueAt = fromMillis(due)
	a.CreatedAt = fromMillis(created)
	a.UpdatedAt = fromMillis(updated)
	a.VerifiedAt = nullableTime(verifiedAt)
	return a, nil
}

func scanCAPAEvents(rows *sql.Rows) ([]capa.Event, error) {
	out := []capa.Event{}
	for rows.Next() {
		var e capa.Event
		var created int64
		if err := rows.Scan(&e.ID, &e.CAPAID, &e.Action, &e.Note, &e.PerformedBy, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = fromMillis(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetCAPA(ctx context.Context, id int64) (capa.CorrectiveAction, error) {
	a, err := scanCAPA(s.db.QueryRowContext(ctx, `
		SELECT `+capaCols+` FROM corrective_actions c
		JOIN orders o ON o.id = c.order_id WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return capa.CorrectiveAction{}, ErrNotFound
	}
	return a, err
}

func getCAPATx(ctx context.Context, tx *sql.Tx, id int64) (capa.CorrectiveAction, error) {
	a, err := scanCAPA(tx.QueryRowContext(ctx, `
		SELECT `+capaCols+` FROM corrective_actions c
		JOIN orders o ON o.id = c.order_id WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return capa.CorrectiveAction{}, ErrNotFound
	}
	return a, err
}

func addCAPAEventTx(ctx context.Context, tx *sql.Tx, id int64, action, note, performedBy string, now time.Time) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO capa_events (capa_id, action, note, performed_by, created_at)
		VALUES (?, ?, ?, ?, ?)`, id, action, note, performedBy, millis(now))
	if err != nil {
		return 0, fmt.Errorf("insert CAPA event: %w", err)
	}
	return res.LastInsertId()
}

func notifyCAPATx(ctx context.Context, tx *sql.Tx, a capa.CorrectiveAction, recipient, body string, eventID int64, now time.Time) error {
	if recipient == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO notifications (user_name, kind, order_id, order_number, ref_id, body, created_at)
		VALUES (?, 'capa', ?, ?, ?, ?, ?)`,
		recipient, a.OrderID, a.OrderNumber, eventID, body, millis(now))
	if err != nil {
		return fmt.Errorf("insert CAPA notification: %w", err)
	}
	return nil
}

func auditCAPATx(ctx context.Context, tx *sql.Tx, a capa.CorrectiveAction, action, performedBy string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		a.OrderID, fmt.Sprintf("CAPA-%d %s", a.ID, action), performedBy, millis(now))
	return err
}

func userExistsTx(ctx context.Context, tx *sql.Tx, username string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: %s", ErrUnknownAssignee, username)
	}
	return nil
}

func (s *Store) CreateCAPA(ctx context.Context, req capa.CreateRequest, createdBy string, now time.Time) (capa.CorrectiveAction, error) {
	req.Title = strings.TrimSpace(req.Title)
	req.FailReason = strings.TrimSpace(req.FailReason)
	req.Owner = strings.TrimSpace(req.Owner)
	req.RootCause = strings.TrimSpace(req.RootCause)
	req.ActionPlan = strings.TrimSpace(req.ActionPlan)
	req.Resolution = strings.TrimSpace(req.Resolution)
	if err := capa.ValidateCreate(req, now); err != nil {
		return capa.CorrectiveAction{}, err
	}
	if createdBy == "" {
		createdBy = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	defer tx.Rollback()

	var orderNumber string
	if err := tx.QueryRowContext(ctx, `SELECT order_number FROM orders WHERE id = ?`, req.OrderID).Scan(&orderNumber); errors.Is(err, sql.ErrNoRows) {
		return capa.CorrectiveAction{}, ErrNotFound
	} else if err != nil {
		return capa.CorrectiveAction{}, err
	}
	if req.QCCheckID != nil {
		var checkOrderID int64
		var status, failReason string
		err := tx.QueryRowContext(ctx, `SELECT order_id, status, fail_reason FROM qc_checks WHERE id = ?`, *req.QCCheckID).
			Scan(&checkOrderID, &status, &failReason)
		if errors.Is(err, sql.ErrNoRows) {
			return capa.CorrectiveAction{}, fmt.Errorf("%w: QC check not found", capa.ErrValidation)
		}
		if err != nil {
			return capa.CorrectiveAction{}, err
		}
		if checkOrderID != req.OrderID || status != string(orderQCFail) {
			return capa.CorrectiveAction{}, fmt.Errorf("%w: qcCheckId must reference a FAIL check on the same order", capa.ErrValidation)
		}
		if req.FailReason == "" {
			req.FailReason = failReason
		}
	}
	if err := userExistsTx(ctx, tx, req.Owner); err != nil {
		return capa.CorrectiveAction{}, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO corrective_actions (order_id, qc_check_id, title, fail_reason, owner, due_at,
		root_cause, action_plan, resolution, status, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'OPEN', ?, ?, ?)`,
		req.OrderID, req.QCCheckID, req.Title, req.FailReason, req.Owner, millis(req.DueAt),
		req.RootCause, req.ActionPlan, req.Resolution, createdBy, millis(now), millis(now))
	if err != nil {
		return capa.CorrectiveAction{}, fmt.Errorf("insert CAPA: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	a := capa.CorrectiveAction{
		ID: id, OrderID: req.OrderID, OrderNumber: orderNumber, QCCheckID: req.QCCheckID,
		Title: req.Title, FailReason: req.FailReason, Owner: req.Owner, DueAt: req.DueAt.UTC(),
		RootCause: req.RootCause, ActionPlan: req.ActionPlan, Resolution: req.Resolution,
		Status: capa.StatusOpen, CreatedBy: createdBy, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	eventID, err := addCAPAEventTx(ctx, tx, id, "created", req.Title, createdBy, now)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	if req.Owner != createdBy {
		body := fmt.Sprintf("CAPA-%d assigned to you: %s (%s)", id, req.Title, orderNumber)
		if err := notifyCAPATx(ctx, tx, a, req.Owner, body, eventID, now); err != nil {
			return capa.CorrectiveAction{}, err
		}
	}
	if err := auditCAPATx(ctx, tx, a, "created: "+req.Title, createdBy, now); err != nil {
		return capa.CorrectiveAction{}, err
	}
	if err := tx.Commit(); err != nil {
		return capa.CorrectiveAction{}, err
	}
	return s.GetCAPA(ctx, id)
}

const orderQCFail = "FAIL"

func (s *Store) UpdateCAPA(ctx context.Context, id int64, req capa.UpdateRequest, performedBy string, now time.Time) (capa.CorrectiveAction, error) {
	if performedBy == "" {
		performedBy = "system"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	defer tx.Rollback()
	current, err := getCAPATx(ctx, tx, id)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	if current.Status == capa.StatusClosed {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: closed actions are immutable", capa.ErrInvalidTransition)
	}
	updated := current
	if req.Title != nil {
		updated.Title = strings.TrimSpace(*req.Title)
	}
	if req.FailReason != nil {
		updated.FailReason = strings.TrimSpace(*req.FailReason)
	}
	if req.Owner != nil {
		updated.Owner = strings.TrimSpace(*req.Owner)
	}
	if req.DueAt != nil {
		updated.DueAt = req.DueAt.UTC()
	}
	if req.RootCause != nil {
		updated.RootCause = strings.TrimSpace(*req.RootCause)
	}
	if req.ActionPlan != nil {
		updated.ActionPlan = strings.TrimSpace(*req.ActionPlan)
	}
	if req.Resolution != nil {
		updated.Resolution = strings.TrimSpace(*req.Resolution)
	}
	if req.Status != nil {
		updated.Status = *req.Status
	}
	if updated.Title == "" || len(updated.Title) > 160 {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: title is required and must be at most 160 characters", capa.ErrValidation)
	}
	if updated.Owner == "" {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: owner is required", capa.ErrValidation)
	}
	if updated.DueAt.IsZero() {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: dueAt is required", capa.ErrValidation)
	}
	if !capa.IsValidStatus(updated.Status) || updated.Status == capa.StatusClosed {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: use the verification endpoint to close an action", capa.ErrInvalidTransition)
	}
	if !capa.CanTransition(current.Status, updated.Status) {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: %s -> %s", capa.ErrInvalidTransition, current.Status, updated.Status)
	}
	if updated.Status == capa.StatusPendingVerification {
		if err := capa.ValidateReadyForVerification(updated); err != nil {
			return capa.CorrectiveAction{}, err
		}
	}
	if len(updated.RootCause) > 4000 || len(updated.ActionPlan) > 4000 || len(updated.Resolution) > 4000 {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: text fields must be at most 4000 characters", capa.ErrValidation)
	}
	if updated.Owner != current.Owner {
		if err := userExistsTx(ctx, tx, updated.Owner); err != nil {
			return capa.CorrectiveAction{}, err
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE corrective_actions SET title = ?, fail_reason = ?, owner = ?, due_at = ?, root_cause = ?,
		action_plan = ?, resolution = ?, status = ?, updated_at = ? WHERE id = ?`,
		updated.Title, updated.FailReason, updated.Owner, millis(updated.DueAt), updated.RootCause,
		updated.ActionPlan, updated.Resolution, updated.Status, millis(now), id)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	updated.UpdatedAt = now.UTC()
	statusChanged := current.Status != updated.Status
	eventAction := "updated details"
	if statusChanged {
		eventAction = fmt.Sprintf("status %s -> %s", current.Status, updated.Status)
	}
	eventID, err := addCAPAEventTx(ctx, tx, id, eventAction, "", performedBy, now)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	if updated.Owner != current.Owner && updated.Owner != performedBy {
		body := fmt.Sprintf("CAPA-%d reassigned to you: %s (%s)", id, updated.Title, updated.OrderNumber)
		if err := notifyCAPATx(ctx, tx, updated, updated.Owner, body, eventID, now); err != nil {
			return capa.CorrectiveAction{}, err
		}
	}
	if statusChanged && updated.Status == capa.StatusPendingVerification && updated.CreatedBy != performedBy {
		body := fmt.Sprintf("CAPA-%d is ready for verification: %s (%s)", id, updated.Title, updated.OrderNumber)
		if err := notifyCAPATx(ctx, tx, updated, updated.CreatedBy, body, eventID, now); err != nil {
			return capa.CorrectiveAction{}, err
		}
	}
	if err := auditCAPATx(ctx, tx, updated, eventAction, performedBy, now); err != nil {
		return capa.CorrectiveAction{}, err
	}
	if err := tx.Commit(); err != nil {
		return capa.CorrectiveAction{}, err
	}
	return s.GetCAPA(ctx, id)
}

func (s *Store) VerifyCAPA(ctx context.Context, id int64, note, verifiedBy string, now time.Time) (capa.CorrectiveAction, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: verification note is required", capa.ErrValidation)
	}
	if len(note) > 4000 {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: verification note must be at most 4000 characters", capa.ErrValidation)
	}
	if verifiedBy == "" {
		verifiedBy = "system"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	defer tx.Rollback()
	current, err := getCAPATx(ctx, tx, id)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	if current.Status != capa.StatusPendingVerification {
		return capa.CorrectiveAction{}, fmt.Errorf("%w: only actions pending verification can be closed", capa.ErrInvalidTransition)
	}
	verifiedAt := now.UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE corrective_actions SET status = 'CLOSED', verified_by = ?, verified_at = ?,
		verification_note = ?, updated_at = ? WHERE id = ?`,
		verifiedBy, millis(now), note, millis(now), id); err != nil {
		return capa.CorrectiveAction{}, err
	}
	updated := current
	updated.Status = capa.StatusClosed
	updated.VerifiedBy = verifiedBy
	updated.VerifiedAt = &verifiedAt
	updated.VerificationNote = note
	updated.UpdatedAt = verifiedAt
	eventID, err := addCAPAEventTx(ctx, tx, id, "verified and closed", note, verifiedBy, now)
	if err != nil {
		return capa.CorrectiveAction{}, err
	}
	if updated.Owner != verifiedBy {
		body := fmt.Sprintf("CAPA-%d verified and closed: %s (%s)", id, updated.Title, updated.OrderNumber)
		if err := notifyCAPATx(ctx, tx, updated, updated.Owner, body, eventID, now); err != nil {
			return capa.CorrectiveAction{}, err
		}
	}
	if err := auditCAPATx(ctx, tx, updated, "verified and closed", verifiedBy, now); err != nil {
		return capa.CorrectiveAction{}, err
	}
	if err := tx.Commit(); err != nil {
		return capa.CorrectiveAction{}, err
	}
	return s.GetCAPA(ctx, id)
}

type CAPAFilter struct {
	Status      *capa.Status
	Owner       string
	OverdueOnly bool
	Limit       int
}

func (s *Store) ListCAPA(ctx context.Context, filter CAPAFilter, now time.Time) ([]capa.CorrectiveAction, error) {
	where := []string{}
	args := []any{}
	if filter.Status != nil {
		where = append(where, "c.status = ?")
		args = append(args, *filter.Status)
	}
	if filter.Owner != "" {
		where = append(where, "c.owner = ?")
		args = append(args, filter.Owner)
	}
	if filter.OverdueOnly {
		where = append(where, "c.status != 'CLOSED' AND c.due_at < ?")
		args = append(args, millis(now))
	}
	query := `SELECT ` + capaCols + ` FROM corrective_actions c JOIN orders o ON o.id = c.order_id`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY CASE WHEN c.status = 'CLOSED' THEN 1 ELSE 0 END, c.due_at ASC, c.id DESC`
	if filter.Limit > 0 {
		if filter.Limit > 500 {
			filter.Limit = 500
		}
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []capa.CorrectiveAction{}
	for rows.Next() {
		a, err := scanCAPA(rows)
		if err != nil {
			return nil, err
		}
		a.Overdue = a.Status != capa.StatusClosed && a.DueAt.Before(now)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CAPAEvents(ctx context.Context, id int64) ([]capa.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, capa_id, action, note, performed_by, created_at FROM capa_events
		WHERE capa_id = ? ORDER BY created_at ASC, id ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCAPAEvents(rows)
}

func (s *Store) CAPASummary(ctx context.Context, now time.Time) (capa.Summary, error) {
	var summary capa.Summary
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM corrective_actions GROUP BY status`)
	if err != nil {
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var status capa.Status
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return summary, err
		}
		switch status {
		case capa.StatusOpen:
			summary.Open = count
		case capa.StatusInProgress:
			summary.InProgress = count
		case capa.StatusPendingVerification:
			summary.PendingVerification = count
		case capa.StatusClosed:
			summary.Closed = count
		}
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}
	if err := rows.Close(); err != nil {
		return summary, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM corrective_actions WHERE status != 'CLOSED' AND due_at < ?`, millis(now)).Scan(&summary.Overdue); err != nil {
		return summary, err
	}
	return summary, nil
}
