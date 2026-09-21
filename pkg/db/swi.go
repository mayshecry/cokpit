package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cockpit/pkg/order"
	"cockpit/pkg/swi"
)

// ErrProcessTasksOpen marks an advance blocked by unfinished required tasks.
var ErrProcessTasksOpen = swi.ErrTasksOpen

// ErrEscalationActive marks an action blocked by an open escalation.
var ErrEscalationActive = swi.ErrEscalationActive

// ErrTaskState is returned when a workflow task is already in the requested
// state (ticking a ticked task or unticking an open one).
var ErrTaskState = errors.New("process task is already in that state")

// EnsureProcess creates the process row for an order when it does not exist yet
// and instantiates the work instructions of the current stage. It is safe to
// call for every order; existing processes are left untouched.
func (s *Store) EnsureProcess(ctx context.Context, orderID int64, performedBy string, now time.Time) (swi.Process, error) {
	if performedBy == "" {
		performedBy = "system"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return swi.Process{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var number string
	err = tx.QueryRowContext(ctx, `SELECT order_number FROM orders WHERE id = ?`, orderID).Scan(&number)
	if errors.Is(err, sql.ErrNoRows) {
		return swi.Process{}, ErrNotFound
	}
	if err != nil {
		return swi.Process{}, fmt.Errorf("read order: %w", err)
	}

	var existing string
	err = tx.QueryRowContext(ctx, `SELECT stage FROM order_process WHERE order_id = ?`, orderID).Scan(&existing)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO order_process (order_id, stage, stage_since, escalated, escalation_level, version, updated_by, updated_at)
			 VALUES (?, ?, ?, 0, '', 1, ?, ?)`,
			orderID, string(swi.StageOrderImport), millis(now), performedBy, millis(now)); err != nil {
			return swi.Process{}, fmt.Errorf("insert process: %w", err)
		}
		if err := s.seedTasksTx(ctx, tx, orderID, swi.StageOrderImport, now); err != nil {
			return swi.Process{}, err
		}
		if err := s.processEventTx(ctx, tx, orderID, "process started", "", swi.StageOrderImport, "", "SWI-proces gestart bij orderinvoer", performedBy, now); err != nil {
			return swi.Process{}, err
		}
	case err != nil:
		return swi.Process{}, fmt.Errorf("read process: %w", err)
	default:
		// Already present: make sure the stage has its work instructions.
		if err := s.seedTasksTx(ctx, tx, orderID, swi.Stage(existing), now); err != nil {
			return swi.Process{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return swi.Process{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetProcess(ctx, orderID)
}

// GetProcess returns the process state of an order, creating it when missing.
func (s *Store) GetProcess(ctx context.Context, orderID int64) (swi.Process, error) {
	p, err := s.getProcessRow(ctx, orderID)
	if errors.Is(err, ErrNotFound) {
		return s.EnsureProcess(ctx, orderID, "system", time.Now().UTC())
	}
	if err != nil {
		return swi.Process{}, err
	}
	if err := s.fillProcess(ctx, &p); err != nil {
		return swi.Process{}, err
	}
	return p, nil
}

func (s *Store) getProcessRow(ctx context.Context, orderID int64) (swi.Process, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.order_id, o.order_number, o.status, o.assigned_to, p.stage, p.stage_since,
		       p.escalated, p.escalation_level, p.version, p.updated_by, p.updated_at
		FROM order_process p JOIN orders o ON o.id = p.order_id
		WHERE p.order_id = ?`, orderID)
	return scanProcess(row)
}

func scanProcess(row *sql.Row) (swi.Process, error) {
	var p swi.Process
	var stageSince, updated int64
	var escalated int
	err := row.Scan(&p.OrderID, &p.OrderNumber, &p.OrderStatus, &p.Assignee, &p.Stage, &stageSince,
		&escalated, &p.EscalationLevel, &p.Version, &p.UpdatedBy, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return swi.Process{}, ErrNotFound
	}
	if err != nil {
		return swi.Process{}, fmt.Errorf("scan process: %w", err)
	}
	p.StageSince = fromMillis(stageSince)
	p.UpdatedAt = fromMillis(updated)
	p.Escalated = escalated != 0
	p.StageLabel = swi.StageLabel(p.Stage)
	p.SuggestedStatus = string(swi.SuggestOrderStatus(p.Stage))
	p.NextStages = swi.NextStages(p.Stage)
	return p, nil
}

// fillProcess loads the derived counters of a process: open escalations, task
// progress and the outstanding external updates.
func (s *Store) fillProcess(ctx context.Context, p *swi.Process) error {
	tasks, err := s.ProcessTasks(ctx, p.OrderID, nil)
	if err != nil {
		return err
	}
	p.Tasks = nil
	p.TaskTotal, p.TaskDone, p.TasksRequiredOpen = swi.TaskProgress(tasks)
	for _, t := range tasks {
		if t.Stage == p.Stage {
			p.Tasks = append(p.Tasks, t)
		}
	}
	if p.Tasks == nil {
		p.Tasks = []swi.Task{}
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM escalations WHERE order_id = ? AND resolved_at IS NULL`, p.OrderID).Scan(&p.OpenEscalations); err != nil {
		return fmt.Errorf("count escalations: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN status = 'PENDING' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END), 0)
		 FROM external_sync_jobs WHERE order_id = ?`, p.OrderID).Scan(&p.SyncPending, &p.SyncFailed); err != nil {
		return fmt.Errorf("count sync jobs: %w", err)
	}
	return nil
}
// seedTasksTx instantiates the work instructions of a stage, ignoring titles
// that already exist for the order so re-entering a stage does not duplicate
// completed steps.
func (s *Store) seedTasksTx(ctx context.Context, tx *sql.Tx, orderID int64, stage swi.Stage, now time.Time) error {
	for _, tpl := range swi.StageTasks(stage) {
		required := 0
		if tpl.Required {
			required = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO process_tasks (order_id, stage, seq, title, description, role, required, done, done_by, created_at)
			SELECT ?, ?, ?, ?, ?, ?, ?, 0, '', ?
			WHERE NOT EXISTS (
				SELECT 1 FROM process_tasks WHERE order_id = ? AND stage = ? AND title = ?
			)`,
			orderID, string(stage), tpl.Seq, tpl.Title, tpl.Description, tpl.Role, required, millis(now),
			orderID, string(stage), tpl.Title); err != nil {
			return fmt.Errorf("seed task %q: %w", tpl.Title, err)
		}
	}
	return nil
}

func (s *Store) processEventTx(ctx context.Context, tx *sql.Tx, orderID int64, action string, from, to swi.Stage, level swi.Level, note, performedBy string, now time.Time) error {
	if performedBy == "" {
		performedBy = "system"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO process_events (order_id, action, from_stage, to_stage, level, note, performed_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		orderID, action, string(from), string(to), string(level), note, performedBy, millis(now)); err != nil {
		return fmt.Errorf("insert process event: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (order_id, action, performed_by, timestamp) VALUES (?, ?, ?, ?)`,
		orderID, action, performedBy, millis(now)); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}

func currentStageTx(ctx context.Context, tx *sql.Tx, orderID int64) (swi.Stage, bool, error) {
	var stage string
	var escalated int
	err := tx.QueryRowContext(ctx, `SELECT stage, escalated FROM order_process WHERE order_id = ?`, orderID).Scan(&stage, &escalated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrNotFound
	}
	if err != nil {
		return "", false, fmt.Errorf("read process: %w", err)
	}
	return swi.Stage(stage), escalated != 0, nil
}

func openRequiredTasksTx(ctx context.Context, tx *sql.Tx, orderID int64, stage swi.Stage) (int, error) {
	var open int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM process_tasks WHERE order_id = ? AND stage = ? AND required = 1 AND done = 0`,
		orderID, string(stage)).Scan(&open); err != nil {
		return 0, fmt.Errorf("count open tasks: %w", err)
	}
	return open, nil
}

func orderNumberTx(ctx context.Context, tx *sql.Tx, orderID int64) (string, error) {
	var number string
	err := tx.QueryRowContext(ctx, `SELECT order_number FROM orders WHERE id = ?`, orderID).Scan(&number)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read order number: %w", err)
	}
	return number, nil
}

// queueSyncJobsTx plans and persists the external updates of a stage change.
// The idempotency key makes re-queueing the same transition a no-op.
func queueSyncJobsTx(ctx context.Context, tx *sql.Tx, orderID int64, number string, from, to swi.Stage, now time.Time) ([]swi.SyncJob, error) {
	plans := swi.PlanSync(from, to)
	jobs := make([]swi.SyncJob, 0, len(plans))
	for i, plan := range plans {
		key := swi.JobKey(orderID, from, to, i, plan)
		payload, err := json.Marshal(map[string]any{
			"orderId":     orderID,
			"orderNumber": number,
			"fromStage":   from,
			"toStage":     to,
			"operation":   plan.Operation,
			"entity":      plan.Entity,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO external_sync_jobs (order_id, order_number, system, operation, direction, entity, reason, status, payload, attempts, idempotency_key, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, 0, ?, ?, ?)
			ON CONFLICT(idempotency_key) DO NOTHING`,
			orderID, number, string(plan.System), string(plan.Operation), string(plan.Direction), plan.Entity, plan.Reason,
			string(payload), key, millis(now), millis(now))
		if err != nil {
			return nil, fmt.Errorf("queue sync job: %w", err)
		}
		if id, err := res.LastInsertId(); err == nil && id > 0 {
			jobs = append(jobs, swi.SyncJob{
				ID: id, OrderID: orderID, OrderNumber: number, System: plan.System,
				SystemLabel: swi.SystemLabel(plan.System), Operation: plan.Operation, Direction: plan.Direction,
				Entity: plan.Entity, Reason: plan.Reason, Status: swi.SyncPending,
				StatusLabel: swi.SyncStatusLabel(swi.SyncPending), Payload: payload,
				IdempotencyKey: key, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
			})
		}
	}
	swi.SortJobs(jobs)
	return jobs, nil
}
// AdvanceProcess moves an order to the next stage of the SWI process. It
// enforces the stage machine, blocks advancement while the order is escalated
// and (unless force is set) requires every mandatory task of the current stage
// to be done. All external updates of the transition are queued in the same
// transaction, so the outbox can never miss a stage change.
func (s *Store) AdvanceProcess(ctx context.Context, orderID int64, to swi.Stage, note, performedBy string, force bool, now time.Time) (swi.Process, []swi.SyncJob, error) {
	if performedBy == "" {
		performedBy = "system"
	}
	if _, err := s.EnsureProcess(ctx, orderID, performedBy, now); err != nil {
		return swi.Process{}, nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return swi.Process{}, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	from, escalated, err := currentStageTx(ctx, tx, orderID)
	if err != nil {
		return swi.Process{}, nil, err
	}
	if escalated {
		return swi.Process{}, nil, fmt.Errorf("%w: %v", swi.ErrEscalationActive, from)
	}
	if _, err := swi.DetermineAdvance(from, to); err != nil {
		return swi.Process{}, nil, err
	}
	if !force {
		open, err := openRequiredTasksTx(ctx, tx, orderID, from)
		if err != nil {
			return swi.Process{}, nil, err
		}
		if open > 0 {
			return swi.Process{}, nil, fmt.Errorf("%w: %d verplichte stap(pen) van %q staan nog open",
				swi.ErrTasksOpen, open, swi.StageLabel(from))
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE order_process SET stage = ?, stage_since = ?, version = version + 1, updated_by = ?, updated_at = ? WHERE order_id = ?`,
		string(to), millis(now), performedBy, millis(now), orderID); err != nil {
		return swi.Process{}, nil, fmt.Errorf("update process: %w", err)
	}
	if err := s.seedTasksTx(ctx, tx, orderID, to, now); err != nil {
		return swi.Process{}, nil, err
	}
	action := fmt.Sprintf("process stage %s -> %s", from, to)
	if err := s.processEventTx(ctx, tx, orderID, action, from, to, "", note, performedBy, now); err != nil {
		return swi.Process{}, nil, err
	}

	number, err := orderNumberTx(ctx, tx, orderID)
	if err != nil {
		return swi.Process{}, nil, err
	}
	jobs, err := queueSyncJobsTx(ctx, tx, orderID, number, from, to, now)
	if err != nil {
		return swi.Process{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return swi.Process{}, nil, fmt.Errorf("commit: %w", err)
	}

	updated, err := s.GetProcess(ctx, orderID)
	if err != nil {
		return swi.Process{}, nil, err
	}
	return updated, jobs, nil
}


// ListProcesses returns the process rows, optionally filtered to a single
// stage, for the process board.
func (s *Store) ListProcesses(ctx context.Context, stage *swi.Stage, limit int) ([]swi.Process, error) {
	query := `
		SELECT p.order_id, o.order_number, o.status, o.assigned_to, p.stage, p.stage_since,
		       p.escalated, p.escalation_level, p.version, p.updated_by, p.updated_at
		FROM order_process p JOIN orders o ON o.id = p.order_id`
	args := []any{}
	if stage != nil {
		if *stage == swi.StageEscalation {
			query += ` WHERE p.escalated = 1`
		} else {
			query += ` WHERE p.stage = ? AND p.escalated = 0`
			args = append(args, string(*stage))
		}
	}
	query += ` ORDER BY p.stage_since ASC, p.order_id ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	defer rows.Close()

	procs := []swi.Process{}
	for rows.Next() {
		var p swi.Process
		var stageSince, updated int64
		var escalated int
		if err := rows.Scan(&p.OrderID, &p.OrderNumber, &p.OrderStatus, &p.Assignee, &p.Stage, &stageSince,
			&escalated, &p.EscalationLevel, &p.Version, &p.UpdatedBy, &updated); err != nil {
			return nil, fmt.Errorf("scan process: %w", err)
		}
		p.StageSince = fromMillis(stageSince)
		p.UpdatedAt = fromMillis(updated)
		p.Escalated = escalated != 0
		p.StageLabel = swi.StageLabel(p.Stage)
		p.SuggestedStatus = string(swi.SuggestOrderStatus(p.Stage))
		p.NextStages = swi.NextStages(p.Stage)
		procs = append(procs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range procs {
		if err := s.fillProcess(ctx, &procs[i]); err != nil {
			return nil, err
		}
	}
	return procs, nil
}

// ProcessTasks returns the work instructions of an order, optionally filtered
// to one stage.
func (s *Store) ProcessTasks(ctx context.Context, orderID int64, stage *swi.Stage) ([]swi.Task, error) {
	query := `SELECT id, order_id, stage, seq, title, description, role, required, done, done_by, done_at, created_at
		FROM process_tasks WHERE order_id = ?`
	args := []any{orderID}
	if stage != nil {
		query += ` AND stage = ?`
		args = append(args, string(*stage))
	}
	query += ` ORDER BY seq ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list process tasks: %w", err)
	}
	defer rows.Close()

	tasks := []swi.Task{}
	for rows.Next() {
		var t swi.Task
		var required, done int
		var doneAt sql.NullInt64
		var created int64
		if err := rows.Scan(&t.ID, &t.OrderID, &t.Stage, &t.Seq, &t.Title, &t.Description, &t.Role,
			&required, &done, &t.DoneBy, &doneAt, &created); err != nil {
			return nil, fmt.Errorf("scan process task: %w", err)
		}
		t.Required = required != 0
		t.Done = done != 0
		t.DoneAt = nullableTime(doneAt)
		t.CreatedAt = fromMillis(created)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
// SetProcessTaskDone ticks or unticks a work instruction step. Ticking records
// who did it and when; unticking clears that attribution. It returns the task
// and its order id so callers can publish the change.
func (s *Store) SetProcessTaskDone(ctx context.Context, taskID int64, done bool, note, performedBy string, now time.Time) (swi.Task, int64, error) {
	if performedBy == "" {
		performedBy = "system"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return swi.Task{}, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var t swi.Task
	var required, isDone int
	var created int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, order_id, stage, seq, title, description, role, required, done, created_at
		FROM process_tasks WHERE id = ?`, taskID).
		Scan(&t.ID, &t.OrderID, &t.Stage, &t.Seq, &t.Title, &t.Description, &t.Role, &required, &isDone, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return swi.Task{}, 0, ErrNotFound
	}
	if err != nil {
		return swi.Task{}, 0, fmt.Errorf("read process task: %w", err)
	}
	t.Required = required != 0
	t.CreatedAt = fromMillis(created)

	if isDone == boolToInt(done) {
		return swi.Task{}, 0, ErrTaskState
	}

	if done {
		if _, err := tx.ExecContext(ctx,
			`UPDATE process_tasks SET done = 1, done_by = ?, done_at = ? WHERE id = ?`,
			performedBy, millis(now), taskID); err != nil {
			return swi.Task{}, 0, fmt.Errorf("tick task: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE process_tasks SET done = 0, done_by = '', done_at = NULL WHERE id = ?`, taskID); err != nil {
			return swi.Task{}, 0, fmt.Errorf("untick task: %w", err)
		}
	}

	action := fmt.Sprintf("process task %q %s", strings.TrimSpace(t.Title), doneWord(done))
	if err := s.processEventTx(ctx, tx, t.OrderID, action, t.Stage, t.Stage, "", note, performedBy, now); err != nil {
		return swi.Task{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return swi.Task{}, 0, fmt.Errorf("commit: %w", err)
	}

	t.Done = done
	if done {
		at := now.UTC()
		t.DoneBy = performedBy
		t.DoneAt = &at
	}
	return t, t.OrderID, nil
}

// ProcessEvents returns the process trail of an order (newest first).
func (s *Store) ProcessEvents(ctx context.Context, orderID int64, limit int) ([]swi.Event, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, order_id, action, from_stage, to_stage, level, note, performed_by, created_at
		FROM process_events WHERE order_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		orderID, limit)
	if err != nil {
		return nil, fmt.Errorf("list process events: %w", err)
	}
	defer rows.Close()

	events := []swi.Event{}
	for rows.Next() {
		var e swi.Event
		var created int64
		if err := rows.Scan(&e.ID, &e.OrderID, &e.Action, &e.FromStage, &e.ToStage, &e.Level, &e.Note, &e.PerformedBy, &created); err != nil {
			return nil, fmt.Errorf("scan process event: %w", err)
		}
		e.CreatedAt = fromMillis(created)
		events = append(events, e)
	}
	return events, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func doneWord(done bool) string {
	if done {
		return "afgetekend"
	}
	return "heropend"
}

// EscalateProcess raises an escalation on an order. The process moves to the
// escalation lane (remembering where it came from) and an Omnitracker update
// is queued. Escalating an already escalated order records the additional
// escalation without queueing a duplicate sync job.
func (s *Store) EscalateProcess(ctx context.Context, orderID int64, level swi.Level, reason swi.Reason, note, escalatedTo, performedBy string, now time.Time) (swi.Escalation, swi.Process, []swi.SyncJob, error) {
	if performedBy == "" {
		performedBy = "system"
	}
	if _, err := s.EnsureProcess(ctx, orderID, performedBy, now); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	from, _, err := currentStageTx(ctx, tx, orderID)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	if swi.IsTerminalStage(from) {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("%w: %v", swi.ErrInvalidTransition, from)
	}
	number, err := orderNumberTx(ctx, tx, orderID)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}

	returnStage := from
	if from == swi.StageEscalation {
		var existing string
		if err := tx.QueryRowContext(ctx,
			`SELECT return_stage FROM escalations WHERE order_id = ? AND resolved_at IS NULL ORDER BY raised_at DESC, id DESC LIMIT 1`,
			orderID).Scan(&existing); err == nil && existing != "" {
			returnStage = swi.Stage(existing)
		}
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO escalations (order_id, stage, return_stage, level, reason, note, raised_by, raised_at, escalated_to)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		orderID, string(from), string(returnStage), string(level), string(reason), note, performedBy, millis(now), escalatedTo)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("insert escalation: %w", err)
	}
	escalationID, err := res.LastInsertId()
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("last insert id: %w", err)
	}

	jobs := []swi.SyncJob{}
	if from != swi.StageEscalation {
		if _, err := tx.ExecContext(ctx, `
			UPDATE order_process SET stage = ?, stage_since = ?, escalated = 1, escalation_level = ?,
			       version = version + 1, updated_by = ?, updated_at = ? WHERE order_id = ?`,
			string(swi.StageEscalation), millis(now), string(level), performedBy, millis(now), orderID); err != nil {
			return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("move to escalation lane: %w", err)
		}
		if err := s.seedTasksTx(ctx, tx, orderID, swi.StageEscalation, now); err != nil {
			return swi.Escalation{}, swi.Process{}, nil, err
		}
		jobs, err = queueSyncJobsTx(ctx, tx, orderID, number, from, swi.StageEscalation, now)
		if err != nil {
			return swi.Escalation{}, swi.Process{}, nil, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE order_process SET escalated = 1, escalation_level = ?, version = version + 1,
			       updated_by = ?, updated_at = ? WHERE order_id = ?`,
			string(level), performedBy, millis(now), orderID); err != nil {
			return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("update escalation level: %w", err)
		}
	}

	action := fmt.Sprintf("process escalated to %s (%s)", level, reason)
	if err := s.processEventTx(ctx, tx, orderID, action, from, swi.StageEscalation, level, note, performedBy, now); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	if err := notifyEscalationTx(ctx, tx, orderID, number, escalatedTo, level, reason, note, now); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("commit: %w", err)
	}

	esc, err := s.EscalationByID(ctx, escalationID)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	updated, err := s.GetProcess(ctx, orderID)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	return esc, updated, jobs, nil
}

// notifyEscalationTx notifies the escalation owner, when one was named.
func notifyEscalationTx(ctx context.Context, tx *sql.Tx, orderID int64, number, escalatedTo string, level swi.Level, reason swi.Reason, note string, now time.Time) error {
	if strings.TrimSpace(escalatedTo) == "" {
		return nil
	}
	body := fmt.Sprintf("%s geëscaleerd (%s, %s)", number, level, swi.ReasonLabel(reason))
	if note != "" {
		body += ": " + note
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO notifications (user_name, kind, order_id, order_number, ref_id, body, created_at)
		VALUES (?, 'escalation', ?, ?, 0, ?, ?)`,
		escalatedTo, orderID, number, body, millis(now)); err != nil {
		return fmt.Errorf("insert escalation notification: %w", err)
	}
	return nil
}
// ResolveEscalation closes an escalation. When no open escalations remain the
// process returns to the flow at the remembered return stage, and the release
// is reported to Omnitracker.
func (s *Store) ResolveEscalation(ctx context.Context, escalationID int64, resolution, performedBy string, now time.Time) (swi.Escalation, swi.Process, []swi.SyncJob, error) {
	if performedBy == "" {
		performedBy = "system"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var esc swi.Escalation
	var raisedAt int64
	var resolvedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id, order_id, stage, return_stage, level, reason, note, raised_by, raised_at, escalated_to, resolved_at
		FROM escalations WHERE id = ?`, escalationID).
		Scan(&esc.ID, &esc.OrderID, &esc.Stage, &esc.ReturnStage, &esc.Level, &esc.Reason, &esc.Note, &esc.RaisedBy,
			&raisedAt, &esc.EscalatedTo, &resolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return swi.Escalation{}, swi.Process{}, nil, ErrNotFound
	}
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("read escalation: %w", err)
	}
	if resolvedAt.Valid {
		return swi.Escalation{}, swi.Process{}, nil, swi.ErrNoEscalation
	}
	esc.RaisedAt = fromMillis(raisedAt)

	if _, err := tx.ExecContext(ctx, `
		UPDATE escalations SET resolved_at = ?, resolved_by = ?, resolution_note = ? WHERE id = ?`,
		millis(now), performedBy, resolution, escalationID); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("resolve escalation: %w", err)
	}

	var stillOpen int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM escalations WHERE order_id = ? AND resolved_at IS NULL`, esc.OrderID).Scan(&stillOpen); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("count open escalations: %w", err)
	}

	number, err := orderNumberTx(ctx, tx, esc.OrderID)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	jobs := []swi.SyncJob{}
	if stillOpen == 0 {
		back := esc.ReturnStage
		if !swi.IsValidStage(back) || swi.IsTerminalStage(back) || back == swi.StageEscalation {
			back = swi.StageInControlWork
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE order_process SET stage = ?, stage_since = ?, escalated = 0, escalation_level = '',
			       version = version + 1, updated_by = ?, updated_at = ? WHERE order_id = ?`,
			string(back), millis(now), performedBy, millis(now), esc.OrderID); err != nil {
			return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("release escalation: %w", err)
		}
		if err := s.seedTasksTx(ctx, tx, esc.OrderID, back, now); err != nil {
			return swi.Escalation{}, swi.Process{}, nil, err
		}
		jobs, err = queueSyncJobsTx(ctx, tx, esc.OrderID, number, swi.StageEscalation, back, now)
		if err != nil {
			return swi.Escalation{}, swi.Process{}, nil, err
		}
	}

	action := fmt.Sprintf("escalation %d resolved", escalationID)
	if err := s.processEventTx(ctx, tx, esc.OrderID, action, swi.StageEscalation, esc.ReturnStage, "", resolution, performedBy, now); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return swi.Escalation{}, swi.Process{}, nil, fmt.Errorf("commit: %w", err)
	}

	updated, err := s.EscalationByID(ctx, escalationID)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	proc, err := s.GetProcess(ctx, esc.OrderID)
	if err != nil {
		return swi.Escalation{}, swi.Process{}, nil, err
	}
	return updated, proc, jobs, nil
}

// EscalationByID loads a single escalation.
func (s *Store) EscalationByID(ctx context.Context, id int64) (swi.Escalation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.order_id, o.order_number, e.stage, e.return_stage, e.level, e.reason, e.note,
		       e.raised_by, e.raised_at, e.escalated_to, e.resolved_at, e.resolved_by, e.resolution_note
		FROM escalations e JOIN orders o ON o.id = e.order_id WHERE e.id = ?`, id)
	return scanEscalation(row)
}

// ListEscalations lists escalations, optionally only the open ones.
func (s *Store) ListEscalations(ctx context.Context, openOnly bool, limit int) ([]swi.Escalation, error) {
	if limit <= 0 {
		limit = 200
	}
	query := `
		SELECT e.id, e.order_id, o.order_number, e.stage, e.return_stage, e.level, e.reason, e.note,
		       e.raised_by, e.raised_at, e.escalated_to, e.resolved_at, e.resolved_by, e.resolution_note
		FROM escalations e JOIN orders o ON o.id = e.order_id`
	if openOnly {
		query += ` WHERE e.resolved_at IS NULL`
	}
	query += ` ORDER BY e.raised_at DESC, e.id DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list escalations: %w", err)
	}
	defer rows.Close()

	out := []swi.Escalation{}
	for rows.Next() {
		e, err := scanEscalationRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEscalation(row *sql.Row) (swi.Escalation, error) {
	e, err := scanEscalationRows(row)
	if errors.Is(err, sql.ErrNoRows) {
		return swi.Escalation{}, ErrNotFound
	}
	return e, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEscalationRows(row rowScanner) (swi.Escalation, error) {
	var e swi.Escalation
	var raised int64
	var resolved sql.NullInt64
	if err := row.Scan(&e.ID, &e.OrderID, &e.OrderNumber, &e.Stage, &e.ReturnStage, &e.Level, &e.Reason, &e.Note,
		&e.RaisedBy, &raised, &e.EscalatedTo, &resolved, &e.ResolvedBy, &e.ResolutionNote); err != nil {
		return swi.Escalation{}, fmt.Errorf("scan escalation: %w", err)
	}
	e.RaisedAt = fromMillis(raised)
	e.ResolvedAt = nullableTime(resolved)
	e.Open = e.ResolvedAt == nil
	e.LevelLabel = swi.LevelLabel(e.Level)
	e.ReasonLabel = swi.ReasonLabel(e.Reason)
	return e, nil
}
// AutoEscalate walks the active processes and raises escalations for the ones
// that breached their SLA or dwelled in a stage for too long. It is meant to be
// called periodically (SWI_AUTO_ESCALATE_MINUTES) or from a scheduler, and is
// safe to run repeatedly: an order that is already escalated is skipped.
func (s *Store) AutoEscalate(ctx context.Context, now time.Time) ([]swi.Escalation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.order_number, o.status, o.target_completion_at, o.created_at, o.updated_at,
		       o.assigned_to, o.paused_seconds, p.stage, p.stage_since, p.escalated
		FROM orders o JOIN order_process p ON p.order_id = o.id
		WHERE p.escalated = 0 AND p.stage <> ?`, string(swi.StageCompleted))
	if err != nil {
		return nil, fmt.Errorf("scan processes for escalation: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		order order.Order
		stage swi.Stage
		since time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var o order.Order
		var tgt, created, updated, since int64
		var stage string
		var escalated int
		if err := rows.Scan(&o.ID, &o.OrderNumber, &o.Status, &tgt, &created, &updated, &o.Assignee,
			&o.PausedSeconds, &stage, &since, &escalated); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		o.TargetCompletion = fromMillis(tgt)
		o.CreatedAt = fromMillis(created)
		o.UpdatedAt = fromMillis(updated)
		candidates = append(candidates, candidate{order: o, stage: swi.Stage(stage), since: fromMillis(since)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	raised := []swi.Escalation{}
	for _, c := range candidates {
		proc := swi.Process{OrderID: c.order.ID, Stage: c.stage, StageSince: c.since}
		sla := order.ComputeSLA(c.order.TargetCompletion, now)
		level, reason, needs := swi.NeedsEscalation(c.order, proc, sla, now)
		if !needs {
			continue
		}
		esc, _, _, err := s.EscalateProcess(ctx, c.order.ID, level, reason,
			"Automatisch geëscaleerd: "+swi.ReasonLabel(reason), "", "system", now)
		if err != nil {
			return raised, fmt.Errorf("auto escalate order %d: %w", c.order.ID, err)
		}
		raised = append(raised, esc)
	}
	return raised, nil
}

// ProcessMetrics aggregates the process board numbers used by process
// management: work per stage, escalations, open tasks, outstanding external
// updates and the age of the active work.
func (s *Store) ProcessMetrics(ctx context.Context, now time.Time) (swi.Metrics, error) {
	m := swi.Metrics{ByStage: map[swi.Stage]int{}, SyncFailedBySystem: map[swi.System]int{}, GeneratedAt: now.UTC()}

	rows, err := s.db.QueryContext(ctx, `
		SELECT stage, escalated, stage_since FROM order_process`)
	if err != nil {
		return m, fmt.Errorf("read process metrics: %w", err)
	}
	defer rows.Close()

	var ageSum int64
	for rows.Next() {
		var stage string
		var escalated int
		var since int64
		if err := rows.Scan(&stage, &escalated, &since); err != nil {
			return m, fmt.Errorf("scan metrics row: %w", err)
		}
		st := swi.Stage(stage)
		m.Total++
		if st != swi.StageCompleted {
			m.ByStage[st]++
			if escalated != 0 {
				m.Escalated++
			}
			age := now.Sub(fromMillis(since))
			if age < 0 {
				age = 0
			}
			ageSum += int64(age.Seconds())
			if int64(age.Seconds()) > m.OldestActiveAgeSeconds {
				m.OldestActiveAgeSeconds = int64(age.Seconds())
			}
		}
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	if active := m.Total - m.ByStage[swi.StageCompleted]; active > 0 {
		m.AvgActiveAgeSeconds = ageSum / int64(active)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM process_tasks WHERE done = 0`).Scan(&m.TasksOpen); err != nil {
		return m, fmt.Errorf("count open tasks: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN status = 'PENDING' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END), 0)
		 FROM external_sync_jobs`).Scan(&m.SyncPending, &m.SyncFailed); err != nil {
		return m, fmt.Errorf("count sync jobs: %w", err)
	}

	jobRows, err := s.db.QueryContext(ctx,
		`SELECT system, COUNT(*) FROM external_sync_jobs WHERE status = 'FAILED' GROUP BY system`)
	if err != nil {
		return m, fmt.Errorf("count failed sync jobs: %w", err)
	}
	defer jobRows.Close()
	for jobRows.Next() {
		var system string
		var count int
		if err := jobRows.Scan(&system, &count); err != nil {
			return m, fmt.Errorf("scan failed sync jobs: %w", err)
		}
		m.SyncFailedBySystem[swi.System(system)] = count
	}
	if err := jobRows.Err(); err != nil {
		return m, err
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM process_events
		WHERE action LIKE 'process stage % -> Completed' AND created_at >= ?`,
		millis(now.Add(-24*time.Hour))).Scan(&m.CompletedLast24h); err != nil {
		return m, fmt.Errorf("count completions: %w", err)
	}
	return m, nil
}

// OverdueProcesses returns the active processes whose stage budget is exceeded,
// used by the process-management dashboard.
func (s *Store) OverdueProcesses(ctx context.Context, now time.Time) ([]swi.Process, error) {
	procs, err := s.ListProcesses(ctx, nil, 0)
	if err != nil {
		return nil, err
	}
	out := []swi.Process{}
	for _, p := range procs {
		if swi.IsTerminalStage(p.Stage) {
			continue
		}
		budget := swi.StageBudget(p.Stage)
		if budget <= 0 {
			continue
		}
		if now.Sub(p.StageSince) > budget {
			out = append(out, p)
		}
	}
	return out, nil
}
const syncJobCols = `SELECT id, order_id, order_number, system, operation, direction, entity, reason, status,
	payload, response, attempts, last_error, idempotency_key, created_at, updated_at, completed_at
	FROM external_sync_jobs`

// SyncJobByID loads a single external sync job.
func (s *Store) SyncJobByID(ctx context.Context, id int64) (swi.SyncJob, error) {
	row := s.db.QueryRowContext(ctx, syncJobCols+` WHERE id = ?`, id)
	j, err := scanSyncJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return swi.SyncJob{}, ErrNotFound
	}
	return j, err
}

// ListSyncJobs lists the external updates queued for AFAS, Omnitracker, Intune,
// Knox and Apple Business Manager.
func (s *Store) ListSyncJobs(ctx context.Context, f swi.SyncJobFilter) ([]swi.SyncJob, error) {
	query := syncJobCols
	conds := []string{}
	args := []any{}
	if f.System != "" {
		conds = append(conds, "system = ?")
		args = append(args, string(f.System))
	}
	if f.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, string(f.Status))
	} else if f.PendingOnly {
		conds = append(conds, "status = 'PENDING'")
	}
	if f.OrderID > 0 {
		conds = append(conds, "order_id = ?")
		args = append(args, f.OrderID)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY created_at ASC, id ASC LIMIT ?"
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sync jobs: %w", err)
	}
	defer rows.Close()

	out := []swi.SyncJob{}
	for rows.Next() {
		j, err := scanSyncJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ClaimSyncJobs hands the pending jobs of one system to the integration worker
// and marks them in progress, so two workers never pick up the same job.
func (s *Store) ClaimSyncJobs(ctx context.Context, system swi.System, limit int, now time.Time) ([]swi.SyncJob, error) {
	if limit <= 0 {
		limit = 20
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM external_sync_jobs WHERE status = 'PENDING' AND system = ? ORDER BY created_at ASC, id ASC LIMIT ?`,
		string(system), limit)
	if err != nil {
		return nil, fmt.Errorf("select pending sync jobs: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan pending sync job: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return []swi.SyncJob{}, nil
	}

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE external_sync_jobs SET status = 'IN_PROGRESS', attempts = attempts + 1, updated_at = ?
			WHERE id = ? AND status = 'PENDING'`, millis(now), id); err != nil {
			return nil, fmt.Errorf("claim sync job %d: %w", id, err)
		}
	}
	claimed := make([]swi.SyncJob, 0, len(ids))
	for _, id := range ids {
		j, err := scanSyncJob(tx.QueryRowContext(ctx, syncJobCols+` WHERE id = ?`, id))
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, j)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return claimed, nil
}
// CompleteSyncJob records the result reported by the integration worker.
func (s *Store) CompleteSyncJob(ctx context.Context, id int64, status swi.SyncStatus, response, errMsg string, now time.Time) (swi.SyncJob, error) {
	current, err := s.SyncJobByID(ctx, id)
	if err != nil {
		return swi.SyncJob{}, err
	}
	if current.Status == swi.SyncDone || current.Status == swi.SyncFailed || current.Status == swi.SyncSkipped {
		return swi.SyncJob{}, swi.ErrSyncJobDecided
	}

	var completed any
	if status != swi.SyncPending && status != swi.SyncInProgress {
		completed = millis(now)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE external_sync_jobs SET status = ?, response = ?, last_error = ?, completed_at = ?, updated_at = ?
		WHERE id = ?`, string(status), response, errMsg, completed, millis(now), id); err != nil {
		return swi.SyncJob{}, fmt.Errorf("complete sync job: %w", err)
	}
	return s.SyncJobByID(ctx, id)
}

// RetrySyncJob puts a failed job back in the queue.
func (s *Store) RetrySyncJob(ctx context.Context, id int64, now time.Time) (swi.SyncJob, error) {
	current, err := s.SyncJobByID(ctx, id)
	if err != nil {
		return swi.SyncJob{}, err
	}
	if current.Status != swi.SyncFailed && current.Status != swi.SyncInProgress && current.Status != swi.SyncSkipped {
		return swi.SyncJob{}, swi.ErrSyncJobDecided
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE external_sync_jobs SET status = 'PENDING', last_error = '', response = '', completed_at = NULL, updated_at = ?
		WHERE id = ?`, millis(now), id); err != nil {
		return swi.SyncJob{}, fmt.Errorf("retry sync job: %w", err)
	}
	return s.SyncJobByID(ctx, id)
}

// QueueOrderSync manually queues an update for one system (for example an
// Omnitracker re-push after a failure).
func (s *Store) QueueOrderSync(ctx context.Context, orderID int64, system swi.System, op swi.Operation, entity, reason, performedBy string, now time.Time) (swi.SyncJob, error) {
	o, err := s.GetOrder(ctx, orderID)
	if err != nil {
		return swi.SyncJob{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return swi.SyncJob{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	key := fmt.Sprintf("ord:%d:manual:%s:%s:%d", orderID, strings.ToLower(string(system)), strings.ToLower(string(op)), millis(now))
	payload, err := json.Marshal(map[string]any{
		"orderId": orderID, "orderNumber": o.OrderNumber, "operation": op, "entity": entity, "manual": true,
	})
	if err != nil {
		return swi.SyncJob{}, fmt.Errorf("marshal payload: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO external_sync_jobs (order_id, order_number, system, operation, direction, entity, reason, status, payload, attempts, idempotency_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'OUTBOUND', ?, ?, 'PENDING', ?, 0, ?, ?, ?)`,
		orderID, o.OrderNumber, string(system), string(op), entity, reason, string(payload), key, millis(now), millis(now))
	if err != nil {
		return swi.SyncJob{}, fmt.Errorf("queue sync job: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return swi.SyncJob{}, fmt.Errorf("last insert id: %w", err)
	}
	if err := s.processEventTx(ctx, tx, orderID, fmt.Sprintf("sync job queued for %s", system), "", "", "", reason, performedBy, now); err != nil {
		return swi.SyncJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return swi.SyncJob{}, fmt.Errorf("commit: %w", err)
	}
	return s.SyncJobByID(ctx, id)
}

// ImportOrder implements the AFAS order import: step 1 of the SWI process. It
// creates the order, stores the AFAS/Omnitracker reference data, starts the
// process in OrderImport and queues the inbound AFAS read for the order lines.
// An existing order with the same number is returned as-is (idempotent import),
// so re-running the AFAS feed never duplicates work.
func (s *Store) ImportOrder(ctx context.Context, req swi.ImportRequest, slaTarget time.Duration, performedBy string, now time.Time) (order.Order, bool, error) {
	if performedBy == "" {
		performedBy = "system"
	}
	number := strings.TrimSpace(req.OrderNumber)
	target := now.Add(slaTarget)
	if req.TargetCompletion != nil && req.TargetCompletion.After(now) {
		target = req.TargetCompletion.UTC()
	}

	created, err := s.CreateOrder(ctx, number, target, now, performedBy)
	if errors.Is(err, ErrDuplicateOrderNumber) {
		existing, lookupErr := s.OrderByNumber(ctx, number)
		if lookupErr != nil {
			return order.Order{}, false, lookupErr
		}
		if _, err := s.EnsureProcess(ctx, existing.ID, performedBy, now); err != nil {
			return order.Order{}, false, err
		}
		return existing, false, nil
	}
	if err != nil {
		return order.Order{}, false, err
	}

	updated, err := s.UpdateOmnitrackerInfo(ctx, created.ID, order.OmnitrackerInfoRequest{
		DebitNumber:       req.DebitNumber,
		CustomerName:      req.CustomerName,
		OmnitrackerTicket: req.OmnitrackerTicket,
		Device:            req.Device,
		AssetNumber:       req.AssetNumber,
		Configuration:     req.Configuration,
		SIID:              req.SIID,
	}, now)
	if err != nil {
		return order.Order{}, false, err
	}

	proc, err := s.EnsureProcess(ctx, created.ID, performedBy, now)
	if err != nil {
		return order.Order{}, false, err
	}
	if _, err := s.QueueOrderSync(ctx, created.ID, swi.SystemAFAS, swi.OpRead, "order-lines",
		"Orderregels en debiteurnummer uit AFAS ophalen (import "+sourceOr(req.Source, "AFAS")+")", performedBy, now); err != nil {
		return order.Order{}, false, err
	}
	if proc.Stage != swi.StageOrderImport {
		return order.Order{}, false, fmt.Errorf("imported order %d started in unexpected stage %q", created.ID, proc.Stage)
	}
	return updated, true, nil
}

func sourceOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func scanSyncJob(row rowScanner) (swi.SyncJob, error) {
	var j swi.SyncJob
	var payload string
	var created, updated int64
	var completed sql.NullInt64
	if err := row.Scan(&j.ID, &j.OrderID, &j.OrderNumber, &j.System, &j.Operation, &j.Direction, &j.Entity, &j.Reason,
		&j.Status, &payload, &j.Response, &j.Attempts, &j.LastError, &j.IdempotencyKey, &created, &updated, &completed); err != nil {
		return swi.SyncJob{}, err
	}
	if strings.TrimSpace(payload) != "" {
		j.Payload = json.RawMessage(payload)
	}
	j.CreatedAt = fromMillis(created)
	j.UpdatedAt = fromMillis(updated)
	j.CompletedAt = nullableTime(completed)
	j.SystemLabel = swi.SystemLabel(j.System)
	j.StatusLabel = swi.SyncStatusLabel(j.Status)
	return j, nil
}