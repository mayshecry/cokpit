package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/skip2/go-qrcode"
	"io"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client-only guard: every non-API request must present a session cookie
// minted from a paired client key. The client proves itself with a short
// HMAC over (timestamp | machine) using a key it received at pairing time,
// sent once as query params on its first navigation; the server validates
// and issues an HttpOnly session cookie for the rest of the session.
//
// A pairing key is machine-bound. Spoofing therefore requires BOTH the
// paired key file AND knowledge of the machine profile — "hard", not
// impossible (any binary-held secret can be extracted in principle; for
// full spoof-immunity bind the UI listener to loopback and let only the
// client process reach it).
//
// /api/v1/**, /healthz, /scan.html and /pair-guide.* stay open: the API
// keeps its own bearer-token protection and the pairing page must load
// before a session exists.

const (
	clientCookieName = "ckdpair"
	proofWindowSec   = 300
)

type clientKeyEntry struct {
	Key       string    `json:"key"`
	Machine   string    `json:"machine"`
	Name      string    `json:"name"`
	Username  string    `json:"username,omitempty"` // account the device is bound to
	CreatedAt time.Time `json:"createdAt"`
}

// Pairing CODES are not here: they live in the database (pair_codes table),
// many active at once, each single-use and optionally bound to an account.
type clientKeyStore struct {
	Keys map[string]clientKeyEntry `json:"keys"`
}

type ClientGuard struct {
	enabled bool
	file    string
	mu      sync.Mutex
	store   clientKeyStore
	sess    map[string]clientSession
	pairLim *RateLimiter
	logger  interface{ Printf(string, ...any) }
}

type clientSession struct {
	clientID string
	machine  string
	expires  time.Time
}

func NewClientGuard(enabled bool, keysFile string, logger interface{ Printf(string, ...any) }) *ClientGuard {
	return &ClientGuard{
		enabled: enabled,
		file:    keysFile,
		sess:    map[string]clientSession{},
		pairLim: NewRateLimiter(),
		logger:  logger,
	}
}

func (g *ClientGuard) Enabled() bool { return g.enabled }

// Init loads (or creates) the key store. On first creation a fresh pairing
// code is generated and printed to the server log once.
func (g *ClientGuard) Init() error {
	if !g.enabled {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if raw, err := os.ReadFile(g.file); err == nil {
		var st clientKeyStore
		if json.Unmarshal(raw, &st) == nil && st.Keys != nil {
			g.store = st
			return nil
		}
	}
	g.store = clientKeyStore{Keys: map[string]clientKeyEntry{}}
	return g.saveLocked()
}

// logPairingStateLocked surfaces whatever the operator needs right now:
// the active pairing code (every boot, not just the first) — or, when no
// code is active, how to issue one.
// Call with g.mu held.
func (g *ClientGuard) saveLocked() error {
	raw, err := json.MarshalIndent(g.store, "", "  ")
	if err != nil {
		return err
	}
	tmp := g.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, g.file)
}

// randClientCode returns a one-time pairing code as 8 digits grouped 4-4
// ("1234-5678"). Digits only: easy to read off a console, no letter ambiguity.
// crypto/rand per digit — no modulo bias, no byte reuse.
func randClientCode() string {
	d := make([]byte, 0, 9)
	for i := 0; i < 8; i++ {
		if i == 4 {
			d = append(d, '-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			// crypto/rand broken; fall back to time-seeded digits (still local-only)
			n = big.NewInt(time.Now().UnixNano() % 10)
		}
		d = append(d, byte('0'+n.Int64()))
	}
	return string(d)
}

// normCode strips everything that isn't a letter/digit and uppercases, so
// "1234-5678", "1234 5678" and "12345678" all match — and legacy alphanumeric
// codes in existing key stores keep working.
func normCode(s string) string {
	s = strings.ToUpper(s)
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') {
			out = append(out, c)
		}
	}
	return string(out)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Paths the guard never blocks.
func openPath(p string) bool {
	if p == "/healthz" || p == "/scan.html" || p == "/pair-guide.html" || p == "/js/pairguide.js" ||
		p == "/launch.html" || p == "/js/launch.js" ||
		p == "/qr-approve.html" || p == "/js/qrapprove.js" {
		return true
	}
	for _, pre := range []string{"/api/", "/js/pairguide.js"} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// Wrap enforces the guard around the whole mux when enabled.
func (g *ClientGuard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.enabled || openPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// 1) already-sessioned client?
		if c, err := r.Cookie(clientCookieName); err == nil {
			if g.sessionValid(c.Value) {
				next.ServeHTTP(w, r)
				return
			}
		}
		// 2) fresh proof on the navigation URL?
		if r.Method == http.MethodGet && g.validateProof(r) {
			tok := randHex(16)
			g.mu.Lock()
			g.sess[tok] = clientSession{
				clientID: r.URL.Query().Get("ckid"),
				machine:  r.URL.Query().Get("mach"),
				expires:  time.Now().Add(12 * time.Hour),
			}
			cutoff := time.Now().Add(-12 * time.Hour)
			for k, s := range g.sess {
				if s.expires.Before(cutoff) {
					delete(g.sess, k)
				}
			}
			g.mu.Unlock()
			scheme, _ := effectiveURL(r)
			http.SetCookie(w, &http.Cookie{
				Name:     clientCookieName,
				Value:    tok,
				Path:     "/",
				HttpOnly: true,
				Secure:   scheme == "https",
				// Lax (not Strict): the page a phone opens by scanning a QR is a
				// top-level navigation from another app — Strict would drop the
				// cookie and land the user on the block page. All mutations run
				// through same-origin POSTs + originGuard, so Lax stays safe.
				SameSite: http.SameSiteLaxMode,
			})
			target := r.URL.Path
			if q := r.URL.Query(); len(q) > 0 {
				q.Del("ckid")
				q.Del("ts")
				q.Del("mach")
				q.Del("proof")
				if enc := q.Encode(); enc != "" {
					target += "?" + enc
				}
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		g.writeBlocked(w)
	})
}

func (g *ClientGuard) sessionValid(token string) bool {
	if token == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.sess[token]
	if !ok || s.expires.Before(time.Now()) {
		return false
	}
	return true
}

// validateProof checks ckid/ts/mach against the stored key's HMAC.
func (g *ClientGuard) validateProof(r *http.Request) bool {
	return g.checkProof(
		r.URL.Query().Get("ckid"),
		r.URL.Query().Get("ts"),
		r.URL.Query().Get("mach"),
		r.URL.Query().Get("proof"),
	)
}

// checkProof validates the pairing signature of an already-paired client.
// proof must be hex(HMAC-SHA256(clientKey, "ts|mach")) with ts within ±300s.
func (g *ClientGuard) checkProof(ckid, ts, mach, proof string) bool {
	if ckid == "" || ts == "" || proof == "" || mach == "" {
		return false
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	if t < now-proofWindowSec || t > now+proofWindowSec {
		return false
	}
	g.mu.Lock()
	entry, ok := g.store.Keys[ckid]
	g.mu.Unlock()
	if !ok || entry.Key == "" || entry.Machine == "" || entry.Machine != mach {
		return false
	}
	key, err := hex.DecodeString(entry.Key)
	if err != nil || len(key) == 0 {
		return false
	}
	want := hmacPayload(key, ts, mach)
	got, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(proof)))
	if err != nil || len(got) != sha256.Size {
		return false
	}
	return hmac.Equal(got, want)
}

func hmacPayload(key []byte, ts, mach string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(ts + "|" + mach))
	return m.Sum(nil)
}

func (g *ClientGuard) writeBlocked(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	ioWrite(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Restricted</title>
<style>body{font-family:system-ui,Segoe UI,sans-serif;background:#101216;color:#d7dce3;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{max-width:420px;padding:34px 38px;border:1px solid #232830;border-radius:16px;background:#16191e;text-align:center}
h1{font-size:19px;margin:0 0 10px}p{font-size:13.5px;line-height:1.55;color:#98a0aa;margin:0}small{display:block;margin-top:16px;color:#676e78;font-size:11px;letter-spacing:.08em;text-transform:uppercase}</style>
</head><body><div class="card"><h1>Restricted client</h1>
<p>This console is paired to a specific client. Use the desktop client, or — on a phone — ask an admin to open <em>Users → Pair a device</em> and scan the QR code, then log in with your cockpit account.</p>
<small>cockpit · client-only mode</small></div></body></html>`)
}

func ioWrite(w http.ResponseWriter, s string) { fmt.Fprint(w, s) }

// ---- API endpoints -------------------------------------------------------

// handleClientPair exchanges a one-time pairing code for a client key.
func (s *Server) handleClientPair(w http.ResponseWriter, r *http.Request) {
	g := s.clientGuard
	if g == nil || !g.enabled {
		s.writeError(w, http.StatusNotFound, "client_only_disabled", "client pairing is not enabled on this server")
		return
	}
	ip := clientIP(r)
	if ok, retry := g.pairLim.Allow(ip+"|pair", 10, 10*time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "too many pairing attempts — try again in "+strconv.Itoa(retry)+"s")
		return
	}
	var req struct {
		Code    string `json:"code"`
		Machine string `json:"machine"`
		Name    string `json:"name"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	req.Machine = cleanString(req.Machine)
	req.Code = normCode(cleanString(req.Code))
	if req.Machine == "" || len(req.Code) < 6 || len(req.Code) > 16 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "code and machine are required")
		return
	}
	pc, cerr := s.store.ConsumePairCode(r.Context(), req.Code, s.now().UTC())
	if cerr != nil {
		s.writeError(w, http.StatusForbidden, "pair_failed", "invalid, consumed or expired pairing code")
		return
	}
	boundUser := pc.Username
	g.mu.Lock()
	defer g.mu.Unlock()
	clientID := "cl_" + randHex(8)
	key := randHex(32)
	name := cleanString(req.Name)
	if name == "" {
		name = "client"
	}
	g.store.Keys[clientID] = clientKeyEntry{Key: key, Machine: req.Machine, Name: name, Username: boundUser, CreatedAt: s.now().UTC()}
	if err := g.saveLocked(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	// Pairing just proven (one-time code) — issue the device session cookie in
	// the SAME round trip. Phones on plain-http origins can't sign an HMAC in
	// JS (no WebCrypto), so the pair itself must deliver the session.
	tok := randHex(16)
	g.sess[tok] = clientSession{clientID: clientID, machine: req.Machine, expires: time.Now().Add(12 * time.Hour)} // g.mu already held
	scheme, _ := effectiveURL(r)
	http.SetCookie(w, &http.Cookie{
		Name:     clientCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   scheme == "https",
		SameSite: http.SameSiteLaxMode,
	})
	resp := map[string]any{"clientId": clientID, "key": key, "machine": req.Machine, "name": name}
	// Account-linked code → the paired device ALSO receives that account's login
	// session: scan once, type the code once, and you're paired and signed in.
	if boundUser != "" {
		if user, err2 := s.store.UserByUsername(r.Context(), boundUser); err2 == nil {
			if token, terr := auth.NewToken(); terr == nil {
				now := s.now().UTC()
				if err3 := s.store.IssueToken(r.Context(), user.ID, token, now, now.Add(s.sessionHours)); err3 == nil {
					user.PasswordHash = ""
					user = s.userWithDeptPerms(r.Context(), user)
					resp["token"] = token
					resp["user"] = user
				}
			}
		}
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleClientLaunch exchanges a valid pairing proof (JSON body) for the
// ckdpair session cookie — the phone flow, where JS can fetch() to set the
// cookie but cannot sign navigation URLs like the desktop client.
func (s *Server) handleClientLaunch(w http.ResponseWriter, r *http.Request) {
	g := s.clientGuard
	if g == nil || !g.enabled {
		s.writeError(w, http.StatusNotFound, "client_only_disabled", "client pairing is not enabled on this server")
		return
	}
	ip := clientIP(r)
	if ok, retry := g.pairLim.Allow(ip+"|launch", 10, 10*time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts — try again in "+strconv.Itoa(retry)+"s")
		return
	}
	var req struct {
		ClientID string `json:"ckid"`
		TS       string `json:"ts"`
		Machine  string `json:"mach"`
		Proof    string `json:"proof"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if !g.checkProof(cleanString(req.ClientID), cleanString(req.TS), cleanString(req.Machine), cleanString(req.Proof)) {
		s.writeError(w, http.StatusForbidden, "launch_failed", "invalid pairing proof")
		return
	}
	tok := randHex(16)
	g.mu.Lock()
	g.sess[tok] = clientSession{clientID: req.ClientID, machine: req.Machine, expires: time.Now().Add(12 * time.Hour)}
	g.mu.Unlock()
	scheme, _ := effectiveURL(r)
	http.SetCookie(w, &http.Cookie{
		Name:     clientCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   scheme == "https",
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}


// ensurePairCode returns an active (unexpired, unused) pairing code for the
// given account binding ("" = device-only), issuing a fresh one when none
// exists. Codes are rows in the database: many can be active at once, each
// single-use — re-rendering a QR keeps showing a valid code.
func (s *Server) ensurePairCode(r *http.Request, username string) string {
	now := s.now().UTC()
	if pc, err := s.store.ActivePairCode(r.Context(), username, now); err == nil {
		return pc.Code
	}
	pc := db.PairCode{
		Code:      randClientCode(),
		Username:  username,
		CreatedBy: s.currentUser(r).Username,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
	}
	if err := s.store.IssuePairCode(r.Context(), pc); err != nil {
		if s.logger != nil {
			s.logger.Printf("[cockpit] failed to issue pairing code: %v", err)
		}
		return ""
	}
	return pc.Code
}

// EnsureBootPairCode prints a rescue pairing code on EVERY boot (so a locked-
// out admin can always pair a device), importing any legacy keystore code
// first. Codes are database rows — they survive restarts.
func (s *Server) EnsureBootPairCode(ctx context.Context) {
	g := s.clientGuard
	if g == nil || !g.enabled {
		return
	}
	now := s.now().UTC()
	if raw, err := os.ReadFile(g.file); err == nil {
		var legacy struct{ PairCode, PairUser string }
		if json.Unmarshal(raw, &legacy) == nil && legacy.PairCode != "" {
			if s.store.IssuePairCode(ctx, db.PairCode{
				Code: normCode(legacy.PairCode), Username: legacy.PairUser, CreatedBy: "legacy-keystore",
				CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
			}) == nil && s.logger != nil {
				s.logger.Printf("[cockpit] imported legacy pairing code from keystore into database (valid 24h): %s", legacy.PairCode)
			}
		}
	}
	code := ""
	if pc, err := s.store.AnyActivePairCode(ctx, now); err == nil {
		code = pc.Code
	} else {
		pc := db.PairCode{Code: randClientCode(), CreatedBy: "boot", CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)}
		if err := s.store.IssuePairCode(ctx, pc); err != nil {
			if s.logger != nil {
				s.logger.Printf("[cockpit] boot pairing code issue failed: %v", err)
			}
		} else {
			code = pc.Code
		}
	}
	if code != "" && s.logger != nil {
		s.logger.Printf("[cockpit] client-only mode — PAIRING CODE: %s (one-time; enter it in the client launcher)", code)
	}
}

// handleAdminPairQR returns the current pairing code plus a QR (PNG data-url)
// of the phone launch URL. Scanning it opens /launch.html?code=… which
// auto-pairs the phone and lands the user on the normal login form.
// If no code is active, one is issued (and surfaced in the log).
func (s *Server) handleAdminPairQR(w http.ResponseWriter, r *http.Request) {
	g := s.clientGuard
	if g == nil || !g.enabled {
		s.writeError(w, http.StatusNotFound, "client_only_disabled", "client pairing is not enabled on this server")
		return
	}
	boundQ := cleanString(r.URL.Query().Get("username"))
	if boundQ != "" {
		if _, uerr := s.store.UserByUsername(r.Context(), boundQ); uerr != nil {
			s.writeError(w, http.StatusNotFound, "no_user", "unknown username: "+boundQ)
			return
		}
	}
	code := s.ensurePairCode(r, boundQ)
	if code == "" {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	boundUser := boundQ

	scheme, host := effectiveURL(r)
	url := scheme + "://" + host + "/launch.html?code=" + code

	png, err := qrcode.Encode(url, qrcode.Highest, 320)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "QR generation failed: "+err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{
		"code":     code,
		"url":      url,
		"username": boundUser,
		"dataUrl":  "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	})
}

// handleAdminPairCode generates a fresh one-time pairing code (admin only)
// and returns it — for pairing an additional client after the first.
func (s *Server) handleAdminPairCode(w http.ResponseWriter, r *http.Request) {
	g := s.clientGuard
	if g == nil || !g.enabled {
		s.writeError(w, http.StatusNotFound, "client_only_disabled", "client pairing is not enabled on this server")
		return
	}
	// Optional body {"username":"x"} → account-linked code: the device that pairs
	// with it ALSO receives that account's login session.
	boundUser := ""
	var req struct {
		Username string `json:"username"`
	}
	raw, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if rerr == nil && len(raw) > 0 {
		if json.Unmarshal(raw, &req) != nil {
			s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
		req.Username = cleanString(req.Username)
		if req.Username != "" {
			if _, uerr := s.store.UserByUsername(r.Context(), req.Username); uerr != nil {
				s.writeError(w, http.StatusNotFound, "no_user", "unknown username: "+req.Username)
				return
			}
			boundUser = req.Username
		}
	}
	code := s.ensurePairCode(r, boundUser)
	if code == "" {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if s.logger != nil {
		forUser := ""
		if boundUser != "" {
			forUser = " (account: " + boundUser + ")"
		}
		s.logger.Printf("[cockpit] new pairing code issued by %s%s: %s", s.currentUser(r).Username, forUser, code)
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"pairCode": code, "username": boundUser})
}

// handleListPairedClients lists paired client identities (admin only).
func (s *Server) handleListPairedClients(w http.ResponseWriter, r *http.Request) {
	g := s.clientGuard
	if g == nil || !g.enabled {
		s.writeError(w, http.StatusNotFound, "client_only_disabled", "client pairing is not enabled on this server")
		return
	}
	g.mu.Lock()
	out := []map[string]any{}
	for id, e := range g.store.Keys {
		m := e.Machine
		if len(m) > 12 {
			m = m[:12] + "…" // fingerprint preview only — never expose the full machine id
		}
		row := map[string]any{"clientId": id, "name": e.Name, "createdAt": e.CreatedAt, "machine": m}
		if e.Username != "" {
			row["username"] = e.Username
		}
		out = append(out, row)
	}
	g.mu.Unlock()
	s.writeJSON(w, http.StatusOK, map[string]any{"clients": out})
}

// handleRevokePairedClient removes a paired client (admin only).
func (s *Server) handleRevokePairedClient(w http.ResponseWriter, r *http.Request) {
	g := s.clientGuard
	if g == nil || !g.enabled {
		s.writeError(w, http.StatusNotFound, "client_only_disabled", "client pairing is not enabled on this server")
		return
	}
	id := cleanString(r.PathValue("id"))
	g.mu.Lock()
	if _, ok := g.store.Keys[id]; !ok {
		g.mu.Unlock()
		s.writeError(w, http.StatusNotFound, "not_found", "no client with that id")
		return
	}
	delete(g.store.Keys, id)
	for tok, ss := range g.sess {
		if ss.clientID == id {
			delete(g.sess, tok)
		}
	}
	err := g.saveLocked()
	g.mu.Unlock()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
