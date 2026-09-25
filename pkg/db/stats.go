package db

import (
	"context"
	"database/sql"
	"fmt"
)

// DayCount is a per-calendar-day counter (day = YYYY-MM-DD, UTC).
type DayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// DayMs is a per-calendar-day duration total in milliseconds.
type DayMs struct {
	Day string `json:"day"`
	Ms  int64  `json:"ms"`
}

// UserStat aggregates work-session effort per user.
type UserStat struct {
	Username  string `json:"username"`
	Sessions  int    `json:"sessions"`
	ClockedMs int64  `json:"clockedMs"`
	Completed int    `json:"completed"`
	Active    int    `json:"active"`
}

// CompletedPerDay counts orders that reached Completed (audit trail) since sinceMs.
func (s *Store) CompletedPerDay(ctx context.Context, sinceMs int64) ([]DayCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(timestamp/1000, 'unixepoch') AS d, COUNT(*)
		  FROM audit_logs
		 WHERE action LIKE 'transition % -> Completed' AND timestamp >= ?
		 GROUP BY d ORDER BY d`, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("completed per day: %w", err)
	}
	defer rows.Close()
	out := []DayCount{}
	for rows.Next() {
		var dc DayCount
		if err := rows.Scan(&dc.Day, &dc.Count); err != nil {
			return nil, fmt.Errorf("scan completed per day: %w", err)
		}
		out = append(out, dc)
	}
	return out, rows.Err()
}

// CreatedPerDay counts orders created since sinceMs.
func (s *Store) CreatedPerDay(ctx context.Context, sinceMs int64) ([]DayCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(created_at/1000, 'unixepoch') AS d, COUNT(*)
		  FROM orders
		 WHERE created_at >= ?
		 GROUP BY d ORDER BY d`, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("created per day: %w", err)
	}
	defer rows.Close()
	out := []DayCount{}
	for rows.Next() {
		var dc DayCount
		if err := rows.Scan(&dc.Day, &dc.Count); err != nil {
			return nil, fmt.Errorf("scan created per day: %w", err)
		}
		out = append(out, dc)
	}
	return out, rows.Err()
}

// ClockedPerDay sums ended work-session time per day the session ended.
func (s *Store) ClockedPerDay(ctx context.Context, sinceMs int64) ([]DayMs, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(ended_at/1000, 'unixepoch') AS d, SUM(ended_at - started_at)
		  FROM work_sessions
		 WHERE ended_at IS NOT NULL AND ended_at >= ?
		 GROUP BY d ORDER BY d`, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("clocked per day: %w", err)
	}
	defer rows.Close()
	out := []DayMs{}
	for rows.Next() {
		var dm DayMs
		if err := rows.Scan(&dm.Day, &dm.Ms); err != nil {
			return nil, fmt.Errorf("scan clocked per day: %w", err)
		}
		out = append(out, dm)
	}
	return out, rows.Err()
}

// HandleAvgMs returns the average total clocked time per order (ended sessions
// only) plus the number of orders that have any ended session.
func (s *Store) HandleAvgMs(ctx context.Context) (int64, int, error) {
	var avg sql.NullFloat64
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT AVG(total), COUNT(*) FROM (
			SELECT SUM(ended_at - started_at) AS total
			  FROM work_sessions
			 WHERE ended_at IS NOT NULL
			 GROUP BY order_id
		)`).Scan(&avg, &n)
	if err != nil {
		return 0, 0, fmt.Errorf("handle average: %w", err)
	}
	if !avg.Valid {
		return 0, 0, nil
	}
	return int64(avg.Float64), int(n.Int64), nil
}

// Leaderboard aggregates session effort and completions per user since sinceMs.
func (s *Store) Leaderboard(ctx context.Context, sinceMs int64) ([]UserStat, error) {
	byUser := map[string]*UserStat{}
	get := func(u string) *UserStat {
		if st, ok := byUser[u]; ok {
			return st
		}
		st := &UserStat{Username: u}
		byUser[u] = st
		return st
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT username, COUNT(*), COALESCE(SUM(ended_at - started_at), 0)
		  FROM work_sessions
		 WHERE started_at >= ?
		 GROUP BY username`, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("leaderboard sessions: %w", err)
	}
	for rows.Next() {
		var u string
		var cnt int
		var ms int64
		if err := rows.Scan(&u, &cnt, &ms); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan leaderboard sessions: %w", err)
		}
		st := get(u)
		st.Sessions = cnt
		st.ClockedMs = ms
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT username, COUNT(*) FROM work_sessions
		 WHERE ended_at IS NULL GROUP BY username`)
	if err != nil {
		return nil, fmt.Errorf("leaderboard active: %w", err)
	}
	for rows.Next() {
		var u string
		var cnt int
		if err := rows.Scan(&u, &cnt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan leaderboard active: %w", err)
		}
		get(u).Active = cnt
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT performed_by, COUNT(*) FROM audit_logs
		 WHERE action LIKE 'transition % -> Completed' AND timestamp >= ?
		 GROUP BY performed_by`, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("leaderboard completed: %w", err)
	}
	for rows.Next() {
		var u string
		var cnt int
		if err := rows.Scan(&u, &cnt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan leaderboard completed: %w", err)
		}
		get(u).Completed = cnt
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]UserStat, 0, len(byUser))
	for _, st := range byUser {
		out = append(out, *st)
	}
	return out, nil
}
