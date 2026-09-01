package db

import (
"context"
"database/sql"
"errors"
"fmt"
"time"

"cockpit/pkg/order"
)

const maxChecklistItems = 500

// SetChecklist replaces all pick lines of an order with the given items.
// The previous set (and any tick state) is discarded: re-importing the daily
// verkoopregels rebaselines every order's checklist.
func (s *Store) SetChecklist(ctx context.Context, orderID int64, items []order.ChecklistItemInput, createdBy string, now time.Time) ([]order.ChecklistItem, error) {
if len(items) > maxChecklistItems {
items = items[:maxChecklistItems]
}
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
return nil, fmt.Errorf("begin: %w", err)
}
defer tx.Rollback()

var exists int
if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE id = ?`, orderID).Scan(&exists); err != nil {
return nil, fmt.Errorf("check order: %w", err)
}
if exists == 0 {
return nil, ErrNotFound
}

if _, err := tx.ExecContext(ctx, `DELETE FROM checklist_items WHERE order_id = ?`, orderID); err != nil {
return nil, fmt.Errorf("clear checklist: %w", err)
}

created := []order.ChecklistItem{}
for i, it := range items {
seq := i
if it.Aantal < 0 {
it.Aantal = 0
}
res, err := tx.ExecContext(ctx,
`INSERT INTO checklist_items (order_id, seq, artikel, omschrijving, locatie, aantal, created_at)
 VALUES (?, ?, ?, ?, ?, ?, ?)`,
orderID, seq, it.Artikel, it.Omschrijving, it.Locatie, it.Aantal, millis(now))
if err != nil {
return nil, fmt.Errorf("insert checklist item: %w", err)
}
id, err := res.LastInsertId()
if err != nil {
return nil, fmt.Errorf("last insert id: %w", err)
}
created = append(created, order.ChecklistItem{
ID:          id,
OrderID:     orderID,
Seq:         seq,
Artikel:     it.Artikel,
Omschrijving: it.Omschrijving,
Locatie:     it.Locatie,
Aantal:      it.Aantal,
CreatedAt:    now.UTC(),
})
}

if _, err := tx.ExecContext(ctx,
`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
orderID, fmt.Sprintf("checklist generated (%d items)", len(created)), createdBy, millis(now)); err != nil {
return nil, fmt.Errorf("insert audit: %w", err)
}

if err := tx.Commit(); err != nil {
return nil, fmt.Errorf("commit: %w", err)
}
return created, nil
}

// ChecklistItems returns all pick lines of an order, oldest seq first.
func (s *Store) ChecklistItems(ctx context.Context, orderID int64) ([]order.ChecklistItem, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE id = ?`, orderID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check order: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}

	rows, err := s.db.QueryContext(ctx,
`SELECT id, order_id, seq, artikel, omschrijving, locatie, aantal, checked_by, checked_at, created_at
		 FROM checklist_items WHERE order_id = ? ORDER BY seq ASC, id ASC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list checklist: %w", err)
	}
	defer rows.Close()

	items := []order.ChecklistItem{}
	for rows.Next() {
		var it order.ChecklistItem
		var checkedBy sql.NullString
		var checkedAt sql.NullInt64
		var created int64
		if err := rows.Scan(&it.ID, &it.OrderID, &it.Seq, &it.Artikel, &it.Omschrijving, &it.Locatie, &it.Aantal, &checkedBy, &checkedAt, &created); err != nil {
			return nil, fmt.Errorf("scan checklist item: %w", err)
		}
		it.CheckedBy = checkedBy.String
		it.CheckedAt = nullableTime(checkedAt)
		it.CreatedAt = fromMillis(created)
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Store) checklistItemByID(ctx context.Context, itemID int64) (order.ChecklistItem, error) {
	var it order.ChecklistItem
	var checkedBy sql.NullString
	var checkedAt sql.NullInt64
	var created int64
	err := s.db.QueryRowContext(ctx,
`SELECT id, order_id, seq, artikel, omschrijving, locatie, aantal, checked_by, checked_at, created_at
		 FROM checklist_items WHERE id = ?`, itemID).Scan(
&it.ID, &it.OrderID, &it.Seq, &it.Artikel, &it.Omschrijving, &it.Locatie, &it.Aantal,
		&checkedBy, &checkedAt, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return order.ChecklistItem{}, ErrNotFound
	}
	if err != nil {
		return order.ChecklistItem{}, fmt.Errorf("get checklist item: %w", err)
	}
	it.CheckedBy = checkedBy.String
	it.CheckedAt = nullableTime(checkedAt)
	it.CreatedAt = fromMillis(created)
	return it, nil
}

// TickChecklistItem marks a pick line as done by username and logs who did it.
func (s *Store) TickChecklistItem(ctx context.Context, itemID int64, username string, now time.Time) (order.ChecklistItem, error) {
	item, err := s.checklistItemByID(ctx, itemID)
	if err != nil {
		return order.ChecklistItem{}, err
	}
	if _, err := s.db.ExecContext(ctx,
`UPDATE checklist_items SET checked_by = ?, checked_at = ? WHERE id = ?`,
username, millis(now), itemID); err != nil {
		return order.ChecklistItem{}, fmt.Errorf("tick checklist item: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
item.OrderID, "checklist item ticked:"+item.Artikel, username, millis(now)); err != nil {
		return order.ChecklistItem{}, fmt.Errorf("insert audit: %w", err)
	}
	item.CheckedBy = username
	nowUTC := now.UTC()
	item.CheckedAt = &nowUTC
	return item, nil
}

// UntickChecklistItem marks a pick line as open again (e.g. correction). The
// audit trail keeps the original tick for accountability; only the final state is cleared.
func (s *Store) UntickChecklistItem(ctx context.Context, itemID int64, username string, now time.Time) (order.ChecklistItem, error) {
	item, err := s.checklistItemByID(ctx, itemID)
	if err != nil {
		return order.ChecklistItem{}, err
	}
	if _, err := s.db.ExecContext(ctx,
`UPDATE checklist_items SET checked_by = NULL, checked_at = NULL WHERE id = ?`, itemID); err != nil {
		return order.ChecklistItem{}, fmt.Errorf("untick checklist item: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
item.OrderID, "checklist item unticked:"+item.Artikel, username, millis(now)); err != nil {
		return order.ChecklistItem{}, fmt.Errorf("insert audit: %w", err)
	}
	item.CheckedBy = ""
	item.CheckedAt = nil
	return item, nil
}
