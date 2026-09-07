package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cockpit/pkg/si"
)

var (
	ErrDuplicateCustomerNumber = errors.New("customer number already exists")
	ErrDuplicateProjectCode     = errors.New("project code already exists for this customer")
	ErrDuplicateSICode          = errors.New("SI code already exists")
	ErrPrimaryProjectRequired    = errors.New("the primary project must be linked to the SI")
	ErrCrossCustomerLink         = errors.New("all linked projects must belong to the same customer as the primary project")
)

// CreateCustomer creates a new customer (klant) with a unique number.



func (s *Store) CreateCustomer(ctx context.Context, number, name string, now time.Time) (si.Customer, error) {
	name = strings.TrimSpace(name)
	if number == "" || name == "" {
		return si.Customer{}, errors.New("customer number and name are required")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers WHERE number = ?`, number).Scan(&exists); err != nil {
		return si.Customer{}, fmt.Errorf("check duplicate customer: %w", err)
	}
	if exists > 0 {
		return si.Customer{}, ErrDuplicateCustomerNumber
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO customers (number, name, created_at) VALUES (?, ?, ?)`, number, name, millis(now))
	if err != nil {
		return si.Customer{}, fmt.Errorf("insert customer: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return si.Customer{}, fmt.Errorf("last insert id: %w", err)
	}
	return si.Customer{ID: id, Number: number, Name: name, CreatedAt: now.UTC()}, nil
}

func (s *Store) CustomerByNumber(ctx context.Context, number string) (si.Customer, error) {
	var c si.Customer
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, number, name, created_at FROM customers WHERE number = ?`, number).Scan(&c.ID, &c.Number, &c.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return si.Customer{}, ErrNotFound
	}
	if err != nil {
		return si.Customer{}, fmt.Errorf("customer by number: %w", err)
	}
	c.CreatedAt = fromMillis(created)
	return c, nil
}

func (s *Store) CustomerByID(ctx context.Context, id int64) (si.Customer, error) {
	var c si.Customer
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, number, name, created_at FROM customers WHERE id = ?`, id).Scan(&c.ID, &c.Number, &c.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return si.Customer{}, ErrNotFound
	}
	if err != nil {
		return si.Customer{}, fmt.Errorf("customer by id: %w", err)
	}
	c.CreatedAt = fromMillis(created)
	return c, nil
}

func (s *Store) ListCustomers(ctx context.Context) ([]si.Customer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, number, name, created_at FROM customers ORDER BY number ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()
	customers := []si.Customer{}
	for rows.Next() {
		var c si.Customer
		var created int64
		if err := rows.Scan(&c.ID, &c.Number, &c.Name, &created); err != nil {
			return nil, fmt.Errorf("scan customer: %w", err)
		}
		c.CreatedAt = fromMillis(created)
		customers = append(customers, c)
	}
	return customers, rows.Err()
}
// CreateProject creates a project underneath a customer; code is unique within that customer.



func (s *Store) CreateProject(ctx context.Context, customerID int64, code, name, description string, now time.Time) (si.Project, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return si.Project{}, errors.New("project code and name are required")
	}
	if _, err := s.CustomerByID(ctx, customerID); err != nil {
		return si.Project{}, err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE customer_id = ? AND code = ?`, customerID, code).Scan(&exists); err != nil {
		return si.Project{}, fmt.Errorf("check duplicate project: %w", err)
	}
	if exists > 0 {
		return si.Project{}, ErrDuplicateProjectCode
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO projects (customer_id, code, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, customerID, code, name, description, millis(now), millis(now))
	if err != nil {
		return si.Project{}, fmt.Errorf("insert project: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return si.Project{}, fmt.Errorf("last insert id: %w", err)
	}
	return s.ProjectByID(ctx, id)
}

func (s *Store) ProjectByID(ctx context.Context, id int64) (si.Project, error) {
	var p si.Project
	var created, updated int64
	err := s.db.QueryRowContext(ctx,
		`SELECT p.id, p.customer_id, c.number, p.code, p.name, p.description, p.created_at, p.updated_at
		 FROM projects p JOIN customers c ON c.id = p.customer_id WHERE p.id = ?`, id).Scan(&p.ID, &p.CustomerID, &p.CustomerNumber, &p.Code, &p.Name, &p.Description, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return si.Project{}, ErrNotFound
	}
	if err != nil {
		return si.Project{}, fmt.Errorf("project by id: %w", err)
	}
	p.CreatedAt = fromMillis(created)
	p.UpdatedAt = fromMillis(updated)
	return p, nil
}

// ListProjects returns projects; when customerID > 0 only those of that customer.

func (s *Store) ListProjects(ctx context.Context, customerID int64) ([]si.Project, error) {
	query := `SELECT p.id, p.customer_id, c.number, p.code, p.name, p.description, p.created_at, p.updated_at
			FROM projects p JOIN customers c ON c.id = p.customer_id`
	args := []any{}
	if customerID > 0 {
		query += ` WHERE p.customer_id = ?`
		args = append(args, customerID)
	
	}
	query += ` ORDER BY p.code ASC, p.id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	projects := []si.Project{}
	for rows.Next() {
		var p si.Project
		var created, updated int64
		if err := rows.Scan(&p.ID, &p.CustomerID, &p.CustomerNumber, &p.Code, &p.Name, &p.Description, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		p.CreatedAt = fromMillis(created)
		p.UpdatedAt = fromMillis(updated)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}
const siCols = `id, code, name, description, status, version, environment, primary_project_id, created_by, created_at, updated_at`

// CreateSI creates a new SI in Requested state, linked to one primary project.



func (s *Store) CreateSI(ctx context.Context, req si.CreateSIRequest, performedBy string, now time.Time) (si.SI, error) {
	code := strings.TrimSpace(req.Code)
	name := strings.TrimSpace(req.Name)
	if code == "" || name == "" {
		return si.SI{}, fmt.Errorf("%w: SI code and name are required", si.ErrValidation)
	}
	if req.PrimaryProjectID <= 0 {
		return si.SI{}, fmt.Errorf("%w: primaryProjectId is required", si.ErrValidation)
	}
	env := req.Environment
	if env == "" {
		env = si.EnvironmentProductie
	}
	if !si.IsValidEnvironment(env) {
		return si.SI{}, fmt.Errorf("%w: invalid environment; allowed: TEST, ACCEPTATIE, PRODUCTIE", si.ErrValidation)
	}
	if _, err := s.ProjectByID(ctx, req.PrimaryProjectID); err != nil {
		return si.SI{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return si.SI{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_integrations WHERE code = ?`, code).Scan(&exists); err != nil {
		return si.SI{}, fmt.Errorf("check duplicate SI: %w", err)
	}
	if exists > 0 {
		return si.SI{}, ErrDuplicateSICode
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO system_integrations (code, name, description, status, version, environment, primary_project_id, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,

		code, name, req.Description, string(si.StatusRequested), 0, string(env), req.PrimaryProjectID, performedBy, millis(now), millis(now))
	if err != nil {
		return si.SI{}, fmt.Errorf("insert SI: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return si.SI{}, fmt.Errorf("last insert id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO si_project_links (si_id, project_id, is_primary) VALUES (?, ?, ?)`, id, req.PrimaryProjectID, 1); err != nil {
		return si.SI{}, fmt.Errorf("insert primary link: %w", err)
	}
	to := si.StatusRequested
	if err := s.siEventTx(ctx, tx, id, "created", "", string(to), 0, "requested by "+performedBy, performedBy, now); err != nil {
		return si.SI{}, err
	}
	if err := tx.Commit(); err != nil {
		return si.SI{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetSI(ctx, id)
}

func (s *Store) GetSI(ctx context.Context, id int64) (si.SI, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+siCols+` FROM system_integrations WHERE id = ?`, id)
	var v si.SI
	var status, env string
	var created, updated int64
	err := row.Scan(&v.ID, &v.Code, &v.Name, &v.Description, &status, &v.Version, &env, &v.PrimaryProjectID, &v.CreatedBy, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return si.SI{}, ErrNotFound
	}
	if err != nil {
		return si.SI{}, fmt.Errorf("get SI: %w", err)
	}
	v.Status = si.Status(status)
	v.StatusLabel = si.StatusLabel(v.Status)
	v.Environment = si.Environment(env)
	v.CreatedAt = fromMillis(created)
	v.UpdatedAt = fromMillis(updated)
	if err := s.loadSILinks(ctx, &v); err != nil {
		return si.SI{}, err
	}
	return v, nil
}

func (s *Store) loadSILinks(ctx context.Context, v *si.SI) error {
	rows, err := s.db.QueryContext(ctx, `SELECT project_id FROM si_project_links WHERE si_id = ? ORDER BY is_primary DESC, project_id ASC`, v.ID)
	if err != nil {
		return fmt.Errorf("load SI links: %w", err)
	}
	defer rows.Close()
	v.ProjectIDs = []int64{}
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			return fmt.Errorf("scan SI link: %w", err)
		}
		v.ProjectIDs = append(v.ProjectIDs, pid)
	}
	return rows.Err()
}

// ListSIs returns SIs, optionally filtered by customer number, project id or status.

func (s *Store) ListSIs(ctx context.Context, customerNumber string, projectID int64, status *si.Status) ([]si.SI, error) {
	query := `SELECT ` + siCols + ` FROM system_integrations si`
	where := []string{}
	args := []any{}
	if customerNumber != "" {
		where = append(where, `si.id IN (SELECT l.si_id FROM si_project_links l JOIN projects p ON p.id = l.project_id JOIN customers c ON c.id = p.customer_id WHERE c.number = ?)`)
		args = append(args, customerNumber)
	}
	if projectID > 0 {
		where = append(where, `si.id IN (SELECT l.si_id FROM si_project_links l WHERE l.project_id = ?)`)
		args = append(args, projectID)
	}
	if status != nil {
		where = append(where, "si.status = ?")
		args = append(args, string(*status))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY si.code ASC, si.id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list SIs: %w", err)
	}
	defer rows.Close()
	out := []si.SI{}
	for rows.Next() {
		var v si.SI
		var st, env string
		var created, updated int64
		if err := rows.Scan(&v.ID, &v.Code, &v.Name, &v.Description, &st, &v.Version, &env, &v.PrimaryProjectID, &v.CreatedBy, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan SI: %w", err)
		}
		v.Status = si.Status(st)
		v.StatusLabel = si.StatusLabel(v.Status)
		v.Environment = si.Environment(env)
		v.CreatedAt = fromMillis(created)
		v.UpdatedAt = fromMillis(updated)
		if err := s.loadSILinks(ctx, &v); err != nil {
			return nil, fmt.Errorf("load SI links: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SIs: %w", err)
	}
	return out, nil
}

// UpdateSI changes metadata (name/description/environment) and records an audit event.

func (s *Store) UpdateSI(ctx context.Context, id int64, req si.UpdateSIRequest, performedBy string, now time.Time) (si.SI, error) {
	current, err := s.GetSI(ctx, id)
	if err != nil {
		return si.SI{}, err
	}
	changes := []string{}
	name := current.Name
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			return si.SI{}, errors.New("name must not be empty")
		}
		if trimmed != current.Name {
			changes = append(changes, "name: " + current.Name + " -> " + trimmed)
			name = trimmed
		}
	}
	description := current.Description
	if req.Description != nil {
		if *req.Description != current.Description {
			changes = append(changes, "description updated")
			description = *req.Description
		}
	}
	env := current.Environment
	if req.Environment != nil {
		if !si.IsValidEnvironment(*req.Environment) {
			return si.SI{}, errors.New("invalid environment; allowed: TEST, ACCEPTATIE, PRODUCTIE")
		}
		if *req.Environment != current.Environment {
			changes = append(changes, "environment: " + string(current.Environment) + " -> " + string(*req.Environment))
			env = *req.Environment
		}
	}
	if len(changes) == 0 {
		return current, nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE system_integrations SET name = ?, description = ?, environment = ?, updated_at = ? WHERE id = ?`,
		name, description, string(env), millis(now), id); err != nil {
		return si.SI{}, fmt.Errorf("update SI: %w", err)
	}
	if err := s.siEvent(ctx, id, "updated", "", "", current.Version, strings.Join(changes, "; "), performedBy, now); err != nil {
		return si.SI{}, err
	}
	return s.GetSI(ctx, id)
}
// TransitionSI moves an SI along an allowed lifecycle transition and bumps
// the version when the SI goes live.

func (s *Store) TransitionSI(ctx context.Context, id int64, to si.Status, note, performedBy string, now time.Time) (si.SI, error) {
	current, err := s.GetSI(ctx, id)
	if err != nil {
		return si.SI{}, err
	}
	target, err := si.DetermineTransition(current.Status, to)
	if err != nil {
		return si.SI{}, err
	}
	version := current.Version
	if target == si.StatusLive {
		version++
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return si.SI{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE system_integrations SET status = ?, version = ?, updated_at = ? WHERE id = ?`, string(target), version, millis(now), id); err != nil {
		return si.SI{}, fmt.Errorf("update SI status: %w", err)
	}
	if err := s.siEventTx(ctx, tx, id, "transition", string(current.Status), string(target), version, note, performedBy, now); err != nil {
		return si.SI{}, err
	}
	if err := tx.Commit(); err != nil {
		return si.SI{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetSI(ctx, id)
}

// SetSIProjects replaces the set of linked projects (the primary project must
// stay linked; all projects must belong to the same customer)..


func (s *Store) SetSIProjects(ctx context.Context, id int64, projectIDs []int64, performedBy string, now time.Time) (si.SI, error) {
	current, err := s.GetSI(ctx, id)
	if err != nil {
		return si.SI{}, err
	}
	if len(projectIDs) == 0 {
		return si.SI{}, ErrPrimaryProjectRequired
	}
	hasPrimary := false
	seen := map[int64]bool{}
	for _, pid := range projectIDs {
		if seen[pid] {
			return si.SI{}, errors.New("duplicate project id in projectIds")
		}
		seen[pid] = true
		if pid == current.PrimaryProjectID {
			hasPrimary = true
		}
	}
	if !hasPrimary {
		return si.SI{}, ErrPrimaryProjectRequired
	}
	primary, err := s.ProjectByID(ctx, current.PrimaryProjectID)
	if err != nil {
		return si.SI{}, err
	}
	for _, pid := range projectIDs {
		if pid == current.PrimaryProjectID {
			continue
		}
		p, err := s.ProjectByID(ctx, pid)
		if err != nil {
			return si.SI{}, err
		}
		if p.CustomerID != primary.CustomerID {
			return si.SI{}, ErrCrossCustomerLink
		}
	}
	old := intSet(current.ProjectIDs)
	newSet := intSet(projectIDs)
	added, removed := setDiff(old, newSet)
	if len(added) == 0 && len(removed) == 0 {
		return current, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return si.SI{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM si_project_links WHERE si_id = ?`, id); err != nil {
		return si.SI{}, fmt.Errorf("clear SI links: %w", err)
	}
	for _, pid := range projectIDs {
		isPrimary := 0
		if pid == current.PrimaryProjectID {
			isPrimary =  1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO si_project_links (si_id, project_id, is_primary) VALUES (?, ?, ?)`, id, pid, isPrimary); err != nil {
			return si.SI{}, fmt.Errorf("insert SI link: %w", err)
		}
	}
	note := ""
	if len(added) > 0 {
		note += "added: " + joinIDs(added)
	}
	if len(removed) > 0 {
		if note != "" {
			note += "; "
		}
		note += "removed: " + joinIDs(removed)
	}
	if err := s.siEventTx(ctx, tx, id, "projects", "", "", current.Version, note, performedBy, now); err != nil {
		return si.SI{}, err
	}
	if err := tx.Commit(); err != nil {
		return si.SI{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetSI(ctx, id)
}
// SIEvents returns the immutable audit trail for an SI.

func (s *Store) SIEvents(ctx context.Context, siID int64) ([]si.SIEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, si_id, action, from_status, to_status, version, note, performed_by, timestamp FROM si_events WHERE si_id = ? ORDER BY timestamp ASC, id ASC`, siID)
	if err != nil {
		return nil, fmt.Errorf("list SI events: %w", err)
	}
	defer rows.Close()
	out := []si.SIEvent{}
	for rows.Next() {
		var ev si.SIEvent
		var ts int64
		if err := rows.Scan(&ev.ID, &ev.SIID, &ev.Action, &ev.FromStatus, &ev.ToStatus, &ev.Version, &ev.Note, &ev.PerformedBy, &ts); err != nil {
			return nil, fmt.Errorf("scan SI event: %w", err)
		}
		ev.Timestamp = fromMillis(ts)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) siEvent(ctx context.Context, siID int64, action, fromStatus, toStatus string, version int, note, performedBy string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO si_events (si_id, action, from_status, to_status, version, note, performed_by, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		siID, action, fromStatus, toStatus, version, note, performedBy, millis(now)); err != nil {
		return fmt.Errorf("insert SI event: %w", err)
	}
	return nil
}

func (s *Store) siEventTx(ctx context.Context, tx *sql.Tx, siID int64, action, fromStatus, toStatus string, version int, note, performedBy string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO si_events (si_id, action, from_status, to_status, version, note, performed_by, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		siID, action, fromStatus, toStatus, version, note, performedBy, millis(now)); err != nil {
		return fmt.Errorf("insert SI event: %w", err)
	}
	return nil
}

func intSet(ids []int64) map[int64]bool {
	out := map[int64]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func setDiff(old, new map[int64]bool) (added, removed []int64) {
	for id := range new {
		if !old[id] {
			added = append(added, id)
		}
	}
	for id := range old {
		if !new[id] {
			removed = append(removed, id)
		}
	}
	return added, removed
}

func joinIDs(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ", ")
}

// Project checklist functions

func (s *Store) ProjectChecklist(ctx context.Context, projectID int64) ([]si.ProjectChecklistItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, seq, label, description, checked_by, checked_at, created_at
		 FROM project_checklist_items WHERE project_id = ? ORDER BY seq ASC, id ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project checklist: %w", err)
	}
	defer rows.Close()

	items := []si.ProjectChecklistItem{}
	for rows.Next() {
		var it si.ProjectChecklistItem
		var checkedBy sql.NullString
		var checkedAt sql.NullInt64
		var created int64
		if err := rows.Scan(&it.ID, &it.ProjectID, &it.Seq, &it.Label, &it.Description, &checkedBy, &checkedAt, &created); err != nil {
			return nil, fmt.Errorf("scan project checklist item: %w", err)
		}
		it.CheckedBy = checkedBy.String
		it.CheckedAt = nullableTime(checkedAt)
		it.CreatedAt = fromMillis(created)
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Store) AddProjectChecklistItem(ctx context.Context, projectID int64, label, description string, now time.Time) (si.ProjectChecklistItem, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return si.ProjectChecklistItem{}, errors.New("label is required")
	}

	var seq int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), -1) + 1 FROM project_checklist_items WHERE project_id = ?`, projectID).Scan(&seq); err != nil {
		return si.ProjectChecklistItem{}, fmt.Errorf("get next seq: %w", err)
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO project_checklist_items (project_id, seq, label, description, created_at) VALUES (?, ?, ?, ?, ?)`,
		projectID, seq, label, description, millis(now))
	if err != nil {
		return si.ProjectChecklistItem{}, fmt.Errorf("insert project checklist item: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return si.ProjectChecklistItem{}, fmt.Errorf("last insert id: %w", err)
	}

	return si.ProjectChecklistItem{
		ID:          id,
		ProjectID:   projectID,
		Seq:         seq,
		Label:       label,
		Description: description,
		CreatedAt:   now.UTC(),
	}, nil
}

func (s *Store) TickProjectChecklistItem(ctx context.Context, itemID int64, username string, now time.Time) (si.ProjectChecklistItem, error) {
	var it si.ProjectChecklistItem
	var checkedBy sql.NullString
	var checkedAt sql.NullInt64
	var created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, seq, label, description, checked_by, checked_at, created_at
		 FROM project_checklist_items WHERE id = ?`, itemID).Scan(
		&it.ID, &it.ProjectID, &it.Seq, &it.Label, &it.Description, &checkedBy, &checkedAt, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return si.ProjectChecklistItem{}, ErrNotFound
	}
	if err != nil {
		return si.ProjectChecklistItem{}, fmt.Errorf("get project checklist item: %w", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE project_checklist_items SET checked_by = ?, checked_at = ? WHERE id = ?`,
		username, millis(now), itemID); err != nil {
		return si.ProjectChecklistItem{}, fmt.Errorf("tick project checklist item: %w", err)
	}

	it.CheckedBy = username
	it.CheckedAt = &now
	it.CreatedAt = fromMillis(created)
	return it, nil
}

func (s *Store) UntickProjectChecklistItem(ctx context.Context, itemID int64, now time.Time) (si.ProjectChecklistItem, error) {
	var it si.ProjectChecklistItem
	var checkedBy sql.NullString
	var checkedAt sql.NullInt64
	var created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, seq, label, description, checked_by, checked_at, created_at
		 FROM project_checklist_items WHERE id = ?`, itemID).Scan(
		&it.ID, &it.ProjectID, &it.Seq, &it.Label, &it.Description, &checkedBy, &checkedAt, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return si.ProjectChecklistItem{}, ErrNotFound
	}
	if err != nil {
		return si.ProjectChecklistItem{}, fmt.Errorf("get project checklist item: %w", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE project_checklist_items SET checked_by = NULL, checked_at = NULL WHERE id = ?`,
		itemID); err != nil {
		return si.ProjectChecklistItem{}, fmt.Errorf("untick project checklist item: %w", err)
	}

	it.CheckedBy = ""
	it.CheckedAt = nil
	it.CreatedAt = fromMillis(created)
	return it, nil
}

func (s *Store) DeleteProjectChecklistItem(ctx context.Context, itemID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM project_checklist_items WHERE id = ?`, itemID)
	if err != nil {
		return fmt.Errorf("delete project checklist item: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ProjectChecklistSummary(ctx context.Context, projectID int64) (total, done int, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(checked_by) FROM project_checklist_items WHERE project_id = ?`, projectID).Scan(&total, &done)
	return total, done, err
}
