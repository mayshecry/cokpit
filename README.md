# Cockpit Technical Guide

This guide describes the current implementation in this repository. It covers the Go server, SQLite persistence, browser applications, offline scanner, native client, background workers, and operational interfaces. The complete HTTP route inventory is included at the end.

The source code is the authority for behavior. This document is the consolidated explanation of that behavior; inline source comments are intentionally not required to understand the system.

## 1. System overview

Cockpit is a same-origin web application and JSON API for order execution. Its main responsibilities are:

- tracking orders, targets, assignees, holds, QC decisions, and audit history;
- coordinating pick lists, product manuals, work sessions, and daily goals;
- projecting operational attention, workload, quality, and throughput metrics;
- managing corrective and preventive actions (CAPA);
- evaluating order automations;
- managing system-integration (SI) definitions and their project checklists;
- running the SWI process board, escalations, and external synchronization jobs;
- supporting order-number scans, generated order barcodes, and offline scan recovery;
- ingesting laptop-preparation logs;
- providing live browser updates through server-sent events (SSE).

### 1.1 Runtime shape

```text
Browser or native WebView
        |
        | same-origin HTTP, JSON, SSE, static files
        v
Go net/http server
        |
        +---- authentication and additive department permissions
        +---- domain validation and transaction boundaries
        +---- in-memory SSE, rate-limit, QR-login, and client-session state
        +---- automation and automatic-escalation workers
        |
        v
SQLite database in WAL mode

External integration worker, when used
        |
        +---- claims and completes external_sync_jobs through the API
        +---- AFAS, Omnitracker, Intune, Knox, and Apple Business Manager
```

The server does not contain native network connectors for those external systems. It plans and persists synchronization jobs; an integration worker must claim and complete them through the API.

### 1.2 Design boundaries

- `pkg/order`, `pkg/si`, `pkg/swi`, `pkg/capa`, and `pkg/automation` contain domain types and validation.
- `pkg/db` owns SQL, transactions, migrations, and query projections.
- `pkg/api` owns HTTP routing, authentication middleware, request decoding, response mapping, SSE, and handler-level object authorization.
- `frontend` contains static HTML, CSS, fonts, and plain JavaScript. There is no browser compilation step for the main application.
- `client` is a small Go WebView launcher with machine-bound pairing.
- SQLite is the system of record. SSE messages are refresh hints, not a second event store.

## 2. Repository map

| Path | Responsibility |
|---|---|
| `main.go` | Reads configuration, opens the database, bootstraps admin access, constructs the server, starts workers, and performs graceful HTTP shutdown. |
| `pkg/api/server.go` | Registers routes and shared API behavior. |
| `pkg/api/middleware.go` | Authentication, permission checks, request logging, panic recovery, and streaming-compatible response wrapping. |
| `pkg/api/security.go` | Security headers, same-origin write checks, client-IP handling, and fixed-window rate limits. |
| `pkg/api/session.go` | Bearer/cookie token selection and session-cookie lifecycle. |
| `pkg/api/clientguard.go` | Client-only UI restriction, pairing, launch proofs, paired-key administration, and QR pairing helpers. |
| `pkg/api/qrlogin.go` | Short-lived browser/phone QR-login tickets. |
| `pkg/api/*_handlers.go`, `handlers.go`, `handlers_holds.go` | Feature-specific HTTP handlers. |
| `pkg/api/events.go` | In-memory SSE subscriptions, publishing, heartbeats, and stream authentication. |
| `pkg/auth` | Roles, permissions, PBKDF2 password hashing, random tokens, and token digests. |
| `pkg/order` | Order status graph, hold normalization, SLA calculations, order DTOs, and manual DTOs. |
| `pkg/attention` | Deterministic My Work/attention projection. |
| `pkg/mention` | Parses and resolves `@username` mentions. |
| `pkg/capa` | CAPA statuses, transition rules, and validation. |
| `pkg/automation` | Automation trigger/action types and rule validation. |
| `pkg/si` | SI statuses, configuration, projects, and transition rules. |
| `pkg/swi` | Process stages, stage tasks, escalations, synchronization plans, and validation. |
| `pkg/db/db.go` | Base schema, SQLite opening, and additive migrations. |
| `pkg/db/store.go`, `pkg/db/*.go` | Transactions and queries for each persistence aggregate. |
| `frontend/index.html` | Main modular browser shell. |
| `frontend/js` | Main application, page modules, scanner, pairing, QR approval, and offline support. |
| `frontend/styles.css` | Main application styling. |
| `frontend/sw.js` | Service worker for the application shell and offline navigation. |
| `frontend/build.py`, `frontend/build.ps1` | Generate the legacy single-file HTML variant. |
| `frontend/cockpit-standalone.html` | Generated single-file variant; it is not the primary modular application. |
| `frontend/test/test.html` | Browser-based test page. |
| `client/main.go` | Native WebView launcher and local pairing-file handling. |
| `test.sh` | Live HTTP regression suite. |

## 3. Startup and configuration

### 3.1 Build and run

Run the server with the repository root as its working directory. Static files are served from the relative `frontend` directory; they are not embedded in the Go binary.

```bash
go build -o cockpit-server .
PORT=8080 DB_PATH=cockpit.db ADMIN_PASSWORD='replace-me' ./cockpit-server
```

The default port is `80`, which can require elevated bind privileges on Unix-like systems. Use an unprivileged port for development or grant the binary the appropriate capability.

A minimal explicit production-style startup is:

```bash
PORT=8080 \
DB_PATH=/var/lib/cockpit/cockpit.db \
ADMIN_USERNAME=admin \
ADMIN_PASSWORD='replace-with-a-long-random-secret' \
COCKPIT_PUBLIC_URL=https://cockpit.example.com \
COCKPIT_TRUST_PROXY=1 \
./cockpit-server
```

Only set `COCKPIT_TRUST_PROXY=1` when direct clients cannot spoof forwarded headers and the reverse proxy overwrites them correctly.

### 3.2 Environment variables

| Variable | Default | Meaning |
|---|---:|---|
| `PORT` | `80` | HTTP listen port. The server itself uses plain HTTP; terminate TLS at a proxy for HTTPS. |
| `DB_PATH` | `cockpit.db` | SQLite database path. |
| `SLA_TARGET_HOURS` | `24` | Default target window for new orders without an explicit target. Invalid or non-positive values fall back to 24. |
| `SESSION_HOURS` | `24` | Login-session lifetime. Invalid or non-positive values fall back to 24. |
| `ADMIN_USERNAME` | `admin` | Bootstrap administrator username. |
| `ADMIN_PASSWORD` | unset | Bootstrap administrator password. If the account does not exist and no password is supplied, a random token-length password is logged once. |
| `COCKPIT_TRUST_PROXY` | `0` | `1` trusts `X-Real-IP`, `X-Forwarded-For`, and forwarded protocol/host information as implemented by the effective-URL helpers. |
| `COCKPIT_PUBLIC_URL` | empty | External base URL used for QR/pairing links and external-origin calculations. |
| `COCKPIT_CLIENT_ONLY` | `0` | `1` requires a paired client for non-allowlisted UI/static paths. |
| `COCKPIT_CLIENT_KEYS` | `cockpit-client-keys.json` | JSON key store for paired native/browser clients. This is separate from SQLite. |
| `SWI_AUTO_ESCALATE_MINUTES` | `15` | Automatic SWI escalation interval. `0` disables the worker; invalid or negative values fall back to 15. |
| `AUTOMATION_INTERVAL_SECONDS` | `60` | Order-automation evaluation interval. `0` disables the worker; invalid or negative values fall back to 60. |
| `COCKPIT_SERVER` | native-client default | Native client only: overrides the server URL before reading the saved pairing file. |

### 3.3 Boot sequence

1. Parse scalar configuration and install signal handling for `SIGINT` and `SIGTERM`.
2. Open SQLite, apply the base schema and additive migrations, and ping the database.
3. Ensure the bootstrap admin exists. Existing accounts are not overwritten by startup.
4. Initialize the optional client guard and its JSON key store.
5. Construct the API server, SSE hub, and rate limiters; ensure the boot pairing code when applicable.
6. Start automatic escalation and automation workers when enabled. The automation worker evaluates once immediately and then on its ticker.
7. Start HTTP with 5-second read-header, 15-second read, 15-second write, and 60-second idle timeouts. SSE explicitly clears its write deadline.
8. On shutdown, stop accepting work and allow up to 10 seconds for HTTP shutdown.

`GET /healthz` returns `{"status":"ok"}`. It is a process-health endpoint, not a deep database or external-integration health check.

## 4. HTTP stack and API conventions

### 4.1 Request pipeline

The effective middleware order is:

```text
panic recovery
  -> request logging
    -> security headers
      -> optional paired-client guard
        -> same-origin guard for state-changing browser requests
          -> method/path mux
            -> session authentication, where registered
              -> permission middleware, where registered
                -> handler-specific validation and object authorization
```

Route parameters use Go 1.22+ patterns such as `GET /api/v1/orders/{id}`. Static files are served below `/`; unmatched `/api/` paths return a JSON 404.

HTML and `/` responses use `Cache-Control: no-cache, must-revalidate`. API responses use `no-store` and `Pragma: no-cache`.

### 4.2 Authentication input

Protected routes accept either:

```http
Authorization: Bearer <session-token>
```

or the `cockpit_session` cookie. A Bearer token takes precedence when both are present. No session tokens are accepted in URL query parameters.

### 4.3 JSON and response conventions

- API successes are JSON objects whose feature payload is usually named, for example `order`, `orders`, `detail`, `action`, or `session`.
- JSON request bodies are limited to 1 MiB. A JSON value followed by trailing data is rejected.
- Path IDs are decimal integers and must be valid for the target resource.
- Timestamps exposed as Go `time.Time` values are RFC 3339 JSON strings. Internally, SQLite stores most timestamps as UTC Unix milliseconds.
- Go `time.Duration` fields, such as SLA `remaining`, serialize as integer nanoseconds.
- Bulk operations return an overall response containing per-item `ok`, `code`, and `message` results; one item's failure does not imply that all items failed.

Errors have this shape:

```json
{
  "error": {
    "code": "invalid_transition",
    "message": "human-readable detail"
  }
}
```

Common status classes are:

| HTTP | Meaning |
|---:|---|
| 400 | Malformed JSON, invalid identifier, invalid enum, or invalid field value. |
| 401 | Missing, invalid, revoked, or expired session credentials. |
| 403 | Authenticated but lacking permission, failing object-level authorization, or failing pairing/origin checks. |
| 404 | Missing resource, unmatched order/barcode, or unknown route. |
| 409 | Domain conflict: invalid transition, active/resolved hold, stale write, duplicate value, incomplete QC gate, or idempotency conflict. |
| 410 | Expired QR-login ticket. |
| 422 | Uploaded content is not a recognizable preparation log. |
| 429 | Fixed-window rate limit exceeded; relevant handlers also return `Retry-After`. |
| 500 | Unexpected persistence or server failure. |

Frequently used machine codes include `not_found`, `duplicate_order_number`, `duplicate_username`, `duplicate_product_code`, `duplicate_manual`, `duplicate_customer_number`, `duplicate_project_code`, `duplicate_si_code`, `invalid_status`, `invalid_transition`, `active_hold`, `hold_active`, `hold_already_resolved`, `qc_not_passed`, `stale_write`, `scan_idempotency_conflict`, `invalid_capa`, `invalid_capa_transition`, `invalid_automation_rule`, and `rate_limited`.

## 5. Authentication, sessions, and authorization

### 5.1 Passwords and login

Passwords are encoded as:

```text
pbkdf2-sha256$600000$<hex-salt>$<hex-derived-key>
```

Each password has a 16-byte random salt and a 32-byte PBKDF2-SHA256 derived key using 600,000 iterations. Verification uses constant-time digest comparison. Login against a nonexistent user performs a dummy hash verification to reduce username-timing differences.

Login limits are process-local fixed windows:

- 20 login requests per client IP per minute;
- 5 attempts per IP-and-normalized-username per five minutes;
- successful login resets the per-account bucket.

A successful login creates a 32-byte random token, hex encoded for transport. New sessions store only the token's SHA-256 digest in `api_tokens`. The token expires after `SESSION_HOURS`; logout revokes the presented session, and password reset revokes that user's sessions.

The login response contains the user and raw token and also sets `cockpit_session`. The cookie is host-only, `HttpOnly`, `SameSite=Strict`, and `Secure` when the effective request scheme is HTTPS. API clients can retain the Bearer token instead.

The modular frontend's password flow retains the returned token in local storage for its API helper. QR login instead establishes the cookie session and removes the local Bearer token. Therefore, the backend supports HttpOnly cookie authentication, but not every frontend login path is exclusively cookie-based.

### 5.2 QR login

QR login is separate from native-client pairing:

1. An unauthenticated browser requests `GET /api/v1/qr-login`.
2. The server creates a short-lived, in-memory ticket and returns a QR data URL and approval URL.
3. An already authenticated phone opens the approval page and approves or rejects through `POST /api/v1/qr-login/approve`.
4. Approval creates a normal database-backed session for the approving user.
5. The original browser polls the ticket status. On approval it receives the session cookie and user profile, then enters the application.

Tickets expire, are not persistent across server restarts, and are rate-limited. Any authenticated user can approve or reject a ticket; approval does not elevate the browser beyond that user's own permissions.

Managed-user creation and administrator password resets enforce the password policy in `pkg/api/security.go`: 10–256 characters, not equal to the trimmed username case-insensitively, with at least one letter and one digit or symbol. This policy is separate from verification of an existing password at login.

### 5.3 Roles

| Role | Base capabilities |
|---|---|
| `viewer` | Order list/detail/audit, comment reading, SI list/detail, process viewing, and notification-related baseline permissions. |
| `operator` | Viewer capabilities plus order creation/transition, holds, comment posting, scanning, picking/manual execution, and process advance/escalation. |
| `qc` | Order list/detail/audit/transition, QC submission, comments, manual management, SI viewing, process viewing, and process escalation. |
| `npi` | SI creation/update/transition/project linking, user listing, process viewing/management, and integration-job management. |
| `sc` | Order viewing/audit, comments, manual management and approval, customer management, SI viewing, user listing, and process viewing. |
| `admin` | Every permission, including user/department administration, backup, pairing, automation, and forced process management. |

Authorization is enforced on the server even when the frontend hides a control.

### 5.4 Departments and additive permissions

A user keeps their role permissions and also receives permissions assigned to every department they belong to. Department grants are additive: they can add a capability but cannot subtract a role capability. The effective permission check is therefore:

```text
role has permission OR any assigned department grants permission
```

Department permission updates require `users:manage`. User responses include effective department grants for frontend rendering, but the server repeats the check for every protected request.

### 5.5 Object-level rules beyond route permissions

Route permission alone is not always sufficient:

- work start/update/end requires the order assignee or an admin;
- CAPA detail updates require QC, or the CAPA owner who also has `orders:transition`;
- non-QC CAPA owners cannot change title, failure classification, owner, or due date;
- CAPA creation and final verification require `qc:submit`;
- forced SWI advancement additionally requires `process:manage`;
- administrative object endpoints can apply additional existence and relationship checks in their stores.

## 6. Order lifecycle, holds, QC, and SLA

### 6.1 Order data

An order contains its unique order number, lifecycle status, target and audit timestamps, optional assignee, accumulated paused seconds, and optional operational identifiers:

- debit/customer number and customer name;
- Omnitracker ticket;
- device and asset number;
- configuration;
- linked SI ID;
- generated barcode.

Creation defaults to `Received`. If no target is supplied, the server uses the configured SLA hours. Explicit targets must be valid and in the future. Creation and later mutations write audit records under the authenticated username; request-supplied `performedBy` values do not replace the authenticated actor in the normal HTTP path.

### 6.2 Lifecycle graph

| Current base status | Allowed targets |
|---|---|
| `Received` | `Processing`, `QC_Review` |
| `Processing` | `QC_Review`, `Received` |
| `QC_Review` | `Completed`, `Processing`, `Received` |
| `Completed` | none |

`On_Hold` is a query/display classification, not a direct transition target. Clients cannot request a transition to it; placing a hold produces the stored state described below.

Completing an order has two independent gates:

1. the graph must allow `QC_Review -> Completed`;
2. the most recent QC check must exist and be `PASS`.

A later `FAIL` therefore blocks completion until a newer `PASS` exists. Completed orders reject further transitions, holds, and QC submissions.

### 6.3 Optimistic concurrency

Order transition and hold-placement requests accept `expectedUpdatedAt`. When supplied, it must exactly equal the order's current `updated_at`; otherwise the store returns `stale_write` with HTTP 409. This prevents a client from silently applying a decision to a version it has not seen. When the field is omitted, the operation proceeds without that comparison.

### 6.4 Holds

Placing a hold:

- rejects completed orders and orders that already have an active hold;
- requires a reason;
- stores the lifecycle state as `Held_` plus the base state, such as `Held_Processing`;
- inserts an unresolved hold and an audit entry in the same transaction.

While any hold is unresolved, normal transitions are blocked. Resolving a hold:

- marks its `resolved_at` and attributes the resolver;
- removes the `Held_` prefix, restoring the base state;
- adds the hold duration in whole seconds to `paused_seconds`;
- writes an audit entry.

Resolving an already resolved hold returns `hold_already_resolved`. Bulk resolve reports results per order.

### 6.5 SLA computation

SLA is calculated on reads rather than stored as a fixed status. The effective current time is moved backward by:

- accumulated `paused_seconds` from resolved holds; and
- elapsed time since the oldest active hold.

The remaining duration is `targetCompletionAt - effectiveNow`.

| SLA value | Rule |
|---|---|
| `BREACHED` | Remaining time is negative. |
| `WARNING` | The target has not passed and remaining time is at most four hours. |
| `ON_TIME` | More than four hours remain. |

The response includes the original target and signed remaining duration. Paused SLA time is also used by attention and automation calculations.

### 6.6 QC records

QC submissions are append-only checks with `PASS` or `FAIL`, inspector ID, notes, failure reason, technician/assignee snapshot, and timestamp. A failure requires a failure reason in the HTTP validation path. Submitting QC does not itself transition the order; the client or another authorized workflow performs the appropriate transition separately.

QC endpoints provide:

- the order's check history;
- a QC work queue and contextual order data;
- configured failure reasons;
- monthly pass/fail rates, trends, reasons, per-user data, and history.

### 6.7 Audit trail

Order mutations insert `audit_logs` rows with order ID, action text, actor, and timestamp. Product-manual answers/flags, comments, holds, QC, assignment, CAPA activity, and transitions contribute to this history. There is no normal API to edit or delete an individual audit row.

## 7. Attention, work, comments, notifications, and metrics

### 7.1 Attention projection

`GET /api/v1/attention` computes a fresh projection from noncompleted orders and active holds. Each order produces at most one card, using this precedence:

| Precedence | Reason | Severity | Condition |
|---:|---|---|---|
| 1 | `ON_HOLD` | high | One or more active holds. |
| 2 | `SLA_BREACHED` | high | Pause-adjusted target has passed. |
| 3 | `SLA_WARNING` | medium | Pause-adjusted target is within four hours. |
| 4 | `AWAITING_QC` | low | Base status is `QC_Review`. |
| 5 | `FRESH` | low | `Received` order created less than two hours ago. |

Cards sort by severity and then target time. Summary counters count the selected reason per card, not every simultaneous condition.

### 7.2 Comments and mentions

Comments are stored with author ID/name, body, and timestamp. The mention parser recognizes `@username` outside backtick-delimited code spans, supports letters, digits, `_`, `.`, and `-`, deduplicates names, and resolves them against existing users.

A mention creates a notification for the mentioned user, except self-mentions. The comment, mention links, notifications, and order audit entry are committed together. Bodies are stored as text; the frontend escapes rendered content rather than treating it as trusted HTML.

### 7.3 Notifications

Notifications belong to a username and have a kind, optional order/reference IDs, body, creation time, and read time. Sources include mentions, manual flags, assignments, CAPA activity, automations, escalations, and day goals. Listing and marking read operate on the authenticated user's own notifications; SSE notification messages are also filtered by recipient.

### 7.4 Work sessions and customer workflows

A work session records the order, user, start/end times, current step, a JSON checks payload, and completion flag. Only one active session may hold an order. Starting or modifying one requires the assignee or admin; completed orders cannot be clocked into.

Customer workflows define the steps presented to the worker:

- one to eight steps;
- unique machine-readable step keys;
- up to 12 checks per step;
- optional pick/QC markers and allowed next-order-status metadata.

The workflow update endpoint requires `users:manage`; reading a workflow requires `orders:list`. Work update stores progress but does not, by itself, perform an order lifecycle transition.

### 7.5 Day goals and operational metrics

Day goals are keyed by date and username. QC-authorized users can save a goal and send it as an in-app notification; sending marks the goal's sent timestamp. It is not an email integration.

Operational statistics combine created/completed order counts, work-clock activity, average handling time, leaderboard data, open orders, and SLA mix. The default window is 14 UTC days and can be set from 1 to 60 days.

## 8. Scanning, barcodes, and offline recovery

There are two related but distinct scan paths.

### 8.1 Exact order-number scanning

`POST /api/v1/scan` trims and looks up `code` as an exact order number. Codes are required and limited to 256 characters. The only supported action is `LOOKUP`, which is also the default. A match returns the order, current detail/SLA projection, and a recorded scan event.

Fields used for reliable offline replay are:

- `clientEventId`: optional client-generated ID, at most 128 characters using letters, numbers, `.`, `_`, `:`, or `-`;
- `scannedAt`: optional original scan time, not before 2000-01-01 and no more than five minutes in the future;
- server `receivedAt`: when the server actually accepted the event.

For a nonempty client event ID, a partial unique index on `(scanned_by, client_event_id)` makes retries idempotent per user. Replaying the same ID for the same order, code, and action returns the existing event as a duplicate rather than inserting another. Reusing the ID for a different order, code, or action returns `scan_idempotency_conflict`; the originally stored scan and receive timestamps are retained.

Unknown order numbers return 404 `no_match`; they do not create scan events.

### 8.2 Generated order barcodes

The barcode endpoints operate on the separate `orders.barcode` value:

- `POST /api/v1/orders/{id}/barcode` returns the existing value or generates a `CPT-…` value; `regenerate` requests a replacement.
- `GET /api/v1/orders/{id}/barcode` reads the current value without generating it.
- `GET /api/v1/scan/barcode?code=...` resolves that generated barcode, records a `barcode_scans` row with scan type and User-Agent, and returns order/scan/Omnitracker links.
- `GET /api/v1/orders/{id}/barcode/history` returns that barcode history.

These barcode scan records are separate from the idempotent order-number `scan_events` used by offline scanning.

### 8.3 Offline browser scanner

The offline scanner is user-scoped and stores at most 500 pending items in local storage under `cockpit_offline_scans_v1:<username>`. Each item receives a UUID when available and preserves its original `scannedAt` timestamp.

Synchronization behavior:

- sends the saved code, client event ID, and original timestamp to `/api/v1/scan`;
- removes an item only after server acceptance and successful local queue update;
- retries network errors, 408, 429, and 5xx responses with exponential backoff capped at five minutes;
- marks permanent client/domain rejections as failed for manual retry or discard;
- pauses appropriately for authentication/permission changes;
- retries periodically and on online/login events.

The service worker caches the static shell, not `/api/` responses. Navigations are network-first with cached HTML fallback; other same-origin static GETs are cache-first and then populated from successful network responses. This makes the scanner shell available offline without treating cached API data as authoritative.

## 9. Product manuals and checklists

### 9.1 Product template manuals

A product is a reusable manual template with a unique code, name, description, department, optional approval metadata, and ordered blocks. A block contains sequence, title, body, and optional assignee. Assignees must be existing users.

Template operations include create/update/delete, quick creation from step strings, approval, department assignment, block editing, deletion, and moving blocks up/down. Attempting to move beyond either end returns `manual_edge`.

Approval is metadata controlled by `manuals:approve`; general template editing uses `manuals:manage`.

### 9.2 Per-order manual instances

Instantiating a product on an order copies the product identity and current blocks into `order_manuals` and `order_manual_blocks`. Later template edits do not rewrite existing instances. The same product cannot be instantiated twice on the same order. A product referenced by existing instances cannot simply be deleted while that foreign-key relationship remains.

Each instance block supports:

- `YES`: answered and counted as completed;
- `NO`: answered and counted as failed;
- `CLEAR`: removes the answer and any flag.

Every answer change writes `manual_tick_log` and the order audit trail. A manager can flag an answered block with a required reason or clear that flag. Flagging notifies the user who answered, writes the manual log, and writes the order audit trail. Re-answering or clearing also clears the flag.

Deleting an order manual cascades its copied blocks/log entries and writes an order audit entry. The parent order and its other audit history remain.

### 9.3 Other checklist types

Do not conflate these models:

| Model | Purpose |
|---|---|
| `checklist_items` | Order pick-list lines, ticked/unticked through the pick-list API. |
| `order_manual_blocks` | Copied YES/NO product-manual execution steps. |
| `project_checklist_items` | Checklist items belonging to an SI project. |
| `process_tasks` | Required/optional tasks seeded for each SWI process stage. |
| `work_sessions.checks` | A JSON progress payload for a customer's configured work flow. |

Each has its own permissions, history, and lifecycle rules.

## 10. CAPA

CAPA records corrective action work against an order, optionally linked to a specific failed QC check.

### 10.1 Fields and creation

A corrective action includes order/QC references, title, failure reason, owner, due date, root cause, action plan, resolution, status, creator, timestamps, and verification metadata. Creation requires `qc:submit`, a valid order, an existing owner, a title of at most 160 characters, and a future due date. Long text fields are limited to 4,000 characters.

### 10.2 State graph

| Current status | Allowed next status |
|---|---|
| `OPEN` | `IN_PROGRESS`, `PENDING_VERIFICATION` |
| `IN_PROGRESS` | `OPEN`, `PENDING_VERIFICATION` |
| `PENDING_VERIFICATION` | `IN_PROGRESS`, `CLOSED` through verification |
| `CLOSED` | none; immutable |

Entering `PENDING_VERIFICATION` requires nonempty root cause, action plan, and resolution. A normal update cannot directly set `CLOSED`; only `POST /api/v1/capa/{id}/verify` can close an action, and it requires a verification note.

QC users can update classification and ownership fields. An owner without QC can update execution fields only when they also have `orders:transition`. Changes create CAPA events, order audit records, notifications where relevant, and SSE refresh hints.

List filters include status, owner, `owner=me`, overdue-only, and limit. The summary counts every status plus nonclosed actions whose due date has passed.

## 11. Order automations

Automation administration requires `users:manage`.

### 11.1 Rule model

| Trigger | Candidate |
|---|---|
| `QC_FAILED` | Failed QC checks created at or after the rule's creation time. |
| `SLA_BREACHED` | Noncompleted orders whose pause-adjusted SLA is breached. |
| `HOLD_AGING` | Active holds older than `thresholdMinutes`. |

| Action | Effect |
|---|---|
| `NOTIFY` | Creates an automation notification for every target user. |
| `ASSIGN` | Assigns the order to exactly one target, writes assignment history, and notifies that user. |
| `ESCALATE` | Raises an SWI escalation at `L1`, `L2`, or `L3`, optionally targeted to one user. |

Hold thresholds range from 1 to 525,600 minutes. Non-hold triggers must have threshold zero. Rule names and targets are validated, duplicate targets are rejected, and notification messages are limited to 500 characters.

Escalation reasons are derived from the trigger: quality for QC failure, SLA breach for SLA, and blocked for hold aging.

### 11.2 Execution and deduplication

The background worker, the manual evaluate endpoint, and the immediate evaluation after a `FAIL` QC submission use the same server mutex and store evaluator. A run is first claimed as `PENDING` with a unique rule/trigger key, then executed and marked `SUCCESS` or `FAILED`.

Trigger keys are:

- `qc:<qc-check-id>`;
- `hold:<hold-id>`;
- `sla:<order-id>`.

This gives one recorded attempt per rule and candidate. A failed run is retained as failed and is not automatically retried as a new candidate. Editing a rule does not erase prior run keys; creating a new rule has a new rule ID.

Successful runs publish automation/order/notification SSE hints as appropriate. Run rows reference both their rule and order with `ON DELETE CASCADE`, so deleting either parent also deletes the associated run history.

## 12. SI: customers, projects, configurations, and checklists

SI records describe system-integration configurations independently from order execution.

### 12.1 Hierarchy

```text
Customer
  -> Project
       -> project checklist items
       -> linked SI records

SI record
  -> primary project and additional project links
  -> JSON configuration
  -> status/version/environment
  -> SI events
```

Customers have unique numbers and optional coordinators. Projects have unique codes within their customer context. An SI code has the form `SI-{debit}-{device}-{config}` and can carry structured configuration such as device type, Windows profile, software, asset sticker, sleeve, screen protector, work instructions, SWI/workflow/QC profiles, automations, and escalation flow.

### 12.2 SI lifecycle

| Current status | Allowed targets |
|---|---|
| `Concept` | `In Validation`, `Blocked` |
| `In Validation` | `Accepted`, `Concept`, `Blocked` |
| `Accepted` | `Outphased`, `Blocked` |
| `Blocked` | `Concept`, `In Validation`, `Accepted` |
| `Outphased` | none |

Environments are `TEST`, `ACCEPTATIE`, and `PRODUCTIE`. SI versions and status events provide revision/history metadata. Project links and project checklist operations are separate from order pick lists and SWI process tasks.

## 13. SWI process board, escalations, and synchronization

The SWI process layer is a second, more detailed state machine attached to an order. It does not replace the base order status. A process response includes a suggested base order status, but advancing the process does not implicitly perform the base order transition.

### 13.1 Stages and graph

```text
OrderImport
  <-> WorkPreparation
        <-> PointingInWork
              <-> InControlWork
                    <-> ProcessManagement -> Completed

Operational stages can enter Escalation.
Resolving the last open escalation returns to its recorded return stage.
```

More precisely:

- `OrderImport` advances to `WorkPreparation`;
- `WorkPreparation` advances to `PointingInWork` or back to `OrderImport`;
- `PointingInWork` advances to `InControlWork` or back to `WorkPreparation`;
- `InControlWork` advances to `ProcessManagement` or back to `PointingInWork`;
- `ProcessManagement` advances to `Completed` or back to `InControlWork`;
- `Completed` is terminal;
- escalation is available from operational stages and blocks ordinary advancement while active.

The domain/store graph defines `ProcessManagement -> Completed`, but the current HTTP `ValidateAdvance` check rejects any terminal target before the store is called. Consequently, `POST /api/v1/orders/{id}/process/advance` cannot currently complete a process through that request path, despite the underlying graph containing the edge.

### 13.2 Stage tasks and forced advancement

Entering a stage seeds its task templates once. Tasks have sequence, title, description, role, required flag, completion actor, and completion time. Normally, all required tasks in the current stage must be done before advancing.

A request with `force=true` bypasses the required-task gate and requires `process:manage` in addition to the route's `process:advance`. Force does not bypass the stage graph or active-escalation rule.

The board groups processes into lanes, exposes task/sync counters and next stages, and sorts escalated work first and then by stage age. Metrics include stage counts, open tasks, pending/failed synchronization, active age, and recent completion.

### 13.3 Escalations

Levels are `L1`, `L2`, and `L3`. Reasons are `SLA_BREACH`, `STAGE_STALLED`, `BLOCKED`, `CUSTOMER`, `QUALITY`, and `MANUAL`.

Raising an escalation records the current and return stages, assigns level/reason/note/owner, moves the process to the escalation lane, seeds escalation tasks, queues relevant sync jobs, writes process events, and optionally notifies the target user.

Resolving requires a resolution note. While other escalations remain open, the process stays escalated. Resolving the last one returns to the recorded return stage, with `InControlWork` as the defensive fallback, and queues the corresponding jobs.

Automatic escalation evaluates stage budgets and SLA:

| Stage | Budget before stalled-stage escalation |
|---|---:|
| `OrderImport` | 4 hours |
| `WorkPreparation` | 8 hours |
| `PointingInWork` | 4 hours |
| `InControlWork` | 16 hours |
| `Escalation` | 4 hours |
| `ProcessManagement` | 8 hours |

SLA breaches suggest L2, or L3 when at least four hours overdue. Stalled stages suggest L1. Already escalated, terminal, or invalid-stage processes are skipped. The main process runs this evaluation on `SWI_AUTO_ESCALATE_MINUTES`; an authorized manual endpoint is also available.

### 13.4 External synchronization jobs

Stage transitions consult a built-in plan and insert jobs for systems such as AFAS, Omnitracker, Intune, Knox, and Apple Business Manager. Jobs contain system, operation (`READ`, `CREATE`, `UPDATE`, `DELETE`), direction, entity, reason, JSON payload, status, attempts, response/error, timestamps, and an idempotency key.

The lifecycle is:

```text
PENDING -> IN_PROGRESS -> DONE
                       -> FAILED
                       -> SKIPPED

FAILED, IN_PROGRESS, or SKIPPED -> PENDING through retry
```

Integration workers use `integrations:manage` to claim pending jobs and complete them. Claiming moves jobs to `IN_PROGRESS` and increments `attempts`. Completion accepts only `DONE`, `FAILED`, or `SKIPPED`; decided jobs cannot be completed again. Retry accepts `FAILED`, `IN_PROGRESS`, or `SKIPPED`, returns the job to `PENDING`, and clears its response, error, and completion timestamp. Idempotency keys include order, source/target stage, plan index, system, and operation to suppress duplicate planned jobs.

A new process import creates the order/process pair and queues an initial AFAS order-lines `READ` job. Importing an already existing order number returns the existing order and ensures its process rather than creating a duplicate or another initial import job.

This repository supplies the queue and API contract, not credentials or live connectors for the external systems.

## 14. Server-sent events

`GET /api/v1/events` requires an authenticated user with `orders:list`. The stream starts with a connected comment, sends a heartbeat comment every 25 seconds, and disables proxy buffering with `X-Accel-Buffering: no`.

Named events are:

| Event | Meaning |
|---|---|
| `orders` | Order/process-affecting data changed. |
| `checklist` | Pick-list data changed. |
| `manual` | Product/order-manual data changed. |
| `comment` | Order comments changed. |
| `automation` | Automation rules/runs changed. |
| `capa` | CAPA data changed. |
| `notification` | A specific user's notifications changed. |

Messages are small JSON hints, optionally containing order ID, actor, and recipient. They are not full resource payloads and have no replay IDs. Each subscriber has a buffered channel of 16; publishing drops a message for a subscriber whose buffer is full rather than blocking all clients.

Consequently, SSE is not a durable or guaranteed event log. Clients must refetch authoritative REST state after a hint. The frontend also uses periodic refresh/notification polling as a fallback and reconnects streams when needed.

## 15. Browser frontend

### 15.1 Core architecture

The main UI is a plain JavaScript single-page application loaded from `index.html`. It uses hash routes such as `#/orders/123`, global shared state in `window.cockpitState`, feature modules, and same-origin fetch calls. There is no React/Vue runtime or npm build requirement.

`core.js` supplies DOM helpers, API handling, permission checks, formatting, escaping, dialogs, toast UI, preferences, and shared state. `app.js` coordinates navigation and page lifecycle. `auth.js` coordinates password/QR login, session restoration, refresh timers, and application entry.

Role/permission maps in JavaScript are presentation aids. The Go middleware remains authoritative.

### 15.2 Main JavaScript modules

| File | Responsibility |
|---|---|
| `core.js` | Shared state, API helper, permissions, formatting, escaping, reusable UI, and preferences. |
| `auth.js` | Password and QR login, logout, session restoration, application entry, and refresh timers. |
| `app.js` | Hash routing, pane selection, navigation guards, and global UI coordination. |
| `orders.js`, `ordercreate.js`, `order-pages.js` | Order lists, creation, filtering, bulk actions, and order-detail tabs/pagination/layout. |
| `detail.js` | Order detail, hold/QC/audit/comment interactions. |
| `dashboard.js`, `workload.js`, `dayplan.js` | Operational metrics, assignment/workload views, and daily goals. |
| `work.js` | Customer work flow and active work sessions. |
| `quality.js`, `qcbench.js` | Quality analytics and QC queue/context/submission. |
| `capa.js` | CAPA list/detail/update/verification UI. |
| `automations.js` | Automation rule administration and run history. |
| `checklists.js` | Pick-list views and item operations. |
| `manuals.js` | Product templates, order-manual instances, answers, and flags. |
| `si.js` | Customers, projects, SI configuration/status, and project checklists. |
| `users.js` | Users, departments, permissions, and pairing-code administration. |
| `devices.js` | Preparation-log device list, parsed/raw detail, run history, and deletion. |
| `notifications.js` | Notification loading, unread state, and display. |
| `config.js` | Browser-local pick-list/configuration import and rendering rules. |
| `scan.js`, `offline-scanner.js` | Scanner UI, user-scoped queue, retry, and synchronization. |
| `pairguide.js`, `launch.js` | Native/client pairing and launch support. |
| `qrapprove.js` | Phone-side QR-login approval. |
| `ui.js` | Additional shared page/UI behavior. |

`frontend/app.js` and `frontend/style.css` are legacy monolithic assets; the primary modular page uses `frontend/js/app.js` and `frontend/styles.css`.

### 15.3 Browser-local configuration

The Config area can import JSON and Excel pick-list/configuration data and stores relevant settings in browser local storage. SheetJS and QRCode.js are loaded from cdnjs by the main HTML; CSP explicitly allows that script origin. If the CDN is unavailable, Excel-specific import/export features degrade while the same-origin application remains usable.

This browser configuration is distinct from server environment configuration and SQLite `app_settings`.

### 15.4 Auxiliary pages and standalone build

- `scan.html` is the scanner-focused page.
- `pair-guide.html` and `launch.html` support paired clients.
- `qr-approve.html` is the phone approval page.
- `checkin.html` is an auxiliary static page.
- `frontend/test/test.html` contains browser tests.

`build.py` and `build.ps1` inline `styles.css` and a defined subset of JavaScript modules into `cockpit-standalone.html`, removing the external SheetJS script tag in that generated variant. The standalone file is a legacy/convenience subset, not a guaranteed feature-for-feature replacement for the modular application.

## 16. Preparation-log ingestion

`POST /api/v1/prep-logs` is intentionally public so preparation tooling can submit before a user session exists. It is limited to 30 submissions per client IP per minute and a 512 KiB log payload, with a small multipart envelope allowance.

Accepted forms are:

- `multipart/form-data` with a `file` field;
- a raw request body, optionally named with `X-File-Name`.

The parser accepts UTF-8 or Windows-1252, normalizes line endings, and validates that the content resembles an Esprit preparation log. Among other structural checks, it requires sufficient timestamped lines, hardware sections, and a valid starting-host header. Invalid content returns 422 `not_a_prep_log`.

Parsed output includes device identity, hardware/software details, sections, update command results, issues, completion state, and statistics. Devices are upserted by uppercase system UUID when available, otherwise by uppercase computer name. Repeat submissions increment the run count and update first/last-seen metadata. Both parsed JSON and normalized raw text are stored.

Listing, viewing, and deleting preparation devices requires `users:manage`.

## 17. SQLite persistence and migrations

### 17.1 Connection behavior

The server uses `modernc.org/sqlite`, a pure-Go SQLite driver, through `database/sql`. The DSN enables:

- a 5-second busy timeout;
- WAL journal mode;
- foreign-key enforcement.

The pool is limited to one open and one idle connection. This deliberately serializes database access through the process and matches SQLite's single-writer model. Feature stores use explicit transactions for multi-row invariants.

### 17.2 Schema management

On every startup, the server executes `CREATE TABLE/INDEX IF NOT EXISTS` statements and then inspects existing table columns with `PRAGMA table_info`. Missing legacy columns are added with `ALTER TABLE ... ADD COLUMN`; the scan idempotency index and barcode history table are also ensured.

This is an additive, introspection-based migration strategy, not a numbered migration framework. It supports the repository's legacy databases without requiring a manual migration command. Destructive schema changes would require a deliberately designed new migration.

### 17.3 Table catalog

| Area | Tables |
|---|---|
| Orders and quality | `orders`, `holds`, `qc_checks`, `audit_logs` |
| Identity and sessions | `users`, `api_tokens`, `pair_codes`, `departments`, `user_departments` |
| Collaboration and activity | `comments`, `comment_mentions`, `notifications`, `scan_events`, `barcode_scans` |
| Work and planning | `app_settings`, `day_goals`, `work_sessions`, `checklist_items`, `customer_workflows` |
| Product manuals | `products`, `manual_blocks`, `order_manuals`, `order_manual_blocks`, `manual_tick_log` |
| SI | `customers`, `projects`, `system_integrations`, `si_project_links`, `si_events`, `project_checklist_items` |
| SWI process | `order_process`, `process_events`, `process_tasks`, `escalations`, `external_sync_jobs` |
| Automation and CAPA | `automation_rules`, `automation_runs`, `corrective_actions`, `capa_events` |
| Preparation logs | `prep_devices` |

Important relationships include:

- order-owned history generally cascades when an order is deleted at the database level;
- manual instances reference both their order and source product, while copied blocks reference the instance;
- comments have separate mention links and notifications reference usernames rather than user foreign keys;
- process, escalation, task, event, and sync-job records key directly to orders;
- CAPA events reference corrective actions, which reference orders and optionally QC checks;
- automation runs reference their rule and order and cascade when either parent is deleted.

### 17.4 Backup and restore

`GET /api/v1/admin/backup` requires `users:manage`. The server executes SQLite `VACUUM INTO` into a temporary file, streams it as a timestamped `.db` download, and removes the temporary file. This produces a consistent standalone database without requiring a manual copy of WAL sidecar files.

Back up separately:

- the paired-client JSON key store (`COCKPIT_CLIENT_KEYS`), which contains sensitive client secrets;
- any external configuration/deployment files;
- browser-local data if it matters, because local storage is not in SQLite;
- native pairing files on client machines if those installations must be preserved.

To restore, stop the server, replace the database with a known-good backup, and start against that `DB_PATH`. Do not leave an old `-wal` or `-shm` sidecar from a different database generation beside the restored file.

## 18. Security model and deployment

### 18.1 Browser and HTTP protections

Every response passes through security headers that include:

- a same-origin CSP with cdnjs allowed for scripts;
- `X-Content-Type-Options: nosniff`;
- `X-Frame-Options: DENY` and CSP `frame-ancestors 'none'`;
- same-origin referrer and resource policies;
- camera/microphone/geolocation restrictions;
- HSTS when the effective scheme is HTTPS.

For `POST`, `PUT`, `PATCH`, and `DELETE`, an `Origin` header, when present, must match the request's effective scheme/host/port. Combined with SameSite cookies, this protects browser write paths against cross-origin request forgery. Nonbrowser clients without an `Origin` header still require their normal authentication and authorization.

The API has no general cross-origin CORS grant. Deploy the UI and API on the same origin.

### 18.2 Rate-limit and ephemeral-state scope

Login, QR, pairing, launch, and preparation-log limits are in-memory and process-local. SSE subscriptions, QR tickets, paired-client browser sessions, and automation-worker mutexes are also process-local. Database-backed sessions, users, orders, CAPA, and rules are persistent; paired-client key definitions persist separately in the JSON key store.

The intended deployment is therefore a single application process. Multiple replicas would not share these in-memory structures and would also contend for one SQLite database. Use one writer/process unless the architecture is deliberately changed.

### 18.3 Reverse proxy and TLS

For HTTPS deployment:

1. Run the Go server on a private/unprivileged port.
2. Terminate TLS at a trusted reverse proxy.
3. Proxy static, API, and SSE paths on the same public origin.
4. Disable response buffering for `/api/v1/events` and allow long-lived connections.
5. Set `COCKPIT_PUBLIC_URL` to the external origin.
6. Set `COCKPIT_TRUST_PROXY=1` only when forwarded headers are sanitized by the proxy.
7. Keep the database and client-key files on persistent storage with restrictive permissions.

Secure-cookie and HSTS behavior depends on the effective HTTPS scheme, so forwarded-protocol configuration must be correct.

### 18.4 Secrets and operational data

- Set a strong bootstrap password explicitly or capture the one-time generated password from first-boot logs.
- Password hashes and session-token digests are stored in SQLite; raw session tokens are returned only to the client.
- The client key store contains raw pairing secrets and is written with mode `0600` through a temporary-file rename.
- Preparation logs can contain hardware identifiers, hostnames, MAC addresses, command output, and uploader IP addresses. Treat them as operational/personal data and restrict access accordingly.
- Backup downloads contain the entire database and must be protected as sensitive artifacts.

## 19. Native client and client-only mode

### 19.1 Pairing protocol

Client-only mode is an additional UI gate, not a replacement for user login.

1. An administrator creates a pairing code or QR through the admin endpoints.
2. The unpaired client submits the code, a machine identifier, and a display name to `POST /api/v1/pair`.
3. The server atomically consumes the unexpired, unconsumed code and generates a client ID plus a 32-byte secret.
4. The secret is bound to that machine identifier in the JSON key store. A successful response also creates a 12-hour `ckdpair` client session; a user-bound pairing code can also establish that user's normal login session.
5. Later launches compute `HMAC-SHA256(key, timestamp + "|" + machine)` and present the hex proof with client ID, timestamp, and machine.
6. The server checks the client ID, exact machine binding, proof, and a ±300-second timestamp window. A valid launch creates a new 12-hour `ckdpair` cookie.

Pairing and launch endpoints are rate-limited to 10 attempts per IP per 10 minutes. Revoking a paired client removes its ability to create future valid proofs.

In client-only mode, API paths and a small set of pairing/approval/health pages remain reachable through the client guard; their own authentication rules still apply. Other UI/static paths require `ckdpair` or a valid signed launch URL. Signed launch parameters are removed by redirect after the cookie is set.

### 19.2 Native WebView implementation

`client/main.go` uses `webview_go` and opens a 1024×768 window titled `Esprit ICT`.

- Server selection uses `COCKPIT_SERVER`, then the saved pairing file, then the compiled default.
- The pairing file is `cockpit-client.json` beside the executable.
- The machine ID is SHA-256 of `COMPUTERNAME|PROCESSOR_IDENTIFIER`, which is primarily suited to the intended Windows environment.
- If the saved machine ID differs, the native client deletes the local pairing file and requires re-pairing.
- On first run it opens `pair-guide.html` and binds `clientMachine` and `clientPairSave` for the page.
- On later runs it navigates to a signed root URL using the saved key.
- Pairing-file writes use a temporary file, mode `0600`, and rename.

Build the native client from its module/directory with the platform WebView/GTK dependencies required by `webview_go`:

```bash
cd client
go build -o cockpit-client .
```

A server-only Go test/build environment does not necessarily include the native WebKit/GTK development packages.

## 20. Build, test, and change workflow

### 20.1 Server checks

Use the toolchain declared by the module; the root `go.mod` currently declares Go 1.26.4. Run from the repository root:

```bash
gofmt -w main.go
find pkg client -type f -name '*.go' -exec gofmt -w {} +
go test ./...
go vet ./...
go mod verify
```

When the native WebView dependencies are unavailable, server/package tests can still be run explicitly:

```bash
go test ./pkg/... .
```

For concurrency-enabled environments, also run:

```bash
go test -race ./...
```

Do not run dependency tidy operations merely to repair an incomplete local module/cache setup; verify that the original `go.mod` and `go.sum` remain intact.

### 20.2 Frontend and script checks

```bash
find frontend -type f -name '*.js' -print0 | xargs -0 -n1 node --check
python3 -m py_compile frontend/build.py
bash -n test.sh
```

Inline scripts in HTML should also be extracted and parsed after edits to HTML or the cleanup tooling. The standalone HTML should be regenerated deliberately with `python3 frontend/build.py` or `frontend/build.ps1`, rather than edited as though it were the primary source.

### 20.3 Live API regression

Start an isolated server with a temporary database and known bootstrap credentials, then run:

```bash
BASE_URL=http://localhost:8080 \
ADMIN_USERNAME=admin \
ADMIN_PASSWORD='the-test-password' \
bash test.sh
```

The live suite exercises password authentication, lifecycle/hold/QC gates, users, attention, scanning, comments, assignment, bulk operations, stale-write protection, SSE, backup, and product manuals. The Go domain/store/API test suites additionally cover CAPA, automations, SI, SWI, security, migrations, and offline-scan idempotency. Use a disposable database because the live suite creates real records.

Browser smoke coverage should additionally verify cookie and Bearer sessions, QR approval, SSE refresh, offline scan replay, re-login synchronization, and client-only pairing. Native-client UI execution requires the WebView platform dependencies.

### 20.4 Making changes safely

1. Identify the owning domain package before changing SQL or handlers.
2. Preserve transaction boundaries around state plus history/notification writes.
3. Keep route permission checks and handler-level object rules together in review.
4. Update frontend rendering only after the server contract is defined.
5. Add domain/store tests for invariant changes and API tests for status/access changes.
6. Update this guide and the complete route catalog when routes or behavior change.
7. Run formatting, static checks, unit tests, and live regression before deployment.

## 21. Complete HTTP route catalog

Access labels below describe route middleware. `Authenticated` means a valid session is required; a listed permission is checked after authentication. Handler-level object rules still apply, especially for work sessions, CAPA, and forced process advancement. Public pairing/QR/prep endpoints have their own proof, ticket, payload, and rate-limit checks described above.

The table covers all 145 method-specific health and API registrations. The static `/` file handler and unmatched `/api/` fallback are covered in Section 4.1 rather than listed as API operations.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/healthz` | Public | `handleHealth` |
| POST | `/api/v1/auth/login` | Public | `handleLogin` |
| POST | `/api/v1/auth/logout` | Authenticated | `handleLogout` |
| POST | `/api/v1/pair` | Public | `handleClientPair` |
| POST | `/api/v1/launch` | Public | `handleClientLaunch` |
| GET | `/api/v1/qr-login` | Public | `handleQRLoginCreate` |
| GET | `/api/v1/qr-login/status` | Public | `handleQRLoginStatus` |
| POST | `/api/v1/qr-login/approve` | Authenticated | `handleQRLoginApprove` |
| POST | `/api/v1/prep-logs` | Public | `handlePostPrepLog` |
| GET | `/api/v1/admin/prep-devices` | Authenticated + `users:manage` | `handleListPrepDevices` |
| GET | `/api/v1/admin/prep-devices/{id}` | Authenticated + `users:manage` | `handleGetPrepDevice` |
| DELETE | `/api/v1/admin/prep-devices/{id}` | Authenticated + `users:manage` | `handleDeletePrepDevice` |
| GET | `/api/v1/admin/pair-qr` | Authenticated + `users:manage` | `handleAdminPairQR` |
| POST | `/api/v1/admin/pair-code` | Authenticated + `users:manage` | `handleAdminPairCode` |
| GET | `/api/v1/admin/client-keys` | Authenticated + `users:manage` | `handleListPairedClients` |
| POST | `/api/v1/admin/client-keys/{id}/revoke` | Authenticated + `users:manage` | `handleRevokePairedClient` |
| GET | `/api/v1/auth/me` | Authenticated | `handleMe` |
| GET | `/api/v1/users` | Authenticated + `users:list` | `handleListUsers` |
| POST | `/api/v1/users` | Authenticated + `users:manage` | `handleCreateUser` |
| POST | `/api/v1/users/{id}/role` | Authenticated + `users:manage` | `handleSetUserRole` |
| POST | `/api/v1/users/{id}/password` | Authenticated + `users:manage` | `handleResetPassword` |
| DELETE | `/api/v1/users/{id}` | Authenticated + `users:manage` | `handleDeleteUser` |
| GET | `/api/v1/orders` | Authenticated + `orders:list` | `handleListOrders` |
| POST | `/api/v1/orders` | Authenticated + `orders:create` | `handleCreateOrder` |
| GET | `/api/v1/orders/{id}` | Authenticated + `orders:view` | `handleGetOrder` |
| POST | `/api/v1/orders/{id}/transition` | Authenticated + `orders:transition` | `handleTransition` |
| POST | `/api/v1/orders/{id}/assign` | Authenticated + `orders:transition` | `handleAssign` |
| POST | `/api/v1/orders/{id}/holds` | Authenticated + `holds:create` | `handlePlaceHold` |
| POST | `/api/v1/holds/{id}/resolve` | Authenticated + `holds:resolve` | `handleResolveHold` |
| POST | `/api/v1/orders/{id}/qc` | Authenticated + `qc:submit` | `handleSubmitQC` |
| GET | `/api/v1/orders/{id}/audit` | Authenticated + `audit:view` | `handleAudit` |
| POST | `/api/v1/orders/bulk-transition` | Authenticated + `orders:transition` | `handleBulkTransition` |
| POST | `/api/v1/holds/bulk-resolve` | Authenticated + `holds:resolve` | `handleBulkResolve` |
| GET | `/api/v1/orders/{id}/checklist` | Authenticated + `orders:view` | `handleGetChecklist` |
| GET | `/api/v1/stats/qc` | Authenticated + `qc:submit` | `handleQCStats` |
| GET | `/api/v1/qc/queue` | Authenticated + `qc:submit` | `handleQCQueue` |
| GET | `/api/v1/qc/reasons` | Authenticated + `qc:submit` | `handleQCReasons` |
| GET | `/api/v1/qc/context/{id}` | Authenticated + `qc:submit` | `handleQCContext` |
| GET | `/api/v1/capa` | Authenticated + `orders:list` | `handleListCAPA` |
| POST | `/api/v1/capa` | Authenticated + `qc:submit` | `handleCreateCAPA` |
| GET | `/api/v1/capa/{id}` | Authenticated + `orders:list` | `handleGetCAPA` |
| POST | `/api/v1/capa/{id}` | Authenticated | `handleUpdateCAPA` |
| POST | `/api/v1/capa/{id}/verify` | Authenticated + `qc:submit` | `handleVerifyCAPA` |
| GET | `/api/v1/automations` | Authenticated + `users:manage` | `handleListAutomationRules` |
| POST | `/api/v1/automations` | Authenticated + `users:manage` | `handleCreateAutomationRule` |
| POST | `/api/v1/automations/{id}` | Authenticated + `users:manage` | `handleUpdateAutomationRule` |
| DELETE | `/api/v1/automations/{id}` | Authenticated + `users:manage` | `handleDeleteAutomationRule` |
| GET | `/api/v1/automation-runs` | Authenticated + `users:manage` | `handleListAutomationRuns` |
| POST | `/api/v1/automations/evaluate` | Authenticated + `users:manage` | `handleEvaluateAutomations` |
| GET | `/api/v1/dayplan` | Authenticated + `orders:list` | `handleGetDayplan` |
| PUT | `/api/v1/dayplan/goal` | Authenticated + `qc:submit` | `handlePutDayGoal` |
| POST | `/api/v1/dayplan/send` | Authenticated + `qc:submit` | `handleSendDayGoal` |
| GET | `/api/v1/stats/ops` | Authenticated + `orders:list` | `handleOpsStats` |
| GET | `/api/v1/workflow` | Authenticated + `orders:list` | `handleGetWorkflow` |
| PUT | `/api/v1/workflow` | Authenticated + `users:manage` | `handlePutWorkflow` |
| GET | `/api/v1/work/sessions` | Authenticated + `users:manage` | `handleListAllWork` |
| GET | `/api/v1/work/mine` | Authenticated + `orders:view` | `handleListMyWork` |
| GET | `/api/v1/orders/{id}/work` | Authenticated + `orders:view` | `handleGetWork` |
| POST | `/api/v1/orders/{id}/work/start` | Authenticated + `orders:view` | `handleStartWork` |
| POST | `/api/v1/orders/{id}/work/update` | Authenticated + `orders:view` | `handleUpdateWork` |
| POST | `/api/v1/orders/{id}/work/end` | Authenticated + `orders:view` | `handleEndWork` |
| POST | `/api/v1/orders/{id}/checklist` | Authenticated + `pick:use` | `handleSetChecklist` |
| POST | `/api/v1/checklist/{id}/tick` | Authenticated + `pick:use` | `handleTickChecklistItem` |
| POST | `/api/v1/checklist/{id}/untick` | Authenticated + `pick:use` | `handleUntickChecklistItem` |
| GET | `/api/v1/checklists` | Authenticated + `orders:view` | `handleListChecklists` |
| POST | `/api/v1/checklists/demo` | Authenticated + `orders:create` | `handleSeedDemoChecklists` |
| GET | `/api/v1/products` | Authenticated + `orders:list` | `handleListProducts` |
| POST | `/api/v1/products` | Authenticated + `manuals:manage` | `handleCreateProduct` |
| GET | `/api/v1/products/{id}` | Authenticated + `orders:view` | `handleGetProduct` |
| POST | `/api/v1/products/{id}` | Authenticated + `manuals:manage` | `handleUpdateProduct` |
| DELETE | `/api/v1/products/{id}` | Authenticated + `manuals:manage` | `handleDeleteProduct` |
| POST | `/api/v1/products/quick` | Authenticated + `manuals:manage` | `handleQuickManual` |
| POST | `/api/v1/products/{id}/approve` | Authenticated + `manuals:approve` | `handleApproveProduct` |
| POST | `/api/v1/products/{id}/department` | Authenticated + `manuals:manage` | `handleSetProductDepartment` |
| POST | `/api/v1/si/customers/{id}/coordinator` | Authenticated + `customers:manage` | `handleSetCustomerCoordinator` |
| GET | `/api/v1/customers` | Authenticated + `orders:list` | `handleListCustomersDirectory` |
| POST | `/api/v1/customers` | Authenticated + `customers:manage` | `handleCreateCustomerManaged` |
| GET | `/api/v1/customers/flows` | Authenticated + `orders:list` | `handleListCustomerFlows` |
| POST | `/api/v1/products/{id}/blocks` | Authenticated + `manuals:manage` | `handleAddManualBlock` |
| POST | `/api/v1/products/{pid}/blocks/{bid}` | Authenticated + `manuals:manage` | `handleUpdateManualBlock` |
| POST | `/api/v1/products/{pid}/blocks/{bid}/move` | Authenticated + `manuals:manage` | `handleMoveManualBlock` |
| DELETE | `/api/v1/products/{pid}/blocks/{bid}` | Authenticated + `manuals:manage` | `handleDeleteManualBlock` |
| POST | `/api/v1/orders/{id}/manuals` | Authenticated + `pick:use` | `handleInstantiateManual` |
| GET | `/api/v1/orders/{id}/manuals` | Authenticated + `orders:view` | `handleListOrderManuals` |
| POST | `/api/v1/manuals/{mid}/blocks/{bid}/answer` | Authenticated + `pick:use` | `handleAnswerManualBlock` |
| POST | `/api/v1/manuals/{mid}/blocks/{bid}/flag` | Authenticated + `manuals:manage` | `handleFlagManualBlock` |
| DELETE | `/api/v1/manuals/{mid}` | Authenticated + `manuals:manage` | `handleDeleteManual` |
| GET | `/api/v1/manuals` | Authenticated + `orders:view` | `handleListManuals` |
| GET | `/api/v1/attention` | Authenticated + `orders:list` | `handleAttention` |
| POST | `/api/v1/scan` | Authenticated + `scan:use` | `handleScan` |
| GET | `/api/v1/orders/{id}/scans` | Authenticated + `orders:list` | `handleListScans` |
| GET | `/api/v1/orders/{id}/comments` | Authenticated + `comments:read` | `handleListComments` |
| POST | `/api/v1/orders/{id}/comments` | Authenticated + `comments:post` | `handleAddComment` |
| GET | `/api/v1/orders/{id}/barcode` | Authenticated + `orders:view` | `handleGetBarcode` |
| POST | `/api/v1/orders/{id}/barcode` | Authenticated + `orders:transition` | `handleGenerateBarcode` |
| POST | `/api/v1/orders/{id}/omnitracker` | Authenticated + `orders:transition` | `handleUpdateOmnitrackerInfo` |
| GET | `/api/v1/orders/{id}/barcode/history` | Authenticated + `orders:view` | `handleBarcodeScanHistory` |
| GET | `/api/v1/scan/barcode` | Authenticated + `scan:use` | `handleScanBarcode` |
| POST | `/api/v1/process/orders/import` | Authenticated + `process:advance` | `handleImportOrder` |
| GET | `/api/v1/process/board` | Authenticated + `process:view` | `handleProcessBoard` |
| GET | `/api/v1/process/metrics` | Authenticated + `process:view` | `handleProcessMetrics` |
| GET | `/api/v1/process/escalations` | Authenticated + `process:view` | `handleListEscalations` |
| POST | `/api/v1/process/escalations/auto` | Authenticated + `process:manage` | `handleAutoEscalate` |
| POST | `/api/v1/process/escalations/{id}/resolve` | Authenticated + `process:escalate` | `handleResolveEscalation` |
| GET | `/api/v1/process/sync-jobs` | Authenticated + `process:view` | `handleListSyncJobs` |
| POST | `/api/v1/process/sync-jobs/claim` | Authenticated + `integrations:manage` | `handleClaimSyncJobs` |
| POST | `/api/v1/process/sync-jobs/{id}/complete` | Authenticated + `integrations:manage` | `handleCompleteSyncJob` |
| POST | `/api/v1/process/sync-jobs/{id}/retry` | Authenticated + `integrations:manage` | `handleRetrySyncJob` |
| GET | `/api/v1/orders/{id}/process` | Authenticated + `process:view` | `handleGetProcess` |
| POST | `/api/v1/orders/{id}/process/advance` | Authenticated + `process:advance` | `handleAdvanceProcess` |
| POST | `/api/v1/orders/{id}/process/escalate` | Authenticated + `process:escalate` | `handleEscalateProcess` |
| GET | `/api/v1/orders/{id}/process/tasks` | Authenticated + `process:view` | `handleProcessTasks` |
| POST | `/api/v1/process/tasks/{id}/tick` | Authenticated + `process:advance` | `handleTickProcessTask` |
| POST | `/api/v1/process/tasks/{id}/untick` | Authenticated + `process:advance` | `handleUntickProcessTask` |
| GET | `/api/v1/orders/{id}/process/events` | Authenticated + `process:view` | `handleProcessEvents` |
| GET | `/api/v1/notifications` | Authenticated | `handleListNotifications` |
| POST | `/api/v1/notifications/read` | Authenticated | `handleMarkNotificationsRead` |
| GET | `/api/v1/users/brief` | Authenticated + `orders:list` | `handleListUsersBrief` |
| GET | `/api/v1/si/customers` | Authenticated + `si:list` | `handleListCustomers` |
| POST | `/api/v1/si/customers` | Authenticated + `si:create` | `handleCreateCustomer` |
| GET | `/api/v1/si/customers/{number}/projects` | Authenticated + `si:list` | `handleListProjects` |
| POST | `/api/v1/si/projects` | Authenticated + `si:create` | `handleCreateProject` |
| GET | `/api/v1/si/sis` | Authenticated + `si:list` | `handleListSIs` |
| POST | `/api/v1/si/sis` | Authenticated + `si:create` | `handleCreateSI` |
| GET | `/api/v1/si/sis/{id}` | Authenticated + `si:view` | `handleGetSI` |
| POST | `/api/v1/si/sis/{id}` | Authenticated + `si:update` | `handleUpdateSI` |
| POST | `/api/v1/si/sis/{id}/transition` | Authenticated + `si:transition` | `handleTransitionSI` |
| POST | `/api/v1/si/sis/{id}/projects` | Authenticated + `si:projects` | `handleSetSIProjects` |
| GET | `/api/v1/si/sis/{id}/events` | Authenticated + `si:view` | `handleSIEvents` |
| POST | `/api/v1/si/seed` | Authenticated + `si:create` | `handleSeedDemoSI` |
| GET | `/api/v1/si/projects/{projectId}/checklist` | Authenticated + `si:view` | `handleListChecklist` |
| POST | `/api/v1/si/projects/{projectId}/checklist` | Authenticated + `si:update` | `handleAddChecklistItem` |
| POST | `/api/v1/si/projects/checklist/{itemId}/tick` | Authenticated + `si:update` | `handleSITickChecklistItem` |
| POST | `/api/v1/si/projects/checklist/{itemId}/untick` | Authenticated + `si:update` | `handleSIUntickChecklistItem` |
| DELETE | `/api/v1/si/projects/checklist/{itemId}` | Authenticated + `si:update` | `handleDeleteChecklistItem` |
| GET | `/api/v1/departments` | Authenticated + `users:list` | `handleListDepartments` |
| POST | `/api/v1/departments` | Authenticated + `users:manage` | `handleCreateDepartment` |
| DELETE | `/api/v1/departments/{id}` | Authenticated + `users:manage` | `handleDeleteDepartment` |
| POST | `/api/v1/departments/{id}/users` | Authenticated + `users:manage` | `handleAddUserToDepartment` |
| DELETE | `/api/v1/departments/{id}/users/{userId}` | Authenticated + `users:manage` | `handleRemoveUserFromDepartment` |
| GET | `/api/v1/departments/{id}/users` | Authenticated + `users:manage` | `handleListDepartmentUsers` |
| POST | `/api/v1/departments/{id}/permissions` | Authenticated + `users:manage` | `handleSetDepartmentPermissions` |
| GET | `/api/v1/users/{id}/departments` | Authenticated + `users:list` | `handleListUserDepartments` |
| GET | `/api/v1/admin/backup` | Authenticated + `users:manage` | `handleAdminBackup` |
| GET | `/api/v1/events` | Authenticated + `orders:list` | `handleEvents` |

The catalog contains 145 registered routes. Permissions are enforced by middleware; handlers may apply additional object-level rules, notably CAPA ownership and verification checks.
