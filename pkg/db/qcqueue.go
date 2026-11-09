package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type QCQueueItem struct {
	ID           int64  `json:"id"`
	OrderNumber  string `json:"orderNumber"`
	Customer     string `json:"customer"`
	Device       string `json:"device"`
	Tech         string `json:"tech"`
	UpdatedAt    int64  `json:"updatedAt"`
	TargetAt     int64  `json:"targetAt"`
	ProductCode  string `json:"productCode"`
	ProductName  string `json:"productName"`
	SplitFrom    int64  `json:"splitFrom,omitempty"`
	IsSplit      bool   `json:"isSplit,omitempty"`
}

func (s *Store) QCQueue(ctx context.Context) ([]QCQueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.order_number, o.customer_name, o.device, o.assigned_to,
		       o.updated_at, o.target_completion_at,
		       COALESCE(m.pc, ''), COALESCE(m.pn, ''),
		       COALESCE(o.split_from_order_id, 0)
		FROM orders o
		LEFT JOIN (
			SELECT order_id,
			       group_concat(product_code, ' + ') AS pc,
			       group_concat(product_name, ' + ') AS pn
			FROM order_manuals
			GROUP BY order_id
		) m ON m.order_id = o.id
		 WHERE o.status = 'QC_Review'
		 ORDER BY o.target_completion_at ASC, o.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("qc queue: %w", err)
	}
	defer rows.Close()
	out := []QCQueueItem{}
	for rows.Next() {
		var q QCQueueItem
		var splitFrom int64
		if err := rows.Scan(&q.ID, &q.OrderNumber, &q.Customer, &q.Device, &q.Tech, &q.UpdatedAt, &q.TargetAt, &q.ProductCode, &q.ProductName, &splitFrom); err != nil {
			return nil, fmt.Errorf("scan qc queue: %w", err)
		}
		if splitFrom > 0 {
			q.SplitFrom = splitFrom
			q.IsSplit = true
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

type QCContextRow struct {
	OrderNumber string `json:"orderNumber"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	Inspector   string `json:"inspector"`
	At          int64  `json:"at"`
	SameProduct bool   `json:"sameProduct"`
}

func (s *Store) QCContext(ctx context.Context, excludeID int64, customer, productCode string, limit int) ([]QCContextRow, error) {
	if limit <= 0 || limit > 20 {
		limit = 6
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.order_number, q.status, q.notes, q.inspector_id, q.created_at,
		       EXISTS (SELECT 1 FROM order_manuals om
		                WHERE om.order_id = q.order_id AND om.product_code = ?)
		  FROM qc_checks q
		  JOIN orders o ON o.id = q.order_id
		 WHERE q.order_id <> ?
		   AND (? <> '' AND o.customer_name = ? OR (? <> '' AND EXISTS (SELECT 1 FROM order_manuals om2
		            WHERE om2.order_id = q.order_id AND om2.product_code = ?)))
		 ORDER BY q.created_at DESC
		 LIMIT `+fmt.Sprint(limit), productCode, excludeID, customer, customer, productCode, productCode)
	if err != nil {
		return nil, fmt.Errorf("qc context: %w", err)
	}
	defer rows.Close()
	out := []QCContextRow{}
	for rows.Next() {
		var r QCContextRow
		if err := rows.Scan(&r.OrderNumber, &r.Status, &r.Notes, &r.Inspector, &r.At, &r.SameProduct); err != nil {
			return nil, fmt.Errorf("scan qc context: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) OrderContextKeys(ctx context.Context, orderID int64) (customer, code, name string, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT customer_name FROM orders WHERE id = ?`, orderID).Scan(&customer); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", "", ErrNotFound
		}
		return "", "", "", fmt.Errorf("order context keys: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `SELECT product_code, product_name FROM order_manuals WHERE order_id = ? ORDER BY id LIMIT 1`, orderID).Scan(&code, &name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return customer, "", "", fmt.Errorf("order context product: %w", err)
	}
	return customer, code, name, nil
}
