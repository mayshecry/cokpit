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

func (s *Store) AddManualBlock(ctx context.Context, productID int64, title, body, assignee string, now time.Time) (order.ManualBlock, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return order.ManualBlock{}, errors.New("title is required")
	}
	assignee = strings.TrimSpace(assignee)
	if err := validateAssignee(ctx, s.db, assignee); err != nil {
		return order.ManualBlock{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.ManualBlock{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE id = ?`, productID).Scan(&exists); err != nil {
		return order.ManualBlock{}, fmt.Errorf("check product: %w", err)
	}
	if exists == 0 {
		return order.ManualBlock{}, ErrNotFound
	}

	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM manual_blocks WHERE product_id = ?`, productID).Scan(&maxSeq); err != nil {
		return order.ManualBlock{}, fmt.Errorf("max seq: %w", err)
	}
	seq := 0
	if maxSeq.Valid {
		seq = int(maxSeq.Int64) + 1
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO manual_blocks (product_id, seq, title, body, assignee, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		productID, seq, title, strings.TrimSpace(body), assignee, millis(now), millis(now))
	if err != nil {
		return order.ManualBlock{}, fmt.Errorf("insert manual block: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return order.ManualBlock{}, fmt.Errorf("last insert id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return order.ManualBlock{}, fmt.Errorf("commit: %w", err)
	}
	return order.ManualBlock{
		ID: id, ProductID: productID, Seq: seq,
		Title: title, Body: strings.TrimSpace(body), Assignee: assignee,
		CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}, nil
}

func (s *Store) UpdateManualBlock(ctx context.Context, productID, blockID int64, title, body, assignee *string, now time.Time) (order.ManualBlock, error) {
	if assignee != nil {
		*assignee = strings.TrimSpace(*assignee)
		if err := validateAssignee(ctx, s.db, *assignee); err != nil {
			return order.ManualBlock{}, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.ManualBlock{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var b order.ManualBlock
	var created, updated int64
	err = tx.QueryRowContext(ctx,
		`SELECT id, product_id, seq, title, body, assignee, created_at, updated_at
		 FROM manual_blocks WHERE id = ? AND product_id = ?`, blockID, productID).
		Scan(&b.ID, &b.ProductID, &b.Seq, &b.Title, &b.Body, &b.Assignee, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return order.ManualBlock{}, ErrNotFound
	}
	if err != nil {
		return order.ManualBlock{}, fmt.Errorf("get manual block: %w", err)
	}

	if title != nil {
		*title = strings.TrimSpace(*title)
		if *title == "" {
			return order.ManualBlock{}, errors.New("title must not be empty")
		}
		b.Title = *title
	}
	if body != nil {
		b.Body = strings.TrimSpace(*body)
	}
	if assignee != nil {
		b.Assignee = *assignee
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE manual_blocks SET title = ?, body = ?, assignee = ?, updated_at = ? WHERE id = ?`,
		b.Title, b.Body, b.Assignee, millis(now), blockID); err != nil {
		return order.ManualBlock{}, fmt.Errorf("update manual block: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return order.ManualBlock{}, fmt.Errorf("commit: %w", err)
	}
	b.UpdatedAt = now.UTC()
	return b, nil
}

func (s *Store) MoveManualBlock(ctx context.Context, productID, blockID int64, direction string, now time.Time) ([]order.ManualBlock, error) {
	if direction != "up" && direction != "down" {
		return nil, errors.New("direction must be up or down")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var seq int
	err = tx.QueryRowContext(ctx,
		`SELECT seq FROM manual_blocks WHERE id = ? AND product_id = ?`, blockID, productID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get manual block: %w", err)
	}

	neighbourSeq := seq - 1
	if direction == "down" {
		neighbourSeq = seq + 1
	}
	var neighbourID int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM manual_blocks WHERE product_id = ? AND seq = ?`, productID, neighbourSeq).Scan(&neighbourID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrManualEdge
	}
	if err != nil {
		return nil, fmt.Errorf("find neighbour: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE manual_blocks SET seq = ?, updated_at = ? WHERE id = ?`, -1, millis(now), blockID); err != nil {
		return nil, fmt.Errorf("move block (temp): %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE manual_blocks SET seq = ?, updated_at = ? WHERE id = ?`, seq, millis(now), neighbourID); err != nil {
		return nil, fmt.Errorf("move neighbour: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE manual_blocks SET seq = ?, updated_at = ? WHERE id = ?`, neighbourSeq, millis(now), blockID); err != nil {
		return nil, fmt.Errorf("move block: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.manualBlocks(ctx, productID)
}

func (s *Store) DeleteManualBlock(ctx context.Context, productID, blockID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `DELETE FROM manual_blocks WHERE id = ? AND product_id = ?`, blockID, productID)
	if err != nil {
		return fmt.Errorf("delete manual block: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM manual_blocks WHERE product_id = ? ORDER BY seq ASC, id ASC`, productID)
	if err != nil {
		return fmt.Errorf("list remaining blocks: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan remaining blocks: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate remaining blocks: %w", err)
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE manual_blocks SET seq = ? WHERE id = ?`, i, id); err != nil {
			return fmt.Errorf("renumber block: %w", err)
		}
	}
	return tx.Commit()
}
