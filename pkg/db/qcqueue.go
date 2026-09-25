package db

import (
	"context"
	"fmt"
)

// QCQueueItem is one order waiting on the QC bench.
type QCQueueItem struct {
	ID          int64  `json:"id"`
	OrderNumber string `json:"orderNumber"`
	Customer    string `json:"customer"`
	Device      string `json:"device"`
	Tech        string `json:"tech"`
	UpdatedAt   int64  `json:"updatedAt"`
	TargetAt    int64  `json:"targetAt"`
}

// QCQueue returns orders in QC_Review, most urgent target first.
func (s *Store) QCQueue(ctx context.Context) ([]QCQueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, order_number, customer_name, device, assigned_to, updated_at, target_completion_at
		  FROM orders
		 WHERE status = 'QC_Review'
		 ORDER BY target_completion_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("qc queue: %w", err)
	}
	defer rows.Close()
	out := []QCQueueItem{}
	for rows.Next() {
		var q QCQueueItem
		if err := rows.Scan(&q.ID, &q.OrderNumber, &q.Customer, &q.Device, &q.Tech, &q.UpdatedAt, &q.TargetAt); err != nil {
			return nil, fmt.Errorf("scan qc queue: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
