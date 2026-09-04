# Order Cockpit — Go REST API + Dashboard

A lean, idiomatic Go backend for the Order Cockpit system: order lifecycle,
holds, QC gating, SLA, an immutable audit trail, a **role-based user system**,
an attention projection ("My Work"), a barcode scanner endpoint, comments with
@mentions driving user notifications, pick-list checklists, and **product
manuals** — per-product block manuals that admins build (add, shuffle, assign
steps) and that get ticked off per order with an attributed "Did this? YES /
NO" log; admins can **flag** a step as not done correctly, which notifies the
user who ticked it off with the reason — all served with a web dashboard from
`frontend/`. Standard
`net/http` (Go 1.22+ pattern routing), SQLite via `modernc.org/sqlite` (pure
Go, no CGO), and plain `database/sql`.

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
| `viewer`   | list/view orders, audit trail, read comments, view products & manuals |
| `operator` | viewer + create orders, transition, place/resolve holds, post comments, use the scanner, work pick lists, attach & tick manuals |
| `qc`       | viewer + submit QC checks                          |
| `admin`    | everything + user management + build/edit product manuals (`manuals:manage`) |

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

## Product manuals

Each product carries an admin-authored **manual** — an ordered list of
**blocks** (steps). The product `code` matches the `artikel` used on order
pick lines, so the dashboard can suggest the right manual for an order.

### Building a manual (admin only, `manuals:manage`)

On the **Products** page an admin creates a product and builds its manual:

* **Add block** — a title, optional body text, and an optional *assignee*
  (a real username pulled from the user dropdown). An empty assignee means
  "anyone can pick it up."
* **Shuffle order** — blocks are ordered by `seq`; move a block *up* or
  *down* one position with `POST /products/{pid}/blocks/{bid}/move`
  (`direction: "up"|"down"`). Moving past either end returns `409
  manual_edge`.
* **Direct assign** — set (or change) a block's assignee with
  `POST /products/{pid}/blocks/{bid}` (`assignee: "username"`). Empty string
  unassigns; an unknown user returns `400 unknown_assignee`.
* **Delete** — removes a block and renumbers the rest so the sequence stays
  gapless.

### Working a manual (operator+, `pick:use`)

Open an order and attach a manual: `POST /orders/{id}/manuals` with a
`productId`. The manual is **snapshotted** onto the order at that moment —
later template edits never change manuals already running on orders. Each
block becomes an "Did this?" tick button:

| Button | Meaning | Logged as |
|--------|---------|-----------|
| **YES** | done, passed | `YES` |
| **NO**  | done, failed  | `NO`  |
| **Clear** | reopen the step | `CLEAR` |

Every answer is **attributed** to the authenticated user: an immutable
`manual_tick_log` row (user + timestamp + action) is appended, the block's
current `answer`/`answered_by`/`answered_at` are updated, and an entry lands
in the order's audit trail. The tick log is append-only — clearing a block
adds a `CLEAR` entry but never deletes the history.

### Flagging a step as not done correctly (admin, `manuals:manage`)

If a block was ticked off but wasn't actually done right, an admin can **flag**
it: `POST /manuals/{mid}/blocks/{bid}/flag` with a required `reason`.
Flagging:

* marks the block (`flagged`, `flagged_by`, `flagged_at`, `flag_reason`) so the
  red call-out banner is visible on the order drawer and the manuals work view;
* appends a `FLAG` entry (with the reason) to the append-only tick log;
* writes an audit-trail entry on the order;
* **notifies the user whose tick is on the block** — a `flag` notification
  naming the admin, the step and the reason shows up in their bell panel (live
  via SSE).

Clearing the flag (`?clear=true`, or from the "Clear flag" button) removes it
from the block, logs an `UNFLAG` entry and keeps the full history. Re-answering
a flagged block (YES/NO/CLEAR) also clears the flag — the redo supersedes the
old tick, and the admin can flag again if it is still wrong. Flagging an
unanswered block returns `409 flag_unanswered`; flagging without a reason
returns `400 flag_reason_required`.

### Manuals work view

`GET /api/v1/manuals` returns every instantiated manual across all orders
with per-block tick state and progress (total / answered / failed). The
**Manuals** page shows a live table (updates via SSE `manual` events and 5 s
polling) plus a detail drawer; "Only my blocks" filters to manuals with at
least one block assigned to you.

### Snapshot model

```
products          manual_blocks      (template, admin-built)
   │                    │
   └──── snapshot ──────┘
order_manuals       order_manual_blocks   manual_tick_log  (per-order, immutable log)
```

Deleting a product removes its template; manuals already on orders keep their
frozen snapshot. An admin can still **remove** an instantiated manual from an
order (`DELETE /manuals/{mid}`), which clears its blocks but preserves the
order's audit trail.

### Visual design

Manual cards carry a state accent: green border when fully passed, red when
anything failed or is flagged. Each block is a row card whose step number is
colour-coded (grey open, green YES, red NO, solid red flagged), answered steps
show ✓/✗ with the user and time, and a flagged step grows a red banner with
the admin's reason plus a "Clear flag" action for admins. The tick log renders
`YES/NO/CLEAR/FLAG/UNFLAG` chips, with flag reasons shown inline.

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
| GET    | `/api/v1/products`                | viewer+         | Product catalog (with block counts)      |
| POST   | `/api/v1/products`                | admin           | Create product `{code, name, description}` |
| GET    | `/api/v1/products/{id}`           | viewer+         | Product + manual template blocks         |
| POST   | `/api/v1/products/{id}`           | admin           | Update product (partial)                 |
| DELETE | `/api/v1/products/{id}`           | admin           | Delete product + template                |
| POST   | `/api/v1/products/{id}/blocks`    | admin           | Add manual block `{title, body, assignee?}` |
| POST   | `/api/v1/products/{pid}/blocks/{bid}` | admin       | Edit block (title/body/assignee)         |
| POST   | `/api/v1/products/{pid}/blocks/{bid}/move` | admin   | Shuffle block `{direction: "up"\|"down"}` |
| DELETE | `/api/v1/products/{pid}/blocks/{bid}` | admin       | Delete block (renumbers the rest)        |
| POST   | `/api/v1/orders/{id}/manuals`     | operator+       | Attach product manual `{productId}` (snapshot) |
| GET    | `/api/v1/orders/{id}/manuals`     | viewer+         | Manuals on an order (blocks + tick log)  |
| POST   | `/api/v1/manuals/{mid}/blocks/{bid}/answer` | operator+ | "Did this?" `{answer: "YES"\|"NO"\|"CLEAR"}` |
| POST   | `/api/v1/manuals/{mid}/blocks/{bid}/flag` | admin       | Flag answered block `{reason}` (notify the ticked user); `?clear=true` unflags |
| DELETE | `/api/v1/manuals/{mid}`           | admin           | Remove manual from its order             |
| GET    | `/api/v1/manuals`                 | viewer+         | All instantiated manuals (work view)     |

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

# Build a product manual (admin) and tick it on an order
curl -s -X POST localhost/api/v1/products \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"PC-DEL","name":"Dell Latitude"}'
PID=$(curl -s localhost/api/v1/products -H "Authorization: Bearer $TOKEN" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)
curl -s -X POST localhost/api/v1/products/$PID/blocks \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Check seal","body":"Inspect the box seal"}'
curl -s -X POST localhost/api/v1/orders/1/manuals \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"productId\":$PID}"
curl -s -X POST localhost/api/v1/manuals/1/blocks/1/answer \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"answer":"YES"}'
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
trail, auth (login/logout/token expiry), RBAC (401/403), user management,
product manuals (template CRUD, block shuffle/assign, snapshot instantiation,
attributed YES/NO/CLEAR ticks with append-only log), and the full HTTP surface.

## Swapping to PostgreSQL

Persistence is isolated in `pkg/db` with plain `database/sql`. Moving to
PostgreSQL means: change the driver import and DSN in `db.Open`, swap
`INTEGER PRIMARY KEY AUTOINCREMENT` / `?` placeholders for `BIGSERIAL`
(`$1`) and use `NOW()` in place of millisecond timestamps. Domain logic and
handlers are storage-agnostic.