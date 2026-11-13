package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cockpit/pkg/order"
)

var ErrWorkSessionTaken = errors.New("work session already active for another user")

type WorkSession struct {
	ID        int64  `json:"id"`
	OrderID   int64  `json:"orderId"`
	Username  string `json:"username"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   *int64 `json:"endedAt,omitempty"`
	Step      int    `json:"step"`
	Checks    string `json:"checks"`
	Completed bool   `json:"completed"`
}

func scanWorkSession(row interface {
	Scan(dest ...any) error
}) (*WorkSession, error) {
	var (
		ws        WorkSession
		endedAt   sql.NullInt64
		completed int
	)
	err := row.Scan(&ws.ID, &ws.OrderID, &ws.Username, &ws.StartedAt, &endedAt, &ws.Step, &ws.Checks, &completed)
	if err != nil {
		return nil, err
	}
	if endedAt.Valid {
		v := endedAt.Int64
		ws.EndedAt = &v
	}
	ws.Completed = completed != 0
	return &ws, nil
}

const workSessionCols = `id, order_id, username, started_at, ended_at, step, checks, completed`

func (s *Store) ActiveWorkSession(ctx context.Context, orderID int64) (*WorkSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+workSessionCols+` FROM work_sessions
		 WHERE order_id = ? AND ended_at IS NULL
		 ORDER BY id DESC LIMIT 1`, orderID)
	ws, err := scanWorkSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ws, nil
}

func (s *Store) LatestWorkSession(ctx context.Context, orderID int64) (*WorkSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+workSessionCols+` FROM work_sessions
		 WHERE order_id = ?
		 ORDER BY id DESC LIMIT 1`, orderID)
	ws, err := scanWorkSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ws, nil
}

func (s *Store) StartWorkSession(ctx context.Context, orderID int64, username string, now time.Time) (*WorkSession, error) {
	active, err := s.ActiveWorkSession(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		if active.Username != username {
			return nil, ErrWorkSessionTaken
		}
		return active, nil
	}
	ms := now.UTC().UnixMilli()

	step, checks := 0, "{}"
	var prevStep int
	var prevChecks string
	// First try the current user's own previous incomplete session on this order
	err = s.db.QueryRowContext(ctx,
		`SELECT step, checks FROM work_sessions
		 WHERE order_id = ? AND username = ? AND ended_at IS NOT NULL AND completed = 0
		 ORDER BY id DESC LIMIT 1`, orderID, username).Scan(&prevStep, &prevChecks)
	if err == nil {
		step, checks = prevStep, prevChecks
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	} else {
		err = s.db.QueryRowContext(ctx,
			`SELECT step, checks FROM work_sessions
			 WHERE order_id = ? AND ended_at IS NOT NULL AND completed = 0
			 ORDER BY id DESC LIMIT 1`, orderID).Scan(&prevStep, &prevChecks)
		if err == nil {
			step, checks = prevStep, prevChecks
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO work_sessions (order_id, username, started_at, step, checks, completed)
		 VALUES (?, ?, ?, ?, ?, 0)`, orderID, username, ms, step, checks)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &WorkSession{ID: id, OrderID: orderID, Username: username, StartedAt: ms, Step: step, Checks: checks}, nil
}

func (s *Store) ActiveSessionsFor(ctx context.Context, username string) ([]*WorkSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+workSessionCols+` FROM work_sessions
		 WHERE username = ? AND ended_at IS NULL
		 ORDER BY started_at ASC`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WorkSession{}
	for rows.Next() {
		ws, err := scanWorkSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (s *Store) UpdateWorkSession(ctx context.Context, orderID int64, username string, step int, checks string) (*WorkSession, error) {
	active, err := s.ActiveWorkSession(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, sql.ErrNoRows
	}
	if active.Username != username {
		return nil, ErrWorkSessionTaken
	}
	if step < 0 {
		step = 0
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE work_sessions SET step = ?, checks = ? WHERE id = ?`, step, checks, active.ID); err != nil {
		return nil, err
	}
	active.Step = step
	active.Checks = checks
	return active, nil
}

func (s *Store) EndWorkSession(ctx context.Context, orderID int64, username string, now time.Time, completed bool) (*WorkSession, error) {
	active, err := s.ActiveWorkSession(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, sql.ErrNoRows
	}
	if active.Username != username {
		return nil, ErrWorkSessionTaken
	}
	ms := now.UTC().UnixMilli()
	c := 0
	if completed {
		c = 1
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE work_sessions SET ended_at = ?, completed = ? WHERE id = ?`, ms, c, active.ID); err != nil {
		return nil, err
	}
	active.EndedAt = &ms
	active.Completed = completed
	return active, nil
}

func (s *Store) ActiveSessionsAll(ctx context.Context) ([]*WorkSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+workSessionCols+` FROM work_sessions
		WHERE ended_at IS NULL
		ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WorkSession{}
	for rows.Next() {
		ws, err := scanWorkSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (s *Store) GetWorkedByUsers(ctx context.Context, orderID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT username FROM work_sessions
			WHERE order_id = ? AND username IS NOT NULL AND username != ''
			ORDER BY id ASC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) RecordWorkflowStepResult(ctx context.Context, req order.WorkflowStepResultRequest, now time.Time) (*order.WorkflowStepResult, error) {
	ms := now.UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_step_results (order_id, step_key, step_index, performed_by, correct, customer_impact, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.OrderID, req.StepKey, req.StepIndex, req.PerformedBy, boolInt(req.Correct), boolInt(req.CustomerImpact), req.Notes, ms)
	if err != nil {
		return nil, fmt.Errorf("insert workflow step result: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return &order.WorkflowStepResult{
		ID:            id,
		OrderID:       req.OrderID,
		StepKey:       req.StepKey,
		StepIndex:     req.StepIndex,
		PerformedBy:   req.PerformedBy,
		Correct:       req.Correct,
		CustomerImpact: req.CustomerImpact,
		Notes:         req.Notes,
		CreatedAt:     now,
	}, nil
}

func (s *Store) GetWorkflowStepResults(ctx context.Context, orderID int64) ([]order.WorkflowStepResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, step_key, step_index, performed_by, correct, customer_impact, notes, created_at
		 FROM workflow_step_results
		 WHERE order_id = ?
		 ORDER BY step_index ASC, created_at ASC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query workflow step results: %w", err)
	}
	defer rows.Close()
	out := []order.WorkflowStepResult{}
	for rows.Next() {
		var r order.WorkflowStepResult
		var correct, ci int
		var created int64
		if err := rows.Scan(&r.ID, &r.OrderID, &r.StepKey, &r.StepIndex, &r.PerformedBy, &correct, &ci, &r.Notes, &created); err != nil {
			return nil, fmt.Errorf("scan workflow step result: %w", err)
		}
		r.Correct = correct != 0
		r.CustomerImpact = ci != 0
		r.CreatedAt = fromMillis(created)
		out = append(out, r)
	}
	return out, rows.Err()
}
