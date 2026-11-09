package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrWorkflowProfileNotFound = errors.New("workflow profile not found")

type WorkflowProfile struct {
	ID         int64     `json:"id"`
	Customer   string    `json:"customer"`
	Name       string    `json:"name"`
	StepsJSON  string    `json:"-"`
	Steps      any       `json:"steps,omitempty"`
	UpdatedBy  string    `json:"updatedBy,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (s *Store) ListWorkflowProfiles(ctx context.Context, customer string) ([]WorkflowProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, customer_name, name, updated_by, updated_at FROM workflow_profiles WHERE customer_name = ? ORDER BY name COLLATE NOCASE, id`, customer)
	if err != nil {
		return nil, fmt.Errorf("list workflow profiles: %w", err)
	}
	defer rows.Close()
	out := []WorkflowProfile{}
	for rows.Next() {
		var p WorkflowProfile
		var updated int64
		if err := rows.Scan(&p.ID, &p.Customer, &p.Name, &p.UpdatedBy, &updated); err != nil {
			return nil, fmt.Errorf("scan workflow profile: %w", err)
		}
		p.UpdatedAt = time.UnixMilli(updated).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetWorkflowProfile(ctx context.Context, id int64) (WorkflowProfile, error) {
	var p WorkflowProfile
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT id, customer_name, name, steps_json, updated_by, updated_at FROM workflow_profiles WHERE id = ?`, id).Scan(&p.ID, &p.Customer, &p.Name, &p.StepsJSON, &p.UpdatedBy, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkflowProfile{}, ErrWorkflowProfileNotFound
	}
	if err != nil {
		return WorkflowProfile{}, fmt.Errorf("get workflow profile: %w", err)
	}
	p.UpdatedAt = time.UnixMilli(updated).UTC()
	return p, nil
}

func (s *Store) CreateWorkflowProfile(ctx context.Context, customer, name, stepsJSON, by string, now time.Time) (WorkflowProfile, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO workflow_profiles (customer_name, name, steps_json, updated_by, updated_at) VALUES (?, ?, ?, ?, ?)`, customer, name, stepsJSON, by, millis(now))
	if err != nil {
		return WorkflowProfile{}, fmt.Errorf("create workflow profile: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return WorkflowProfile{}, fmt.Errorf("workflow profile id: %w", err)
	}
	return WorkflowProfile{ID: id, Customer: customer, Name: name, StepsJSON: stepsJSON, UpdatedBy: by, UpdatedAt: now.UTC()}, nil
}

func (s *Store) UpdateWorkflowProfile(ctx context.Context, id int64, name, stepsJSON, by string, now time.Time) (WorkflowProfile, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE workflow_profiles SET name = ?, steps_json = ?, updated_by = ?, updated_at = ? WHERE id = ?`, name, stepsJSON, by, millis(now), id)
	if err != nil {
		return WorkflowProfile{}, fmt.Errorf("update workflow profile: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WorkflowProfile{}, fmt.Errorf("workflow profile update count: %w", err)
	}
	if n == 0 {
		return WorkflowProfile{}, ErrWorkflowProfileNotFound
	}
	return s.GetWorkflowProfile(ctx, id)
}
