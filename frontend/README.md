# Cockpit — reimagined

An Esprit ICT-branded redesign of the Order Operations dashboard. Same stack (no build step,
no dependencies, no framework), same `/api/v1` contract, same feature set.

## Files

| File | Notes |
|---|---|
| `index.html` | Rebuilt markup — sidebar shell, split-screen login, semantic sections |
| `styles.css` | Full rewrite: light enterprise theme, design tokens |
| `app.js` | Rewritten SPA. **Every `fetch` path, method and payload is unchanged.** |
| `fonts/` | Self-hosted Archivo + Noto Sans, subsetted (96 KB total) |
| `cockpit-standalone.html` | Optional. Everything inlined into one file — previews, single-file handoff |
| `build.py` | Regenerates the standalone file from the sources |

Three files, drop-in. Serve them from the same origin as your API and they work — the
frontend makes same-origin relative requests to `/api/v1/...`, exactly as the original did.

`cockpit-standalone.html` is a convenience build with zero external references, for
contexts that won't fetch sibling files (sandboxed iframe previews, `file://`
double-click, emailing someone a copy). Edit the three sources, then `python3 build.py`.

## API contract — unchanged

Every endpoint the original `app.js` called, still called the same way:

| Method | Path | Sent | Expected back |
|---|---|---|---|
| `POST` | `/api/v1/auth/login` | `{ username, password }` | `{ token, user }` |
| `POST` | `/api/v1/auth/logout` | — (bearer only) | — |
| `GET` | `/api/v1/orders` | — | `{ orders: [...] }` |
| `POST` | `/api/v1/orders` | `{ orderNumber, targetCompletionAt? }` | — |
| `GET` | `/api/v1/orders/:id` | — | `{ order }` |
| `GET` | `/api/v1/orders/:id/audit` | — | `{ auditLogs: [...] }` |
| `POST` | `/api/v1/orders/:id/transition` | `{ status }` | — |
| `POST` | `/api/v1/orders/:id/holds` | `{ reason }` | — |
| `POST` | `/api/v1/orders/:id/qc` | `{ status, notes }` | — |
| `POST` | `/api/v1/holds/:id/resolve` | `{}` | — |
| `GET` | `/api/v1/users` | — | `{ users: [...] }` |
| `POST` | `/api/v1/users` | `{ username, displayName, role, password }` | — |
| `POST` | `/api/v1/users/:id/role` | `{ role }` | — |
| `POST` | `/api/v1/users/:id/password` | `{ password }` | — |
| `DELETE` | `/api/v1/users/:id` | — | — |

All authenticated requests send `Authorization: Bearer <token>` and
`Content-Type: application/json`.

**Fields read off the responses** (unchanged from the original):

- order — `id`, `orderNumber`, `status`, `targetCompletionAt`, `createdAt`, `updatedAt`,
  and on the detail endpoint also `onHold`, `activeHolds[]`, `qcChecks[]`, `sla.status`
- hold — `id`, `reason`, `createdBy`, `createdAt`
- qc check — `status`, `inspectorId`, `notes`, `createdAt`
- audit log — `action`, `performedBy`, `timestamp`
- user — `id`, `username`, `displayName`, `role`, `createdAt`

**Error shape** — non-2xx responses are read as `{ error: { message } }` and surfaced in a
toast; anything else falls back to `HTTP <status>`. A `401` on any path other than
`/auth/login` force-logs-out and clears storage.

Also identical: `localStorage` keys (`cockpit_token`, `cockpit_user`), the client-side
`rolePerms` map, `slaOf()` thresholds (breached at 0, warning at 4h), the `nextStates()`
transition graph, `Held_*` status handling, hash routes (`#/orders`, `#/users`) and the
20-second auto-refresh.

## Esprit ICT branding

Pulled from the live site rather than eyeballed:

| Token | Value | Source |
|---|---|---|
| `--brand` | `#ff8200` | the only fill colour in `EspritICT-Logo-Dark-Full-Colour.svg` |
| `--ink` | `#0a0303` | near-black used site-wide |
| `--font-head` | Archivo 700 | site heading face |
| `--font-body` | Noto Sans 400/600/700 | site body face |
| `--radius-pill` | `999px` | their fully-rounded CTA buttons |

- **Logo is the real vector**, lifted from their SVG — the orange mark and the wordmark
  as separate paths. The wordmark uses `currentColor`, so the same markup renders black
  on light surfaces and white on the dark login panel. Sidebar shows mark + `Cockpit`
  in a divider lockup, so the product name reads as an Esprit product, not a competitor.
- **Fonts are self-hosted, not hotlinked.** Subsetted to Latin + Dutch accents + the
  punctuation actually used (208 KB → 85 KB). This avoids the Google Fonts CDN, which is
  a live GDPR liability for EU-based companies after the German court rulings.
- Orange drives primary buttons, active nav/chips, focus rings, selected rows, the
  avatar, and the audit timeline head. Status semantics stay separate so `WARNING` amber
  never gets confused with brand orange.
- Login hero uses their dark-with-orange-glow treatment and the `voor samenwerkers`
  tagline; hero copy is in Dutch, app chrome left in English (say the word and I'll do
  the whole UI in Dutch).

## What changed

**Design** — light enterprise theme on a token system. Persistent left sidebar instead of
a topbar. Split-screen login. Stats grouped into *Pipeline* / *Service level* with
colour-coded accent rails. Bordered, softer badges. Sticky table headers, tabular
numerals, dashed-line audit timeline.

**UX additions**
- Sortable columns on every orders field (SLA sorts by urgency, not alphabetically)
- Status filter chips with live counts, replacing the `<select>`
- Real modals for create-order, add-user, change-role, reset-password and delete —
  no more `prompt()` / `confirm()`
- Stacked, dismissible toasts instead of one banner overwriting the last
- Loading skeletons and empty states, incl. a "clear filters" escape hatch
- Relative timestamps: `in 5h 0m`, `12h 0m ago`
- "Live · synced Ns ago" indicator that turns amber when stale
- Keyboard shortcuts: `/` search · `n` new · `r` refresh · `g o` / `g u` navigate ·
  `Esc` close · `?` cheatsheet
- Closable detail panel; table reflows to full width when nothing is selected
- Show/hide password, disabled submit state, inline login errors
- Role permission cards on the Users page
- Keyboard-navigable rows, `aria-sort`, `aria-current`, `prefers-reduced-motion`
- Unreachable-API failures now say so, rather than surfacing a raw `TypeError`
- Toasts moved to bottom-right — top-right collided with the page's primary action button

**Kept deliberately** — the `esc()` HTML escaper is applied to every interpolated value,
including all the new markup.

## Running it

The frontend is static. Serve it from the same origin as the API, or put a dev proxy in
front so `/api/v1/*` reaches your backend:

```
python3 -m http.server 3000     # static only — API calls will 404 until the backend is behind it
```

Without a backend on the same origin you'll get the login screen and a
"Cannot reach the API" / `HTTP 404` error on submit. That's expected.

## Troubleshooting

**Blank page.** Almost always means `app.js` didn't execute while `styles.css` did.
The app is client-rendered, so the CSS hides the shell and nothing reveals it.

Check the browser console and network tab for `app.js` — a 404 (files not deployed
side by side, wrong base path) or a CSP blocking inline/external scripts.

Since the redesign the login screen renders by default and JS only *hides* it when a
session exists, so this now degrades to a visible login form rather than a blank
screen — plus there's a `<noscript>` notice. If you still get nothing at all, neither
file is loading; if you get an unstyled page, only `styles.css` is missing.

For previews that don't fetch sibling files, use `cockpit-standalone.html`.

**Wrong fonts / everything falls back to Arial.** The `fonts/` directory didn't deploy
alongside `styles.css`. The `@font-face` rules use paths relative to the stylesheet.
