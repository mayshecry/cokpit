package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const Schema = `
CREATE TABLE IF NOT EXISTS orders (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    order_number        TEXT    NOT NULL UNIQUE,
    status              TEXT    NOT NULL,
    target_completion_at BIGINT NOT NULL,
    created_at          BIGINT  NOT NULL,
    updated_at          BIGINT  NOT NULL,
    assigned_to         TEXT    NOT NULL DEFAULT '',
    paused_seconds      INTEGER NOT NULL DEFAULT 0,
    debit_number        TEXT    NOT NULL DEFAULT '',
    customer_name       TEXT    NOT NULL DEFAULT '',
    omnitracker_ticket  TEXT    NOT NULL DEFAULT '',
    device              TEXT    NOT NULL DEFAULT '',
    asset_number        TEXT    NOT NULL DEFAULT '',
    configuration       TEXT    NOT NULL DEFAULT '',
    workflow_profile_id INTEGER NOT NULL DEFAULT 0,
    workflow_profile_name TEXT NOT NULL DEFAULT '',
    si_id               INTEGER REFERENCES system_integrations(id),
    barcode             TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_created ON orders(created_at);
CREATE INDEX IF NOT EXISTS idx_orders_barcode ON orders(barcode);
CREATE INDEX IF NOT EXISTS idx_orders_omnitracker ON orders(omnitracker_ticket);

CREATE TABLE IF NOT EXISTS prep_devices (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    device_key    TEXT    NOT NULL UNIQUE,
    computer_name TEXT    NOT NULL,
    summary       TEXT    NOT NULL,
    raw           TEXT    NOT NULL,
    file_name     TEXT    NOT NULL DEFAULT '',
    uploader_ip   TEXT    NOT NULL DEFAULT '',
    runs          INTEGER NOT NULL DEFAULT 1,
    first_seen_at BIGINT  NOT NULL,
    last_seen_at  BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_prep_devices_name ON prep_devices(computer_name);

CREATE TABLE IF NOT EXISTS holds (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    reason      TEXT    NOT NULL,
    created_by  TEXT    NOT NULL,
    resolved_at BIGINT,
    created_at  BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_holds_order ON holds(order_id);
CREATE INDEX IF NOT EXISTS idx_holds_active ON holds(order_id, resolved_at);

CREATE TABLE IF NOT EXISTS qc_checks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    status       TEXT    NOT NULL CHECK (status IN ('PASS','FAIL')),
    inspector_id TEXT    NOT NULL,
    notes        TEXT    NOT NULL DEFAULT '',
    created_at   BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_qc_checks_order ON qc_checks(order_id);

CREATE TABLE IF NOT EXISTS audit_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    action       TEXT    NOT NULL,
    performed_by TEXT    NOT NULL DEFAULT 'system',
    timestamp    BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_order ON audit_logs(order_id, timestamp);

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    display_name  TEXT    NOT NULL DEFAULT '',
    role          TEXT    NOT NULL DEFAULT 'viewer',
    created_at    BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

CREATE TABLE IF NOT EXISTS api_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token      TEXT    NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at BIGINT  NOT NULL,
    expires_at BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_token ON api_tokens(token);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

CREATE TABLE IF NOT EXISTS pair_codes (
    code       TEXT   PRIMARY KEY,
    username   TEXT   NOT NULL DEFAULT '',
    created_by TEXT   NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    used_at    BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_pair_codes_state ON pair_codes(username, used_at, expires_at);

CREATE TABLE IF NOT EXISTS comments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    author_id    TEXT    NOT NULL,
    author_name  TEXT    NOT NULL DEFAULT '',
    body         TEXT    NOT NULL,
    created_at   BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_comments_order ON comments(order_id, created_at);

CREATE TABLE IF NOT EXISTS comment_mentions (
    comment_id INTEGER NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    username   TEXT    NOT NULL,
    PRIMARY KEY (comment_id, username)
);

CREATE TABLE IF NOT EXISTS notifications (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_name   TEXT    NOT NULL,
    kind        TEXT    NOT NULL,
    order_id    INTEGER NOT NULL,
    order_number TEXT   NOT NULL DEFAULT '',
    ref_id      INTEGER NOT NULL DEFAULT 0,
    body        TEXT    NOT NULL DEFAULT '',
    read_at     BIGINT,
    created_at  BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_name, read_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_unique
    ON notifications(user_name, kind, ref_id);

CREATE TABLE IF NOT EXISTS scan_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id       INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    code           TEXT    NOT NULL,
    scanned_by     TEXT    NOT NULL,
    action         TEXT    NOT NULL DEFAULT 'LOOKUP',
    created_at     BIGINT  NOT NULL,
    client_event_id TEXT   NOT NULL DEFAULT '',
    received_at    BIGINT
);

CREATE INDEX IF NOT EXISTS idx_scan_events_order ON scan_events(order_id, created_at);
CREATE INDEX IF NOT EXISTS idx_scan_events_code ON scan_events(code);

CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS day_goals (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    day        TEXT    NOT NULL,
    username   TEXT    NOT NULL,
    goal       TEXT    NOT NULL DEFAULT '',
    updated_by TEXT    NOT NULL DEFAULT '',
    updated_at BIGINT  NOT NULL,
    sent_at    BIGINT,
    UNIQUE(day, username)
);

CREATE TABLE IF NOT EXISTS work_sessions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    username    TEXT    NOT NULL,
    started_at  BIGINT  NOT NULL,
    ended_at    BIGINT,
    step        INTEGER NOT NULL DEFAULT 0,
    checks      TEXT    NOT NULL DEFAULT '{}',
    completed   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_work_sessions_order ON work_sessions(order_id, id);

CREATE TABLE IF NOT EXISTS checklist_items (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    artikel      TEXT    NOT NULL,
    omschrijving TEXT    NOT NULL DEFAULT '',
    locatie      TEXT    NOT NULL DEFAULT '',
    aantal       INTEGER NOT NULL DEFAULT 0,
    checked_by   TEXT,
    checked_at   BIGINT,
    created_at   BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_checklist_order ON checklist_items(order_id, seq);

CREATE TABLE IF NOT EXISTS products (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    code        TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_by  TEXT    NOT NULL DEFAULT '',
    created_at  BIGINT  NOT NULL,
    updated_at  BIGINT  NOT NULL,
    department  TEXT    NOT NULL DEFAULT '',
    approved_by TEXT    NOT NULL DEFAULT '',
    approved_at BIGINT
);

CREATE TABLE IF NOT EXISTS manual_blocks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    assignee   TEXT    NOT NULL DEFAULT '',
    created_at BIGINT  NOT NULL,
    updated_at BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_manual_blocks_product ON manual_blocks(product_id, seq);

CREATE TABLE IF NOT EXISTS order_manuals (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id   INTEGER NOT NULL REFERENCES products(id),
    product_code TEXT    NOT NULL,
    product_name TEXT    NOT NULL DEFAULT '',
    created_by   TEXT    NOT NULL,
    created_at   BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_manuals_order ON order_manuals(order_id);

CREATE TABLE IF NOT EXISTS order_manual_blocks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    manual_id   INTEGER NOT NULL REFERENCES order_manuals(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    body        TEXT    NOT NULL DEFAULT '',
    assignee    TEXT    NOT NULL DEFAULT '',
    answer      TEXT    CHECK (answer IN ('YES','NO')),
    answered_by TEXT,
    answered_at BIGINT,
    flagged     INTEGER NOT NULL DEFAULT 0,
    flagged_by  TEXT    NOT NULL DEFAULT '',
    flagged_at  BIGINT,
    flag_reason TEXT    NOT NULL DEFAULT '',
    created_at  BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_manual_blocks_manual ON order_manual_blocks(manual_id, seq);

CREATE TABLE IF NOT EXISTS manual_tick_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    manual_id  INTEGER NOT NULL REFERENCES order_manuals(id) ON DELETE CASCADE,
    block_id   INTEGER NOT NULL REFERENCES order_manual_blocks(id) ON DELETE CASCADE,
    action     TEXT    NOT NULL CHECK (action IN ('YES','NO','CLEAR','FLAG','UNFLAG')),
    username   TEXT    NOT NULL,
    note       TEXT    NOT NULL DEFAULT '',
    created_at BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_manual_tick_log_block ON manual_tick_log(block_id, created_at);
CREATE INDEX IF NOT EXISTS idx_manual_tick_log_manual ON manual_tick_log(manual_id, created_at);
CREATE TABLE IF NOT EXISTS customer_workflows (
    customer_name TEXT PRIMARY KEY,
    steps_json    TEXT NOT NULL,
    updated_by    TEXT NOT NULL DEFAULT '',
    updated_at    BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS workflow_profiles (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_name TEXT NOT NULL,
    name          TEXT NOT NULL COLLATE NOCASE,
    steps_json    TEXT NOT NULL,
    updated_by    TEXT NOT NULL DEFAULT '',
    updated_at    BIGINT NOT NULL,
    UNIQUE(customer_name, name)
);

CREATE INDEX IF NOT EXISTS idx_workflow_profiles_customer ON workflow_profiles(customer_name, name);

CREATE TABLE IF NOT EXISTS customers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    number     TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL,
    created_at BIGINT  NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    code        TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_at  BIGINT  NOT NULL,
    updated_at  BIGINT  NOT NULL,
    UNIQUE (customer_id, code)
);

CREATE TABLE IF NOT EXISTS system_integrations (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    code                TEXT    NOT NULL UNIQUE,
    name                TEXT    NOT NULL,
    description         TEXT    NOT NULL DEFAULT '',
    status              TEXT    NOT NULL,
    version             INTEGER NOT NULL DEFAULT 0,
    environment         TEXT    NOT NULL DEFAULT 'PRODUCTIE',
    primary_project_id  INTEGER NOT NULL REFERENCES projects(id),
    created_by          TEXT    NOT NULL DEFAULT '',
    created_at          BIGINT  NOT NULL,
    updated_at          BIGINT  NOT NULL
);

CREATE TABLE IF NOT EXISTS si_project_links (
    si_id      INTEGER NOT NULL REFERENCES system_integrations(id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    is_primary INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (si_id, project_id)
);

CREATE TABLE IF NOT EXISTS si_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    si_id        INTEGER NOT NULL REFERENCES system_integrations(id) ON DELETE CASCADE,
    action       TEXT    NOT NULL,
    from_status  TEXT,
    to_status    TEXT,
    version      INTEGER NOT NULL DEFAULT 0,
    note         TEXT    NOT NULL DEFAULT '',
    performed_by  TEXT    NOT NULL DEFAULT 'system',
    timestamp    BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_si_events_si ON si_events(si_id, timestamp);

CREATE TABLE IF NOT EXISTS project_checklist_items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL DEFAULT 0,
    label       TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    checked_by  TEXT,
    checked_at  BIGINT,
    created_at  BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_project_checklist_project ON project_checklist_items(project_id, seq);

CREATE TABLE IF NOT EXISTS departments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT    NOT NULL DEFAULT '',
    permissions TEXT    NOT NULL DEFAULT '[]',
    created_at  BIGINT  NOT NULL
);

CREATE TABLE IF NOT EXISTS user_departments (
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    department_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, department_id)
);

CREATE INDEX IF NOT EXISTS idx_user_departments_user ON user_departments(user_id);
CREATE INDEX IF NOT EXISTS idx_user_departments_dept ON user_departments(department_id);

CREATE TABLE IF NOT EXISTS order_process (
    order_id         INTEGER PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    stage            TEXT    NOT NULL,
    stage_since      BIGINT  NOT NULL,
    escalated        INTEGER NOT NULL DEFAULT 0,
    escalation_level TEXT    NOT NULL DEFAULT '',
    version          INTEGER NOT NULL DEFAULT 1,
    updated_by       TEXT    NOT NULL DEFAULT 'system',
    updated_at       BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_process_stage ON order_process(stage, escalated);

CREATE TABLE IF NOT EXISTS process_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    action       TEXT    NOT NULL,
    from_stage   TEXT    NOT NULL DEFAULT '',
    to_stage     TEXT    NOT NULL DEFAULT '',
    level        TEXT    NOT NULL DEFAULT '',
    note         TEXT    NOT NULL DEFAULT '',
    performed_by TEXT    NOT NULL DEFAULT 'system',
    created_at   BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_process_events_order ON process_events(order_id, created_at);

CREATE TABLE IF NOT EXISTS process_tasks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    stage       TEXT    NOT NULL,
    seq         INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    role        TEXT    NOT NULL DEFAULT '',
    required    INTEGER NOT NULL DEFAULT 0,
    done        INTEGER NOT NULL DEFAULT 0,
    done_by     TEXT    NOT NULL DEFAULT '',
    done_at     BIGINT,
    created_at  BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_process_tasks_order ON process_tasks(order_id, stage, seq);

CREATE TABLE IF NOT EXISTS escalations (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id        INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    stage           TEXT    NOT NULL,
    return_stage    TEXT    NOT NULL DEFAULT '',
    level           TEXT    NOT NULL,
    reason          TEXT    NOT NULL,
    note            TEXT    NOT NULL DEFAULT '',
    raised_by       TEXT    NOT NULL DEFAULT 'system',
    raised_at       BIGINT  NOT NULL,
    escalated_to    TEXT    NOT NULL DEFAULT '',
    resolved_at     BIGINT,
    resolved_by     TEXT    NOT NULL DEFAULT '',
    resolution_note TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_escalations_order ON escalations(order_id, resolved_at);
CREATE INDEX IF NOT EXISTS idx_escalations_open ON escalations(resolved_at);

CREATE TABLE IF NOT EXISTS external_sync_jobs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id        INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    order_number    TEXT    NOT NULL DEFAULT '',
    system          TEXT    NOT NULL,
    operation       TEXT    NOT NULL,
    direction       TEXT    NOT NULL,
    entity          TEXT    NOT NULL DEFAULT '',
    reason          TEXT    NOT NULL DEFAULT '',
    status          TEXT    NOT NULL DEFAULT 'PENDING',
    payload         TEXT    NOT NULL DEFAULT '{}',
    response        TEXT    NOT NULL DEFAULT '',
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT    NOT NULL DEFAULT '',
    idempotency_key TEXT    NOT NULL UNIQUE,
    created_at      BIGINT  NOT NULL,
    updated_at      BIGINT  NOT NULL,
    completed_at    BIGINT
);

CREATE INDEX IF NOT EXISTS idx_sync_jobs_status ON external_sync_jobs(status, system);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_order ON external_sync_jobs(order_id, created_at);

CREATE TABLE IF NOT EXISTS automation_rules (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT    NOT NULL UNIQUE,
    trigger_type      TEXT    NOT NULL CHECK (trigger_type IN ('QC_FAILED','SLA_BREACHED','HOLD_AGING')),
    threshold_minutes INTEGER NOT NULL DEFAULT 0,
    action_type       TEXT    NOT NULL CHECK (action_type IN ('NOTIFY','ASSIGN','ESCALATE')),
    targets_json      TEXT    NOT NULL DEFAULT '[]',
    teams_json        TEXT    NOT NULL DEFAULT '[]',
    customers_json    TEXT    NOT NULL DEFAULT '[]',
    excluded_customers_json TEXT NOT NULL DEFAULT '[]',
    departments_json  TEXT    NOT NULL DEFAULT '[]',
    products_json     TEXT    NOT NULL DEFAULT '[]',
    customer_targets_json TEXT NOT NULL DEFAULT '{}',
    customer_teams_json   TEXT NOT NULL DEFAULT '{}',
    message           TEXT    NOT NULL DEFAULT '',
    escalation_level  TEXT    NOT NULL DEFAULT 'L1',
    enabled           INTEGER NOT NULL DEFAULT 1,
    created_by        TEXT    NOT NULL DEFAULT '',
    created_at        BIGINT  NOT NULL,
    updated_at        BIGINT  NOT NULL
);

CREATE TABLE IF NOT EXISTS automation_runs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id      INTEGER NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    trigger_key  TEXT    NOT NULL,
    action_type  TEXT    NOT NULL,
    status       TEXT    NOT NULL CHECK (status IN ('PENDING','SUCCESS','FAILED')),
    detail       TEXT    NOT NULL DEFAULT '',
    error        TEXT    NOT NULL DEFAULT '',
    created_at   BIGINT  NOT NULL,
    UNIQUE(rule_id, trigger_key)
);

CREATE INDEX IF NOT EXISTS idx_automation_runs_created ON automation_runs(created_at, id);
CREATE INDEX IF NOT EXISTS idx_automation_runs_order ON automation_runs(order_id, created_at);

CREATE TABLE IF NOT EXISTS corrective_actions (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id          INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    qc_check_id       INTEGER REFERENCES qc_checks(id) ON DELETE SET NULL,
    title             TEXT    NOT NULL,
    fail_reason       TEXT    NOT NULL DEFAULT '',
    owner             TEXT    NOT NULL,
    due_at            BIGINT  NOT NULL,
    root_cause        TEXT    NOT NULL DEFAULT '',
    action_plan       TEXT    NOT NULL DEFAULT '',
    resolution        TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL CHECK (status IN ('OPEN','IN_PROGRESS','PENDING_VERIFICATION','CLOSED')),
    created_by        TEXT    NOT NULL,
    created_at        BIGINT  NOT NULL,
    updated_at        BIGINT  NOT NULL,
    verified_by       TEXT    NOT NULL DEFAULT '',
    verified_at       BIGINT,
    verification_note TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_capa_queue ON corrective_actions(status, due_at);
CREATE INDEX IF NOT EXISTS idx_capa_owner ON corrective_actions(owner, status);
CREATE INDEX IF NOT EXISTS idx_capa_order ON corrective_actions(order_id, created_at);

CREATE TABLE IF NOT EXISTS capa_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    capa_id      INTEGER NOT NULL REFERENCES corrective_actions(id) ON DELETE CASCADE,
    action       TEXT    NOT NULL,
    note         TEXT    NOT NULL DEFAULT '',
    performed_by TEXT    NOT NULL,
    created_at   BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_capa_events_action ON capa_events(capa_id, created_at, id);
`

func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if _, err := conn.ExecContext(ctx, Schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(ctx, conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return conn, nil
}

func ensureAutomationScopeColumns(ctx context.Context, conn *sql.DB) error {
	existing, err := tableColumns(ctx, conn, "automation_rules")
	if err != nil {
		return err
	}
	cols := map[string]string{
		"teams_json":              `ALTER TABLE automation_rules ADD COLUMN teams_json TEXT NOT NULL DEFAULT '[]'`,
		"customers_json":          `ALTER TABLE automation_rules ADD COLUMN customers_json TEXT NOT NULL DEFAULT '[]'`,
		"excluded_customers_json": `ALTER TABLE automation_rules ADD COLUMN excluded_customers_json TEXT NOT NULL DEFAULT '[]'`,
		"departments_json":        `ALTER TABLE automation_rules ADD COLUMN departments_json TEXT NOT NULL DEFAULT '[]'`,
		"products_json":           `ALTER TABLE automation_rules ADD COLUMN products_json TEXT NOT NULL DEFAULT '[]'`,
		"customer_targets_json":   `ALTER TABLE automation_rules ADD COLUMN customer_targets_json TEXT NOT NULL DEFAULT '{}'`,
		"customer_teams_json":     `ALTER TABLE automation_rules ADD COLUMN customer_teams_json TEXT NOT NULL DEFAULT '{}'`,
	}
	for name, ddl := range cols {
		if existing[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add column automation_rules.%s: %w", name, err)
		}
	}
	return nil
}

func migrate(ctx context.Context, conn *sql.DB) error {
	existing, err := tableColumns(ctx, conn, "orders")
	if err != nil {
		return err
	}
	cols := map[string]string{
		"assigned_to":    `ALTER TABLE orders ADD COLUMN assigned_to TEXT NOT NULL DEFAULT ''`,
		"paused_seconds": `ALTER TABLE orders ADD COLUMN paused_seconds INTEGER NOT NULL DEFAULT 0`,
	}
	for name, ddl := range cols {
		if existing[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add column %s: %w", name, err)
		}
	}

	if err := ensureAutomationScopeColumns(ctx, conn); err != nil {
		return err
	}

	blockCols, err := tableColumns(ctx, conn, "order_manual_blocks")
	if err != nil {
		return err
	}
	qcCols, err := tableColumns(ctx, conn, "qc_checks")
	if err != nil {
		return err
	}
	qcDDL := map[string]string{
		"fail_reason": `ALTER TABLE qc_checks ADD COLUMN fail_reason TEXT NOT NULL DEFAULT ''`,
		"tech":        `ALTER TABLE qc_checks ADD COLUMN tech TEXT NOT NULL DEFAULT ''`,
	}
	for name, ddl := range qcDDL {
		if qcCols[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add column %s: %w", name, err)
		}
	}

	blockDDL := map[string]string{
		"flagged":     `ALTER TABLE order_manual_blocks ADD COLUMN flagged INTEGER NOT NULL DEFAULT 0`,
		"flagged_by":  `ALTER TABLE order_manual_blocks ADD COLUMN flagged_by TEXT NOT NULL DEFAULT ''`,
		"flagged_at":  `ALTER TABLE order_manual_blocks ADD COLUMN flagged_at BIGINT`,
		"flag_reason": `ALTER TABLE order_manual_blocks ADD COLUMN flag_reason TEXT NOT NULL DEFAULT ''`,
	}
	for name, ddl := range blockDDL {
		if blockCols[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add column %s: %w", name, err)
		}
	}

	prodCols, err := tableColumns(ctx, conn, "products")
	if err != nil {
		return err
	}
	prodDDL := map[string]string{
		"department":  `ALTER TABLE products ADD COLUMN department TEXT NOT NULL DEFAULT ''`,
		"approved_by": `ALTER TABLE products ADD COLUMN approved_by TEXT NOT NULL DEFAULT ''`,
		"approved_at": `ALTER TABLE products ADD COLUMN approved_at BIGINT`,
	}
	for name, ddl := range prodDDL {
		if prodCols[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add column %s: %w", name, err)
		}
	}

	custCols, err := tableColumns(ctx, conn, "customers")
	if err != nil {
		return err
	}
	if !custCols["assigned_to"] {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE customers ADD COLUMN assigned_to TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add column assigned_to: %w", err)
		}
	}

	logCols, err := tableColumns(ctx, conn, "manual_tick_log")
	if err != nil {
		return err
	}
	if !logCols["note"] {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE manual_tick_log ADD COLUMN note TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add column note: %w", err)
		}
	}

	orderCols, err := tableColumns(ctx, conn, "orders")
	if err != nil {
		return err
	}
	orderMigrations := map[string]string{
		"debit_number":            `ALTER TABLE orders ADD COLUMN debit_number TEXT NOT NULL DEFAULT ''`,
		"customer_name":           `ALTER TABLE orders ADD COLUMN customer_name TEXT NOT NULL DEFAULT ''`,
		"omnitracker_ticket":      `ALTER TABLE orders ADD COLUMN omnitracker_ticket TEXT NOT NULL DEFAULT ''`,
		"device":                  `ALTER TABLE orders ADD COLUMN device TEXT NOT NULL DEFAULT ''`,
		"asset_number":            `ALTER TABLE orders ADD COLUMN asset_number TEXT NOT NULL DEFAULT ''`,
		"configuration":           `ALTER TABLE orders ADD COLUMN configuration TEXT NOT NULL DEFAULT ''`,
		"workflow_profile_id":     `ALTER TABLE orders ADD COLUMN workflow_profile_id INTEGER NOT NULL DEFAULT 0`,
		"workflow_profile_name":   `ALTER TABLE orders ADD COLUMN workflow_profile_name TEXT NOT NULL DEFAULT ''`,
		"si_id":                   `ALTER TABLE orders ADD COLUMN si_id INTEGER`,
		"barcode":                 `ALTER TABLE orders ADD COLUMN barcode TEXT NOT NULL DEFAULT ''`,
		"split_from_order_id":     `ALTER TABLE orders ADD COLUMN split_from_order_id INTEGER`,
		"split_reason":            `ALTER TABLE orders ADD COLUMN split_reason TEXT NOT NULL DEFAULT ''`,
	}
	for name, ddl := range orderMigrations {
		if orderCols[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add column %s: %w", name, err)
		}
	}

	scanCols, err := tableColumns(ctx, conn, "scan_events")
	if err != nil {
		return err
	}
	scanMigrations := map[string]string{
		"client_event_id": `ALTER TABLE scan_events ADD COLUMN client_event_id TEXT NOT NULL DEFAULT ''`,
		"received_at":     `ALTER TABLE scan_events ADD COLUMN received_at BIGINT`,
	}
	for name, ddl := range scanMigrations {
		if scanCols[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add scan column %s: %w", name, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_scan_events_client_event
		ON scan_events(scanned_by, client_event_id) WHERE client_event_id <> ''`); err != nil {
		return fmt.Errorf("create scan idempotency index: %w", err)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS barcode_scans (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			barcode      TEXT    NOT NULL,
			scanned_by   TEXT    NOT NULL DEFAULT '',
			scan_type    TEXT    NOT NULL DEFAULT 'view',
			device_info  TEXT    NOT NULL DEFAULT '',
			created_at   BIGINT  NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_barcode_scans_order ON barcode_scans(order_id);
		CREATE INDEX IF NOT EXISTS idx_barcode_scans_barcode ON barcode_scans(barcode);
	`); err != nil {
		return fmt.Errorf("create barcode_scans table: %w", err)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS order_splits (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			original_order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			split_order_id   INTEGER REFERENCES orders(id) ON DELETE SET NULL,
			requested_by     TEXT    NOT NULL,
			accepted_by      TEXT    NOT NULL DEFAULT '',
			status           TEXT    NOT NULL DEFAULT 'pending',
			reason           TEXT    NOT NULL DEFAULT '',
			created_at       BIGINT  NOT NULL,
			accepted_at      BIGINT
		);
		CREATE INDEX IF NOT EXISTS idx_order_splits_original ON order_splits(original_order_id);
		CREATE INDEX IF NOT EXISTS idx_order_splits_status ON order_splits(status, created_at);
	`); err != nil {
		return fmt.Errorf("create order_splits table: %w", err)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS workflow_step_results (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id       INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			step_key       TEXT    NOT NULL,
			step_index     INTEGER NOT NULL DEFAULT 0,
			performed_by   TEXT    NOT NULL,
			correct        INTEGER NOT NULL DEFAULT 0,
			customer_impact INTEGER NOT NULL DEFAULT 0,
			notes          TEXT    NOT NULL DEFAULT '',
			created_at     BIGINT  NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_workflow_step_results_order ON workflow_step_results(order_id, step_index);
		CREATE INDEX IF NOT EXISTS idx_workflow_step_results_person ON workflow_step_results(performed_by, correct);
	`); err != nil {
		return fmt.Errorf("create workflow_step_results table: %w", err)
	}

	siCols, err := tableColumns(ctx, conn, "system_integrations")
	if err != nil {
		return err
	}
	siDDL := map[string]string{
		"config": `ALTER TABLE system_integrations ADD COLUMN config TEXT NOT NULL DEFAULT '{}'`,
	}
	for name, ddl := range siDDL {
		if siCols[name] {
			continue
		}
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("add column %s: %w", name, err)
		}
	}

	deptCols, err := tableColumns(ctx, conn, "departments")
	if err != nil {
		return err
	}
	if !deptCols["permissions"] {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE departments ADD COLUMN permissions TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("add column permissions: %w", err)
		}
	}

	return nil
}

func tableColumns(ctx context.Context, conn *sql.DB, table string) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, fmt.Errorf("read table info: %w", err)
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan table info: %w", err)
		}
		existing[name] = true
	}
	return existing, rows.Err()
}

func millis(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func nullableMillis(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: millis(*t), Valid: true}
}

func nullableTime(ms sql.NullInt64) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := fromMillis(ms.Int64)
	return &t
}
