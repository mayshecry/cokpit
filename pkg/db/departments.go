package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cockpit/pkg/si"
)

var ErrDuplicateDepartmentName = errors.New("department name already exists")

// scanPerms decodes the JSON permission list stored on a department row.
func scanPerms(raw string) []string {
	if raw == "" {
		return nil
	}
	var perms []string
	if err := json.Unmarshal([]byte(raw), &perms); err != nil {
		return nil
	}
	return perms
}

func scanDepartment(rows interface{ Scan(...any) error }) (si.Department, error) {
	var d si.Department
	var created int64
	var permsRaw string
	if err := rows.Scan(&d.ID, &d.Name, &d.Description, &permsRaw, &created); err != nil {
		return si.Department{}, err
	}
	d.Permissions = scanPerms(permsRaw)
	d.CreatedAt = fromMillis(created)
	return d, nil
}

func (s *Store) CreateDepartment(ctx context.Context, name, description string, now time.Time) (si.Department, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return si.Department{}, errors.New("department name is required")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM departments WHERE name = ?`, name).Scan(&exists); err != nil {
		return si.Department{}, fmt.Errorf("check duplicate department: %w", err)
	}
	if exists > 0 {
		return si.Department{}, ErrDuplicateDepartmentName
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO departments (name, description, created_at) VALUES (?, ?, ?)`, name, description, millis(now))
	if err != nil {
		return si.Department{}, fmt.Errorf("insert department: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return si.Department{}, fmt.Errorf("last insert id: %w", err)
	}
	return si.Department{ID: id, Name: name, Description: description, CreatedAt: now.UTC()}, nil
}

func (s *Store) ListDepartments(ctx context.Context) ([]si.Department, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, permissions, created_at FROM departments ORDER BY name ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	defer rows.Close()
	departments := []si.Department{}
	for rows.Next() {
		d, err := scanDepartment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		departments = append(departments, d)
	}
	return departments, rows.Err()
}

func (s *Store) DepartmentByID(ctx context.Context, id int64) (si.Department, error) {
	d, err := scanDepartment(s.db.QueryRowContext(ctx, `SELECT id, name, description, permissions, created_at FROM departments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return si.Department{}, ErrNotFound
	}
	if err != nil {
		return si.Department{}, fmt.Errorf("department by id: %w", err)
	}
	return d, nil
}

func (s *Store) DeleteDepartment(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM departments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete department: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddUserToDepartment(ctx context.Context, userID, departmentID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO user_departments (user_id, department_id) VALUES (?, ?)`, userID, departmentID)
	if err != nil {
		return fmt.Errorf("add user to department: %w", err)
	}
	return nil
}

func (s *Store) RemoveUserFromDepartment(ctx context.Context, userID, departmentID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_departments WHERE user_id = ? AND department_id = ?`, userID, departmentID)
	if err != nil {
		return fmt.Errorf("remove user from department: %w", err)
	}
	return nil
}

func (s *Store) UserDepartments(ctx context.Context, userID int64) ([]si.Department, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.name, d.description, d.permissions, d.created_at FROM departments d JOIN user_departments ud ON ud.department_id = d.id WHERE ud.user_id = ? ORDER BY d.name ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("user departments: %w", err)
	}
	defer rows.Close()
	departments := []si.Department{}
	for rows.Next() {
		d, err := scanDepartment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		departments = append(departments, d)
	}
	return departments, rows.Err()
}

// SetDepartmentPermissions replaces the permission list of a department.
// The permissions are persisted as a JSON array.
func (s *Store) SetDepartmentPermissions(ctx context.Context, id int64, perms []string) error {
	if perms == nil {
		perms = []string{}
	}
	raw, err := json.Marshal(perms)
	if err != nil {
		return fmt.Errorf("marshal department permissions: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE departments SET permissions = ? WHERE id = ?`, string(raw), id)
	if err != nil {
		return fmt.Errorf("update department permissions: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UserDepartmentPermissions returns the deduplicated set of permissions the
// user gains through their department memberships (independent of their role).
func (s *Store) UserDepartmentPermissions(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.permissions FROM departments d
		JOIN user_departments ud ON ud.department_id = d.id
		WHERE ud.user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("user department permissions: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	perms := []string{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan department permissions: %w", err)
		}
		for _, p := range scanPerms(raw) {
			if !seen[p] {
				seen[p] = true
				perms = append(perms, p)
			}
		}
	}
	return perms, rows.Err()
}

func (s *Store) DepartmentUsers(ctx context.Context, departmentID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM user_departments WHERE department_id = ?`, departmentID)
	if err != nil {
		return nil, fmt.Errorf("department users: %w", err)
	}
	defer rows.Close()
	var users []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan user id: %w", err)
		}
		users = append(users, uid)
	}
	return users, rows.Err()
}
