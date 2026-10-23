package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
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
	err = s.db.QueryRowContext(ctx,
		`SELECT step, checks FROM work_sessions
		 WHERE order_id = ? AND username = ? AND ended_at IS NOT NULL AND completed = 0
		 ORDER BY id DESC LIMIT 1`, orderID, username).Scan(&prevStep, &prevChecks)
	if err == nil {
		step, checks = prevStep, prevChecks
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
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
