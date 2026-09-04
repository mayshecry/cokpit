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

const notificationSnippetLen = 120

func (s *Store) AllActiveHolds(ctx context.Context) ([]order.Hold, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, reason, created_by, resolved_at, created_at
		 FROM holds WHERE resolved_at IS NULL ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list active holds: %w", err)
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

func (s *Store) AddComment(ctx context.Context, orderID int64, authorID, authorName, body string, mentions []string, now time.Time) (order.Comment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Comment{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var number string
	err = tx.QueryRowContext(ctx, `SELECT order_number FROM orders WHERE id = ?`, orderID).Scan(&number)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Comment{}, ErrNotFound
	}
	if err != nil {
		return order.Comment{}, fmt.Errorf("check order: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO comments (order_id, author_id, author_name, body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		orderID, authorID, authorName, body, millis(now))
	if err != nil {
		return order.Comment{}, fmt.Errorf("insert comment: %w", err)
	}
	commentID, err := res.LastInsertId()
	if err != nil {
		return order.Comment{}, fmt.Errorf("last insert id: %w", err)
	}

	snippet := body
	if len(snippet) > notificationSnippetLen {
		snippet = snippet[:notificationSnippetLen]
	}

	notified := []string{}
	for _, m := range mentions {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO comment_mentions (comment_id, username) VALUES (?, ?)`,
			commentID, m); err != nil {
			return order.Comment{}, fmt.Errorf("insert mention: %w", err)
		}
		if m == authorID {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO notifications (user_name, kind, order_id, order_number, ref_id, body, created_at)
			 VALUES (?, 'mention', ?, ?, ?, ?, ?)`,
			m, orderID, number, commentID, snippet, millis(now)); err != nil {
			return order.Comment{}, fmt.Errorf("insert notification: %w", err)
		}
		notified = append(notified, m)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, "comment added", authorID, millis(now)); err != nil {
		return order.Comment{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.Comment{}, fmt.Errorf("commit: %w", err)
	}

	return order.Comment{
		ID:         commentID,
		OrderID:    orderID,
		AuthorID:   authorID,
		AuthorName: authorName,
		Body:       body,
		Mentions:   notified,
		CreatedAt:  now.UTC(),
	}, nil
}

func (s *Store) Comments(ctx context.Context, orderID int64) ([]order.Comment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, author_id, author_name, body, created_at
		 FROM comments WHERE order_id = ? ORDER BY created_at ASC, id ASC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()

	comments := []order.Comment{}
	for rows.Next() {
		var c order.Comment
		var created int64
		if err := rows.Scan(&c.ID, &c.OrderID, &c.AuthorID, &c.AuthorName, &c.Body, &created); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		c.CreatedAt = fromMillis(created)
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comments: %w", err)
	}
	if len(comments) == 0 {
		return comments, nil
	}

	ids := make([]string, 0, len(comments))
	for _, c := range comments {
		ids = append(ids, fmt.Sprintf("%d", c.ID))
	}
	mrows, err := s.db.QueryContext(ctx,
		`SELECT comment_id, username FROM comment_mentions
		 WHERE comment_id IN (`+strings.Join(ids, ",")+`)
		 ORDER BY username ASC`)
	if err != nil {
		return nil, fmt.Errorf("list mentions: %w", err)
	}
	defer mrows.Close()

	byComment := make(map[int64][]string)
	for mrows.Next() {
		var refID int64
		var user string
		if err := mrows.Scan(&refID, &user); err != nil {
			return nil, fmt.Errorf("scan mention: %w", err)
		}
		byComment[refID] = append(byComment[refID], user)
	}
	if err := mrows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mentions: %w", err)
	}
	for i := range comments {
		comments[i].Mentions = byComment[comments[i].ID]
		if comments[i].Mentions == nil {
			comments[i].Mentions = []string{}
		}
	}
	return comments, nil
}

func (s *Store) RecordScan(ctx context.Context, orderID int64, code, scannedBy, action string, now time.Time) (order.ScanEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.ScanEvent{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE id = ?`, orderID).Scan(&exists)
	if err != nil {
		return order.ScanEvent{}, fmt.Errorf("check order: %w", err)
	}
	if exists == 0 {
		return order.ScanEvent{}, ErrNotFound
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO scan_events (order_id, code, scanned_by, action, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		orderID, code, scannedBy, action, millis(now))
	if err != nil {
		return order.ScanEvent{}, fmt.Errorf("insert scan: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return order.ScanEvent{}, fmt.Errorf("last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, "barcode scanned: "+code, scannedBy, millis(now)); err != nil {
		return order.ScanEvent{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.ScanEvent{}, fmt.Errorf("commit: %w", err)
	}

	return order.ScanEvent{
		ID:        id,
		OrderID:   orderID,
		Code:      code,
		ScannedBy: scannedBy,
		Action:    action,
		CreatedAt: now.UTC(),
	}, nil
}

func (s *Store) ScanEvents(ctx context.Context, orderID int64) ([]order.ScanEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, order_id, code, scanned_by, action, created_at
		 FROM scan_events WHERE order_id = ? ORDER BY created_at DESC, id DESC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list scan events: %w", err)
	}
	defer rows.Close()

	events := []order.ScanEvent{}
	for rows.Next() {
		var e order.ScanEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.OrderID, &e.Code, &e.ScannedBy, &e.Action, &created); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.CreatedAt = fromMillis(created)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) OrderByNumber(ctx context.Context, number string) (order.Order, error) {
	o, err := scanOrder(s.db.QueryRowContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE order_number = ?`, number))
	if errors.Is(err, sql.ErrNoRows) {
		return order.Order{}, ErrNotFound
	}
	if err != nil {
		return order.Order{}, fmt.Errorf("order by number: %w", err)
	}
	return o, nil
}

func (s *Store) Notify(ctx context.Context, userName, kind string, orderID int64, orderNumber string, refID int64, body string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO notifications (user_name, kind, order_id, order_number, ref_id, body, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userName, kind, orderID, orderNumber, refID, body, millis(now))
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

func (s *Store) Notifications(ctx context.Context, userName string, unreadOnly bool, limit int) ([]order.Notification, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT id, user_name, kind, order_id, order_number, ref_id, body, read_at, created_at
		  FROM notifications WHERE user_name = ?`
	if unreadOnly {
		query += ` AND read_at IS NULL`
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, userName, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	notes := []order.Notification{}
	for rows.Next() {
		var n order.Notification
		var readAt sql.NullInt64
		var created int64
		if err := rows.Scan(&n.ID, &n.UserName, &n.Kind, &n.OrderID, &n.OrderNumber, &n.RefID, &n.Body, &readAt, &created); err != nil {
			return nil, 0, fmt.Errorf("scan notification: %w", err)
		}
		n.ReadAt = nullableTime(readAt)
		n.CreatedAt = fromMillis(created)
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate notifications: %w", err)
	}

	var unread int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_name = ? AND read_at IS NULL`, userName).Scan(&unread); err != nil {
		return nil, 0, fmt.Errorf("count unread: %w", err)
	}
	return notes, unread, nil
}

func (s *Store) MarkNotificationsRead(ctx context.Context, userName string, ids []int64, now time.Time) (int64, error) {
	query := `UPDATE notifications SET read_at = ? WHERE user_name = ? AND read_at IS NULL`
	args := []any{millis(now), userName}
	if len(ids) > 0 {
		ph := make([]string, 0, len(ids))
		for _, id := range ids {
			ph = append(ph, fmt.Sprintf("%d", id))
			args = append(args, id)
		}
		query += ` AND id IN (` + strings.Join(ph, ",") + `)`
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mark read: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
