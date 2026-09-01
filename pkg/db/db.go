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
    updated_at          BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_created ON orders(created_at);

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
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    code         TEXT    NOT NULL,
    scanned_by   TEXT    NOT NULL,
    action       TEXT    NOT NULL DEFAULT 'LOOKUP',
    created_at   BIGINT  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_scan_events_order ON scan_events(order_id, created_at);
CREATE INDEX IF NOT EXISTS idx_scan_events_code ON scan_events(code);

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
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return conn, nil
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
