package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNoCustomerFlow = errors.New("no customer workflow")

type CustomerFlowMeta struct {
	Customer  string    `json:"customer"`
	UpdatedBy string    `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Store) GetCustomerWorkflow(ctx context.Context, customer string) (string, error) {
	var json string
	err := s.db.QueryRowContext(ctx, `SELECT steps_json FROM customer_workflows WHERE customer_name = ?`, customer).Scan(&json)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoCustomerFlow
	}
	if err != nil {
		return "", fmt.Errorf("get customer workflow: %w", err)
	}
	return json, nil
}

func (s *Store) PutCustomerWorkflow(ctx context.Context, customer, stepsJSON, by string, now time.Time) error {
	if stepsJSON == "" {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM customer_workflows WHERE customer_name = ?`, customer); err != nil {
			return fmt.Errorf("remove customer workflow: %w", err)
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO customer_workflows (customer_name, steps_json, updated_by, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(customer_name) DO UPDATE SET steps_json = excluded.steps_json,
		                                        updated_by = excluded.updated_by,
		                                        updated_at = excluded.updated_at`,
		customer, stepsJSON, by, millis(now)); err != nil {
		return fmt.Errorf("put customer workflow: %w", err)
	}
	return nil
}

func (s *Store) ListCustomerWorkflows(ctx context.Context) ([]CustomerFlowMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT customer_name, updated_by, updated_at FROM customer_workflows ORDER BY customer_name`)
	if err != nil {
		return nil, fmt.Errorf("list customer workflows: %w", err)
	}
	defer rows.Close()
	out := []CustomerFlowMeta{}
	for rows.Next() {
		var m CustomerFlowMeta
		var at int64
		if err := rows.Scan(&m.Customer, &m.UpdatedBy, &at); err != nil {
			return nil, fmt.Errorf("scan customer workflow: %w", err)
		}
		m.UpdatedAt = time.UnixMilli(at).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DistinctOrderCustomers(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT customer_name FROM orders WHERE customer_name <> '' ORDER BY customer_name`)
	if err != nil {
		return nil, fmt.Errorf("distinct order customers: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan customer name: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
