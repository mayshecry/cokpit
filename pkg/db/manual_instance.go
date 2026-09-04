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

func (s *Store) InstantiateManual(ctx context.Context, orderID, productID int64, createdBy string, now time.Time) (order.OrderManual, error) {
	if createdBy == "" {
		createdBy = "system"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.OrderManual{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var orderExists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE id = ?`, orderID).Scan(&orderExists); err != nil {
		return order.OrderManual{}, fmt.Errorf("check order: %w", err)
	}
	if orderExists == 0 {
		return order.OrderManual{}, ErrNotFound
	}

	var prod order.Product
	var pc, pn, pd, pby string
	var created, updated int64
	err = tx.QueryRowContext(ctx,
		`SELECT id, code, name, description, created_by, created_at, updated_at FROM products WHERE id = ?`, productID).
		Scan(&prod.ID, &pc, &pn, &pd, &pby, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return order.OrderManual{}, ErrNotFound
	}
	if err != nil {
		return order.OrderManual{}, fmt.Errorf("get product: %w", err)
	}
	prod.Code, prod.Name = pc, pn

	var dup int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM order_manuals WHERE order_id = ? AND product_id = ?`, orderID, productID).Scan(&dup); err != nil {
		return order.OrderManual{}, fmt.Errorf("check duplicate manual: %w", err)
	}
	if dup > 0 {
		return order.OrderManual{}, ErrDuplicateManual
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO order_manuals (order_id, product_id, product_code, product_name, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		orderID, productID, prod.Code, prod.Name, createdBy, millis(now))
	if err != nil {
		return order.OrderManual{}, fmt.Errorf("insert order manual: %w", err)
	}
	manualID, err := res.LastInsertId()
	if err != nil {
		return order.OrderManual{}, fmt.Errorf("last insert id: %w", err)
	}

	blocks, err := manualBlocksTx(ctx, tx, productID)
	if err != nil {
		return order.OrderManual{}, err
	}
	for _, b := range blocks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO order_manual_blocks (manual_id, seq, title, body, assignee, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			manualID, b.Seq, b.Title, b.Body, b.Assignee, millis(now)); err != nil {
			return order.OrderManual{}, fmt.Errorf("insert order manual block: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, fmt.Sprintf("manual started: %s (%d steps)", prod.Code, len(blocks)), createdBy, millis(now)); err != nil {
		return order.OrderManual{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.OrderManual{}, fmt.Errorf("commit: %w", err)
	}
	return s.OrderManual(ctx, manualID)
}

func manualBlocksTx(ctx context.Context, tx *sql.Tx, productID int64) ([]order.ManualBlock, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, product_id, seq, title, body, assignee FROM manual_blocks
		 WHERE product_id = ? ORDER BY seq ASC, id ASC`, productID)
	if err != nil {
		return nil, fmt.Errorf("list template blocks: %w", err)
	}
	defer rows.Close()

	blocks := []order.ManualBlock{}
	for rows.Next() {
		var b order.ManualBlock
		if err := rows.Scan(&b.ID, &b.ProductID, &b.Seq, &b.Title, &b.Body, &b.Assignee); err != nil {
			return nil, fmt.Errorf("scan template block: %w", err)
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

func (s *Store) OrderManual(ctx context.Context, manualID int64) (order.OrderManual, error) {
	var m order.OrderManual
	var created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, order_id, product_id, product_code, product_name, created_by, created_at
		 FROM order_manuals WHERE id = ?`, manualID).
		Scan(&m.ID, &m.OrderID, &m.ProductID, &m.ProductCode, &m.ProductName, &m.CreatedBy, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return order.OrderManual{}, ErrNotFound
	}
	if err != nil {
		return order.OrderManual{}, fmt.Errorf("get order manual: %w", err)
	}
	m.CreatedAt = fromMillis(created)

	if err := s.fillOrderManual(ctx, &m); err != nil {
		return order.OrderManual{}, err
	}
	return m, nil
}

func (s *Store) fillOrderManual(ctx context.Context, m *order.OrderManual) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, manual_id, seq, title, body, assignee, answer, answered_by, answered_at,
		        flagged, flagged_by, flagged_at, flag_reason, created_at
		 FROM order_manual_blocks WHERE manual_id = ? ORDER BY seq ASC, id ASC`, m.ID)
	if err != nil {
		return fmt.Errorf("list order manual blocks: %w", err)
	}
	defer rows.Close()

	m.Blocks = []order.OrderManualBlock{}
	m.Total, m.Answered, m.Failed, m.Flagged = 0, 0, 0, 0
	for rows.Next() {
		var b order.OrderManualBlock
		var answer, answeredBy sql.NullString
		var answeredAt, created sql.NullInt64
		var flagged int
		var flaggedBy, flagReason sql.NullString
		var flaggedAt sql.NullInt64
		if err := rows.Scan(&b.ID, &b.ManualID, &b.Seq, &b.Title, &b.Body, &b.Assignee,
			&answer, &answeredBy, &answeredAt, &flagged, &flaggedBy, &flaggedAt, &flagReason, &created); err != nil {
			return fmt.Errorf("scan order manual block: %w", err)
		}
		b.Answer = answer.String
		b.AnsweredBy = answeredBy.String
		b.AnsweredAt = nullableTime(answeredAt)
		b.Flagged = flagged != 0
		b.FlaggedBy = flaggedBy.String
		b.FlaggedAt = nullableTime(flaggedAt)
		b.FlagReason = flagReason.String
		b.CreatedAt = fromMillis(created.Int64)
		m.Blocks = append(m.Blocks, b)
		m.Total++
		if answer.String == order.AnswerYes || answer.String == order.AnswerNo {
			m.Answered++
		}
		if answer.String == order.AnswerNo {
			m.Failed++
		}
		if flagged != 0 {
			m.Flagged++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate order manual blocks: %w", err)
	}
	return s.fillManualLog(ctx, m)
}

func (s *Store) fillManualLog(ctx context.Context, m *order.OrderManual) error {
	logRows, err := s.db.QueryContext(ctx,
		`SELECT id, manual_id, block_id, action, username, note, created_at
		 FROM manual_tick_log WHERE manual_id = ? ORDER BY created_at ASC, id ASC`, m.ID)
	if err != nil {
		return fmt.Errorf("list manual tick log: %w", err)
	}
	defer logRows.Close()

	m.Log = []order.ManualTickEvent{}
	for logRows.Next() {
		var e order.ManualTickEvent
		var ts int64
		if err := logRows.Scan(&e.ID, &e.ManualID, &e.BlockID, &e.Action, &e.Username, &e.Note, &ts); err != nil {
			return fmt.Errorf("scan manual tick event: %w", err)
		}
		e.CreatedAt = fromMillis(ts)
		m.Log = append(m.Log, e)
	}
	return logRows.Err()
}

func (s *Store) OrderManuals(ctx context.Context, orderID int64) ([]order.OrderManual, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM order_manuals WHERE order_id = ? ORDER BY id ASC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order manuals: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan order manual id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order manuals: %w", err)
	}

	out := []order.OrderManual{}
	for _, id := range ids {
		m, err := s.OrderManual(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *Store) AnswerManualBlock(ctx context.Context, manualID, blockID int64, answer, username string, now time.Time) (order.OrderManualBlock, error) {
	if !order.IsValidManualAnswer(answer) {
		return order.OrderManualBlock{}, ErrBadAnswer
	}
	if username == "" {
		username = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.OrderManualBlock{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var b order.OrderManualBlock
	var orderID int64
	var ans, ansBy sql.NullString
	var ansAt, created sql.NullInt64
	var seq int
	var title, body, assignee string
	err = tx.QueryRowContext(ctx,
		`SELECT b.id, b.manual_id, b.seq, b.title, b.body, b.assignee, b.answer, b.answered_by, b.answered_at, b.created_at, m.order_id
		 FROM order_manual_blocks b JOIN order_manuals m ON m.id = b.manual_id
		 WHERE b.id = ? AND b.manual_id = ?`, blockID, manualID).
		Scan(&b.ID, &b.ManualID, &seq, &title, &body, &assignee, &ans, &ansBy, &ansAt, &created, &orderID)
	if errors.Is(err, sql.ErrNoRows) {
		return order.OrderManualBlock{}, ErrNotFound
	}
	if err != nil {
		return order.OrderManualBlock{}, fmt.Errorf("get manual block: %w", err)
	}

	switch answer {
	case order.AnswerYes, order.AnswerNo:
		if _, err := tx.ExecContext(ctx,
			`UPDATE order_manual_blocks SET answer = ?, answered_by = ?, answered_at = ?,
			        flagged = 0, flagged_by = '', flagged_at = NULL, flag_reason = '' WHERE id = ?`,
			answer, username, millis(now), blockID); err != nil {
			return order.OrderManualBlock{}, fmt.Errorf("answer manual block: %w", err)
		}
		b.Answer, b.AnsweredBy = answer, username
		b.Flagged, b.FlaggedBy, b.FlagReason = false, "", ""
		t := now.UTC()
		b.AnsweredAt = &t
	default:
		if _, err := tx.ExecContext(ctx,
			`UPDATE order_manual_blocks SET answer = NULL, answered_by = NULL, answered_at = NULL,
			        flagged = 0, flagged_by = '', flagged_at = NULL, flag_reason = '' WHERE id = ?`,
			blockID); err != nil {
			return order.OrderManualBlock{}, fmt.Errorf("clear manual block: %w", err)
		}
		b.Answer, b.AnsweredBy, b.AnsweredAt = "", "", nil
		b.Flagged, b.FlaggedBy, b.FlagReason = false, "", ""
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO manual_tick_log (manual_id, block_id, action, username, note, created_at) VALUES (?, ?, ?, ?, '', ?)`,
		manualID, blockID, answer, username, millis(now)); err != nil {
		return order.OrderManualBlock{}, fmt.Errorf("insert tick log: %w", err)
	}

	action := "manual " + strings.TrimSpace(title) + ": " + answer
	if answer == order.AnswerClear {
		action = "manual " + strings.TrimSpace(title) + ": answer cleared"
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, action, username, millis(now)); err != nil {
		return order.OrderManualBlock{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return order.OrderManualBlock{}, fmt.Errorf("commit: %w", err)
	}
	b.ManualID, b.Seq, b.Title, b.Body, b.Assignee = manualID, seq, title, body, assignee
	b.CreatedAt = fromMillis(created.Int64)
	return b, nil
}

func (s *Store) FlagManualBlock(ctx context.Context, manualID, blockID int64, set bool, reason, flaggedBy string, now time.Time) (order.OrderManualBlock, string, error) {
	reason = strings.TrimSpace(reason)
	if set && reason == "" {
		return order.OrderManualBlock{}, "", ErrBadFlagReason
	}
	if flaggedBy == "" {
		flaggedBy = "system"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.OrderManualBlock{}, "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var b order.OrderManualBlock
	var orderID int64
	var orderNumber string
	var ans, ansBy sql.NullString
	var ansAt, created sql.NullInt64
	var seq int
	var title, body, assignee string
	err = tx.QueryRowContext(ctx,
		`SELECT b.id, b.manual_id, b.seq, b.title, b.body, b.assignee, b.answer, b.answered_by, b.answered_at,
		        b.flagged, b.flagged_by, b.flagged_at, b.flag_reason, b.created_at, m.order_id, o.order_number
		 FROM order_manual_blocks b
		 JOIN order_manuals m ON m.id = b.manual_id
		 JOIN orders o ON o.id = m.order_id
		 WHERE b.id = ? AND b.manual_id = ?`, blockID, manualID).
		Scan(&b.ID, &b.ManualID, &seq, &title, &body, &assignee, &ans, &ansBy, &ansAt,
			new(int), new(sql.NullString), new(sql.NullInt64), new(sql.NullString), &created, &orderID, &orderNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return order.OrderManualBlock{}, "", ErrNotFound
	}
	if err != nil {
		return order.OrderManualBlock{}, "", fmt.Errorf("get manual block: %w", err)
	}
	if set && !ans.Valid {
		return order.OrderManualBlock{}, "", ErrFlagUnanswered
	}

	notified := ansBy.String
	var action string
	if set {
		if _, err := tx.ExecContext(ctx,
			`UPDATE order_manual_blocks SET flagged = 1, flagged_by = ?, flagged_at = ?, flag_reason = ? WHERE id = ?`,
			flaggedBy, millis(now), reason, blockID); err != nil {
			return order.OrderManualBlock{}, "", fmt.Errorf("flag manual block: %w", err)
		}
		b.Flagged, b.FlaggedBy, b.FlagReason = true, flaggedBy, reason
		t := now.UTC()
		b.FlaggedAt = &t
		action = order.FlagSet
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE order_manual_blocks SET flagged = 0, flagged_by = '', flagged_at = NULL, flag_reason = '' WHERE id = ?`,
			blockID); err != nil {
			return order.OrderManualBlock{}, "", fmt.Errorf("unflag manual block: %w", err)
		}
		b.Flagged, b.FlaggedBy, b.FlagReason, b.FlaggedAt = false, "", "", nil
		notified = ""
		action = order.FlagClear
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO manual_tick_log (manual_id, block_id, action, username, note, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		manualID, blockID, action, flaggedBy, reason, millis(now)); err != nil {
		return order.OrderManualBlock{}, "", fmt.Errorf("insert flag log: %w", err)
	}

	verb := "flagged"
	if !set {
		verb = "flag cleared"
	}
	audit := fmt.Sprintf("manual %s: %s by %s", strings.TrimSpace(title), verb, flaggedBy)
	if set && reason != "" {
		audit += " — " + reason
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, audit, flaggedBy, millis(now)); err != nil {
		return order.OrderManualBlock{}, "", fmt.Errorf("insert audit: %w", err)
	}

	if notified != "" {
		notif := fmt.Sprintf("%s flagged your step “%s”", flaggedBy, strings.TrimSpace(title))
		if reason != "" {
			notif += ": " + reason
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO notifications (user_name, kind, order_id, order_number, ref_id, body, created_at)
			 VALUES (?, 'flag', ?, ?, ?, ?, ?)
			 ON CONFLICT(user_name, kind, ref_id) DO UPDATE SET
			     body = excluded.body, order_id = excluded.order_id,
			     order_number = excluded.order_number, read_at = NULL,
			     created_at = excluded.created_at`,
			notified, orderID, orderNumber, blockID, notif, millis(now)); err != nil {
			return order.OrderManualBlock{}, "", fmt.Errorf("insert flag notification: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return order.OrderManualBlock{}, "", fmt.Errorf("commit: %w", err)
	}
	b.ManualID, b.Seq, b.Title, b.Body, b.Assignee = manualID, seq, title, body, assignee
	b.CreatedAt = fromMillis(created.Int64)
	return b, notified, nil
}

func (s *Store) DeleteOrderManual(ctx context.Context, manualID int64, performedBy string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var orderID int64
	var code string
	err = tx.QueryRowContext(ctx,
		`SELECT order_id, product_code FROM order_manuals WHERE id = ?`, manualID).Scan(&orderID, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get order manual: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM order_manuals WHERE id = ?`, manualID); err != nil {
		return fmt.Errorf("delete order manual: %w", err)
	}
	if performedBy == "" {
		performedBy = "system"
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, "manual removed: "+code, performedBy, millis(now)); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return tx.Commit()
}

func (s *Store) ManualOverviews(ctx context.Context) ([]order.ManualOverview, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.order_id, o.order_number, o.status, m.product_id, m.product_code, m.product_name, m.created_by, m.created_at
FROM order_manuals m JOIN orders o ON o.id = m.order_id
ORDER BY o.id DESC, m.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list manual overviews: %w", err)
	}
	defer rows.Close()

	out := []order.ManualOverview{}
	idx := map[int64]int{}
	for rows.Next() {
		var ov order.ManualOverview
		var created int64
		if err := rows.Scan(&ov.ManualID, &ov.OrderID, &ov.OrderNumber, &ov.Status,
			&ov.ProductID, &ov.ProductCode, &ov.ProductName, &ov.CreatedBy, &created); err != nil {
			return nil, fmt.Errorf("scan manual overview: %w", err)
		}
		ov.CreatedAt = fromMillis(created)
		ov.Blocks = []order.OrderManualBlock{}
		idx[ov.ManualID] = len(out)
		out = append(out, ov)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate manual overviews: %w", err)
	}
	return out, s.fillOverviewBlocks(ctx, out)
}

func (s *Store) fillOverviewBlocks(ctx context.Context, out []order.ManualOverview) error {
	blockRows, err := s.db.QueryContext(ctx, `
SELECT b.manual_id, b.id, b.seq, b.title, b.body, b.assignee, b.answer, b.answered_by, b.answered_at,
       b.flagged, b.flagged_by, b.flagged_at, b.flag_reason, b.created_at
FROM order_manual_blocks b
ORDER BY b.manual_id ASC, b.seq ASC, b.id ASC`)
	if err != nil {
		return fmt.Errorf("list overview blocks: %w", err)
	}
	defer blockRows.Close()

	idx := map[int64]int{}
	for i, ov := range out {
		idx[ov.ManualID] = i
	}
	for blockRows.Next() {
		var manualID int64
		var b order.OrderManualBlock
		var answer, answeredBy sql.NullString
		var answeredAt, created sql.NullInt64
		var flagged int
		var flaggedBy, flagReason sql.NullString
		var flaggedAt sql.NullInt64
		if err := blockRows.Scan(&manualID, &b.ID, &b.Seq, &b.Title, &b.Body, &b.Assignee,
			&answer, &answeredBy, &answeredAt, &flagged, &flaggedBy, &flaggedAt, &flagReason, &created); err != nil {
			return fmt.Errorf("scan overview block: %w", err)
		}
		b.Answer = answer.String
		b.AnsweredBy = answeredBy.String
		b.AnsweredAt = nullableTime(answeredAt)
		b.Flagged = flagged != 0
		b.FlaggedBy = flaggedBy.String
		b.FlaggedAt = nullableTime(flaggedAt)
		b.FlagReason = flagReason.String
		b.CreatedAt = fromMillis(created.Int64)
		i, ok := idx[manualID]
		if !ok {
			continue
		}
		out[i].Blocks = append(out[i].Blocks, b)
		out[i].Total++
		if answer.String == order.AnswerYes || answer.String == order.AnswerNo {
			out[i].Answered++
		}
		if answer.String == order.AnswerNo {
			out[i].Failed++
		}
		if flagged != 0 {
			out[i].Flagged++
		}
	}
	return blockRows.Err()
}
