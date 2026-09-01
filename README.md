# Order Cockpit — Go REST API + Dashboard

A lean, idiomatic Go backend for the Order Cockpit system: order lifecycle,
holds, QC gating, SLA, an immutable audit trail, a **role-based user system**,
an attention projection ("My Work"), a barcode scanner endpoint, and
comments with @mentions driving user notifications — all served with a web
dashboard from `frontend/`. Standard `net/http` (Go 1.22+ pattern routing),
SQLite via `modernc.org/sqlite` (pure Go, no CGO), and plain `database/sql`.

## Run

```bash
go build -o cockpit-server main.go
ADMIN_PASSWORD=choose-a-secret ./cockpit-server   # listens on :80, serves frontend/ at /
```

Binding port 80 requires `cap_net_bind_service` when running unprivileged. Grant
it once to the binary (do this after every rebuild — capabilities don't survive
recompilation):

```bash
sudo setcap cap_net_bind_service=+ep ./cockpit-server
```

Alternatively set `PORT` to an unprivileged port (e.g. `PORT=8080`).

The `frontend/` folder is served as static content at `/`, so the dashboard is
available at `http://localhost/` on the default port 80.

Configuration via environment variables:

| Variable            | Default      | Purpose                                |
|---------------------|--------------|----------------------------------------|
| `PORT`              | `80`         | HTTP listen port                       |
| `DB_PATH`           | `cockpit.db` | SQLite file (created on boot)          |
| `SLA_TARGET_HOURS`  | `24`         | Default SLA window for new orders      |
| `SESSION_HOURS`     | `24`         | Login token lifetime                   |
| `ADMIN_USERNAME`    | `admin`      | Bootstrap admin account                |
| `ADMIN_PASSWORD`    | `admin`      | Bootstrap admin password               |

On first boot the API seeds the bootstrap admin
(`ADMIN_USERNAME`/`ADMIN_PASSWORD`, defaults `admin`/`admin`). Set
`ADMIN_PASSWORD` in production.

## Users & permissions

Accounts are managed by admins via `POST /api/v1/users` or the dashboard's
Users page. Passwords are hashed with PBKDF2-SHA256 (600k iterations, per-user
salt) and never returned by the API. Login issues a revocable, expiring bearer
token (`SESSION_HOURS`); use it as `Authorization: Bearer <token>`.

Roles and permissions:

| Role       | Permissions                                        |
|------------|----------------------------------------------------|
| `viewer`   | list/view orders, audit trail, read comments       |
| `operator` | viewer + create orders, transition, place/resolve holds, post comments, use the scanner |
| `qc`       | viewer + submit QC checks                          |
| `admin`    | everything + user management                       |

Protected endpoints answer `401` without/with an expired token and `403` when
the role lacks the permission. All actions (create, transition, hold, QC)
are recorded in the audit log under the authenticated username.

## Order state machine

```
Received ──► Processing ──► QC_Review ──► Completed
   ▲            ▲               │
   └────────────┴───────────────┘   (rework / direct fly-through allowed)
```

* Strict transitions: `Received->Completed` and `Processing->Completed` are
  rejected — an order must pass through `QC_Review`.
* `On_Hold` is an auxiliary state applied while an unresolved hold exists. The
  underlying progress state is preserved (`Held_Processing`, ...) and restored
  on resolve. Transitions are refused while a hold is active.
* SLA is computed dynamically on every read: `ON_TIME`, `WARNING` (≤ 4 h to
  target), or `BREACHED` (target passed).
* Orders can only reach `Completed` when the *most recent* QC check is `PASS`.

## My Work, Scanner & Comments

* **My Work** (`GET /api/v1/attention`) derives an attention projection from
  orders, active holds and SLA. `pkg/attention` is a pure function
  (`Project(now, orders, holds)`) that classifies each order into a severity
  (`high`/`medium`/`low`) and reason (`SLA_BREACHED`, `SLA_WARNING`, `ON_HOLD`,
  `AWAITING_QC`, `FRESH`), drops completed and healthy orders, and sorts
  highest severity first (ties by earliest target). The dashboard renders the
  cards on the *My Work* page with deep links into the order drawer.
* **Scanner** (`POST /api/v1/scan`) resolves a scanned barcode to an order and
  appends an immutable scan event (`scan_events`) recording the operator
  identity and code; every scan also lands in the order's audit trail. The
  *My Work* page embeds a keyboard-wedge style scan box.
* **Comments & @mentions** — comments are stored per order (max 2000 chars).
  `pkg/mention` parses `@username` tokens (code spans excluded, sentence
  punctuation stripped) and resolves them against real users; unresolved
  mentions are stored as text but do not notify. Each resolved mention creates
  exactly one notification per user (enforced by a UNIQUE constraint —
  retries are idempotent). Notifications are strictly user-scoped and support
  `?unread=true` plus bulk mark-read.

## API

| Method | Path                              | Permission      | Description                              |
|--------|-----------------------------------|-----------------|------------------------------------------|
| GET    | `/healthz`                        | public          | Liveness probe                           |
| POST   | `/api/v1/auth/login`              | public          | Exchange credentials for a bearer token  |
| POST   | `/api/v1/auth/logout`             | authenticated   | Revoke the current token                 |
| GET    | `/api/v1/auth/me`                 | authenticated   | Current user profile                     |
| GET    | `/api/v1/users`                   | admin           | List users                               |
| POST   | `/api/v1/users`                   | admin           | Create user                              |
| POST   | `/api/v1/users/{id}/role`         | admin           | Change role                              |
| POST   | `/api/v1/users/{id}/password`     | admin           | Reset password                           |
| DELETE | `/api/v1/users/{id}`              | admin           | Delete user (unless self)                |
| POST   | `/api/v1/orders`                  | operator+       | Create order (initial state `Received`)  |
| GET    | `/api/v1/orders?status=Received`  | viewer+         | List orders, optional status filter      |
| GET    | `/api/v1/orders/{id}`             | viewer+         | Order detail + active holds + SLA        |
| POST   | `/api/v1/orders/{id}/transition`  | operator+       | Transition `Processing\|QC_Review\|Completed` |
| POST   | `/api/v1/orders/{id}/holds`       | operator+       | Place a hold (order -> `On_Hold`)        |
| POST   | `/api/v1/holds/{id}/resolve`      | operator+       | Resolve a hold (restores progress state) |
| POST   | `/api/v1/orders/{id}/qc`          | qc+             | Submit QC evaluation `PASS\|FAIL`        |
| GET    | `/api/v1/orders/{id}/audit`       | viewer+         | Immutable audit trail                    |
| GET    | `/api/v1/attention`               | viewer+         | "My Work" attention projection           |
| POST   | `/api/v1/scan`                    | operator+       | Barcode lookup + audited scan event      |
| GET    | `/api/v1/orders/{id}/scans`       | viewer+         | Scan history for an order                |
| GET    | `/api/v1/orders/{id}/comments`    | viewer+         | Comments (with resolved @mentions)       |
| POST   | `/api/v1/orders/{id}/comments`    | operator+       | Add comment, parse @mentions, notify     |
| GET    | `/api/v1/notifications`           | authenticated   | Own notifications (`?unread=true`)       |
| POST   | `/api/v1/notifications/read`      | authenticated   | Mark own notifications read              |
| GET    | `/api/v1/attention`               | viewer+         | "My Work" attention projection           |
| POST   | `/api/v1/scan`                    | operator+       | Barcode lookup, records a scan event     |
| GET    | `/api/v1/orders/{id}/scans`       | viewer+         | Scan history for an order                |
| GET    | `/api/v1/orders/{id}/comments`    | viewer+         | Comments (with resolved mentions)        |
| POST   | `/api/v1/orders/{id}/comments`    | operator+       | Add comment; `@username` mentions notify |
| GET    | `/api/v1/notifications`           | authenticated   | Own notifications (`?unread=true`)       |
| POST   | `/api/v1/notifications/read`      | authenticated   | Mark own notifications read              |

Responses are JSON. Errors use
`{"error":{"code":"...","message":"..."}}` with `401 / 403` and
`400 / 404 / 409 / 500`.

Example:

```bash
TOKEN=$(curl -s -X POST localhost/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')

curl -s -X POST localhost/api/v1/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"orderNumber":"ORD-1001"}'

curl -s -X POST localhost/api/v1/orders/1/transition \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"Processing"}'
```

## Layout

```
main.go               — entry point: db init, admin seed, routes, server on :80
frontend/             — web dashboard (HTML/CSS/JS) served at /
pkg/auth/             — password hashing, tokens, roles & permissions
pkg/order/            — domain types, state machine, SLA calculation
pkg/attention/        — pure "My Work" attention projection
pkg/mention/          — @mention parsing and resolution
pkg/db/               — schema + Store (database/sql, transactional)
pkg/api/              — HTTP handlers, auth middleware, routing
```

## Tests

```bash
go test ./...
bash test.sh          # end-to-end smoke test against a running server
```

Covers the state machine, hold lifecycle, QC gating, SLA computation, audit
trail, auth (login/logout/token expiry), RBAC (401/403), user management, and
the full HTTP surface.

## Swapping to PostgreSQL

Persistence is isolated in `pkg/db` with plain `database/sql`. Moving to
PostgreSQL means: change the driver import and DSN in `db.Open`, swap
`INTEGER PRIMARY KEY AUTOINCREMENT` / `?` placeholders for `BIGSERIAL`
(`$1`) and use `NOW()` in place of millisecond timestamps. Domain logic and
handlers are storage-agnostic.