package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
)

type setupConfig struct {
	Port         string `json:"port"`
	AdminUser    string `json:"adminUser"`
	AdminPass    string `json:"adminPass"`
	SessionHours string `json:"sessionHours"`
	SLAHours     string `json:"slaHours"`
	CustomerNo   string `json:"customerNo"`
	CustomerName string `json:"customerName"`
}

const setupHTML = `<!DOCTYPE html>
<html lang="en" data-theme="dark">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Cockpit setup · Esprit ICT</title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' rx='7' fill='%23ee6d05'/%3E%3Cpath d='m9 17 5 5 9-11' stroke='%23fff' stroke-width='3.5' fill='none' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3e">
<style>
  :root {
    --brand: #ee6d05;
    --brand-bright: #ff8a1f;
    --brand-hover: #dd6200;
    --brand-soft: rgba(238, 109, 5, .14);
    --brand-line: rgba(238, 109, 5, .34);
    --bg: #09090b;
    --surface: rgba(22, 22, 26, 0.74);
    --border: rgba(255, 255, 255, 0.08);
    --text: #fafafa;
    --text-2: #d4d4d8;
    --muted: #a1a1aa;
    --r-md: 12px;
    --font-body: Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    --font-head: Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  }
  * { box-sizing: border-box; margin: 0; }
  html, body { height: 100%; }
  body {
    background: var(--bg);
    background-image:
      radial-gradient(ellipse 70% 42% at 50% -12%, rgba(238, 109, 5, 0.14), transparent 68%),
      radial-gradient(ellipse 45% 35% at 85% 108%, rgba(238, 109, 5, 0.06), transparent 70%);
    background-attachment: fixed;
    color: var(--text);
    font: 400 14px/1.5 var(--font-body);
    -webkit-font-smoothing: antialiased;
  }
  .wrap {
    min-height: 100%;
    display: grid;
    place-items: center;
    padding: 24px;
  }
  .card {
    width: min(440px, 92%);
    padding: 30px 30px 26px;
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    background: var(--surface);
    backdrop-filter: blur(20px);
    box-shadow: 0 24px 48px -16px rgba(0,0,0,0.6), inset 0 1px 0 rgba(255,255,255,0.06);
  }
  .brand {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 24px;
  }
  .brand .orb {
    width: 30px; height: 30px;
    border-radius: 9px;
    background: linear-gradient(135deg, #ff7a1a, #ea580c);
    color: #fff;
    display: grid;
    place-items: center;
    font-weight: 700;
    font-size: 13px;
    box-shadow: 0 4px 12px rgba(238, 109, 5, 0.4);
  }
  .brand-text strong {
    font: 640 16px var(--font-head);
    letter-spacing: -.02em;
  }
  .brand-text span {
    font-size: 10.5px;
    color: var(--muted);
    letter-spacing: .06em;
    text-transform: uppercase;
  }
  h1 {
    font: 640 22px var(--font-head);
    letter-spacing: -.03em;
    margin: 0 0 6px;
    color: var(--text);
  }
  p.sub {
    font-size: 13px;
    color: var(--muted);
    margin: 0 0 24px;
    line-height: 1.55;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 14px;
  }
  .field > label {
    font-size: 11px;
    font-weight: 620;
    letter-spacing: .1em;
    text-transform: uppercase;
    color: var(--muted);
  }
  input[type="text"], input[type="password"], input[type="number"] {
    width: 100%;
    min-height: 38px;
    padding: 8px 11px;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: rgba(12, 12, 15, 0.82);
    color: var(--text);
    font-size: 13.5px;
    transition: border-color .15s ease, box-shadow .15s ease;
  }
  input:focus {
    outline: none;
    border-color: var(--brand);
    box-shadow: 0 0 0 3px rgba(238, 109, 5, 0.2);
  }
  .row {
    display: flex;
    gap: 12px;
  }
  .row > div { flex: 1; }
  button {
    width: 100%;
    height: 44px;
    border-radius: 10px;
    border: 0;
    background: linear-gradient(180deg, var(--brand-bright), var(--brand));
    color: #fff;
    font: 640 15px var(--font-body);
    letter-spacing: -.01em;
    cursor: pointer;
    box-shadow: 0 8px 20px -8px rgba(238, 109, 5, 0.55);
    transition: filter .15s ease, transform .1s ease;
  }
  button:hover { filter: brightness(1.06); transform: translateY(-1px); }
  button:active { transform: translateY(0); }
  button:disabled { opacity: .5; cursor: not-allowed; filter: none; }
  #msg {
    margin-top: 14px;
    font-size: 12px;
    min-height: 18px;
  }
  .note {
    font-size: 11.5px;
    color: var(--muted);
    margin-top: 20px;
    line-height: 1.5;
  }
  b { color: var(--brand); }
  .ok { color: #22c55e; }
  .err { color: #ef4444; }
</style>
<div class="wrap">
  <div class="card">
    <div class="brand">
      <div class="orb">C</div>
      <div class="brand-text">
        <strong>Cockpit</strong>
        <span>Order Operations · Esprit ICT</span>
      </div>
    </div>
    <h1>Setup</h1>
    <p class="sub">Configure Cockpit before it deploys. Only <b>cockpit.db</b> is used and no accounts are created automatically &mdash; the admin account and customer below are created only because you configure them here.</p>
    <form id="f" onsubmit="return false;">
      <div class="field">
        <label>Listen port</label>
        <input name="port" type="number" value="80">
      </div>
      <div class="field">
        <label>Setup port (this page)</label>
        <input name="setupPort" type="number" value="492" disabled>
      </div>
      <div class="field">
        <label>Admin username</label>
        <input name="adminUser" type="text" value="admin">
      </div>
      <div class="field">
        <label>Admin password</label>
        <input name="adminPass" type="password">
      </div>
      <div class="field">
        <label>Customer number</label>
        <input name="customerNo" type="text" placeholder="e.g. 1010">
      </div>
      <div class="field">
        <label>Customer name</label>
        <input name="customerName" type="text" placeholder="e.g. Kenter">
      </div>
      <div class="row">
        <div class="field">
          <label>Session hours</label>
          <input name="sessionHours" type="number" value="24">
        </div>
        <div class="field">
          <label>SLA target hours</label>
          <input name="slaHours" type="number" value="24">
        </div>
      </div>
      <button type="button" id="deploy">Deploy Cockpit</button>
    </form>
    <div id="msg"></div>
    <div class="note">After deploy, Cockpit starts on the configured listen port, using cockpit.db. On later boots this setup page is skipped and port 492 is never used again.</div>
  </div>
</div>
<script>
  const form = document.getElementById('f');
  const btn = document.getElementById('deploy');
  const msg = document.getElementById('msg');
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    btn.textContent = 'Deploying...';
    msg.textContent = 'Deploying...';
    msg.className = '';
    const data = Object.fromEntries(new FormData(form));
    try {
      const res = await fetch('/api/setup/deploy', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || 'deploy failed');
      msg.className = 'ok';
      msg.textContent = 'Deployed. Cockpit is starting on port ' + json.port + '.';
      btn.textContent = 'Deployed';
    } catch (e) {
      msg.className = 'err';
      msg.textContent = 'Error: ' + e.message;
      btn.disabled = false;
      btn.textContent = 'Deploy Cockpit';
    }
  });
</script>
`

func runSetup(ctx context.Context, logger *log.Logger, store *db.Store) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(setupHTML))
	})

	var srv *http.Server
	done := make(chan error, 1)

	mux.HandleFunc("POST /api/setup/deploy", func(w http.ResponseWriter, r *http.Request) {
		var c setupConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if c.Port == "" {
			c.Port = "80"
		}
		if c.SessionHours == "" {
			c.SessionHours = "24"
		}
		if c.SLAHours == "" {
			c.SLAHours = "24"
		}

		now := time.Now().UTC()
		save := func(key, value string) {
			if err := store.SetSetting(ctx, key, value, "setup", now); err != nil {
				logger.Printf("setup setting %s: %v", key, err)
			}
		}
		save("setup_done", "1")
		save("setup_port", c.Port)
		save("setup_admin_user", c.AdminUser)
		save("setup_session_hours", c.SessionHours)
		save("setup_sla_hours", c.SLAHours)

		if c.CustomerNo != "" && c.CustomerName != "" {
			_, err := store.DB().ExecContext(ctx,
				`INSERT INTO customers (number, name, created_at) VALUES (?, ?, ?)`,
				c.CustomerNo, c.CustomerName, millis(now))
			if err != nil {
				http.Error(w, "could not create customer: "+err.Error(), http.StatusInternalServerError)
				return
			}
			logger.Printf("setup created customer %s %s", c.CustomerNo, c.CustomerName)
		}

		if c.AdminUser != "" && c.AdminPass != "" {
			hash, err := auth.HashPassword(c.AdminPass)
			if err != nil {
				http.Error(w, "could not hash password", http.StatusInternalServerError)
				return
			}
			if _, err := store.EnsureUser(ctx, c.AdminUser, hash, c.AdminUser, auth.RoleAdmin, now); err != nil {
				http.Error(w, "could not create admin user: "+err.Error(), http.StatusInternalServerError)
				return
			}
			logger.Printf("setup created admin user %q", c.AdminUser)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deployed", "port": c.Port})

		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = srv.Shutdown(context.Background())
			done <- nil
		}()
	})

	srv = &http.Server{Addr: ":492", Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
		done <- ctx.Err()
	}()

	go func() {
		logger.Printf("setup listening on :492")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			done <- err
		}
	}()

	return <-done
}

func millis(t time.Time) int64 { return t.UTC().UnixMilli() }
