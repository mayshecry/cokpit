# Cockpit client (“Esprit ICT”)

Desktop launcher for the Cockpit UI — a WebView2 shell that proves its
identity to the server (client-only mode).

## Build (on Windows, in this folder)

```bat
go mod tidy
go build -ldflags="-H windowsgui" -o CockpitClient.exe .
```

WebView2 is preinstalled on Windows 10/11 (Win11 always; otherwise the
evergreen runtime installer may be needed once on very old images).

## Pairing

1. Start the server with `COCKPIT_CLIENT_ONLY=1`.
   The console prints a one-time 8-digit **PAIRING CODE** (e.g. `4821-9034`) on every boot.
2. Put `CockpitClient.exe` anywhere and start it → it opens the
   *Link this client* page.
3. Enter the code → the client stores `cockpit-client.json` (0600) beside
   the exe and continues into the app.
4. Done — every later launch auto-signs itself. Pairing is **one-time** per
   code: create more codes as an admin via
   `POST /api/v1/admin/pair-code` (Bearer admin token).

To unlink / re-pair a machine: delete its `cockpit-client.json`, or an admin
revokes the client via `POST /api/v1/admin/client-keys/{id}/revoke`.

## Notes

- The pairing key is machine-bound: copying `cockpit-client.json` to another
  PC breaks its proof (machine hash mismatch) and the client starts unpaired.
- The API (`/api/v1/**`) is **not** gated — normal Bearer-token auth keeps
  working for integrations and scripts.
- Server address resolution (first match wins): `COCKPIT_SERVER` env var
  (e.g. `https://cockpit.example.nl`) → `server` field of a pre-seeded
  `cockpit-client.json` next to the exe → `http://localhost`. After pairing
  the resolved address is stored into the pairing file.
