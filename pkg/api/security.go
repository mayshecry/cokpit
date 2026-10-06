package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// securityHeaders wraps the whole app in browser-hardening response headers.
// CSP allows inline styles (the SPA builds DOM strings with style attributes
// and inline <style> usage) but constrains everything else to same-origin,
// plus the two CDN libraries already referenced by the shell.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self' https://cdnjs.cloudflare.com; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"font-src 'self' data:; " +
		"connect-src 'self'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), fullscreen=(self)")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		// Harmless on plain HTTP, essential once served behind TLS.
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// originGuard rejects cross-site state-changing requests. Bearer tokens are
// not ambient credentials, but this adds cheap CSRF-shaped defense in depth:
// any mutating request that carries an Origin header must match its Host.
func originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			origin := r.Header.Get("Origin")
			if origin != "" {
				host := origin
				if i := strings.Index(origin, "://"); i >= 0 {
					// strip scheme and trailing path
					rest := origin[i+3:]
					if j := strings.IndexByte(rest, '/'); j >= 0 {
						rest = rest[:j]
					}
					host = rest
				}
				if h, _, err := net.SplitHostPort(host); err == nil {
					// normalize "host:port" vs Host header
					requestHost := r.Host
					if rh, _, err2 := net.SplitHostPort(requestHost); err2 == nil {
						if !strings.EqualFold(h, rh) {
							w.WriteHeader(http.StatusForbidden)
							return
						}
					} else if !strings.EqualFold(host, requestHost) {
						w.WriteHeader(http.StatusForbidden)
						return
					}
				} else if !strings.EqualFold(host, r.Host) {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter is a small fixed-window, in-memory limiter keyed by an
// arbitrary string (usually client IP, optionally plus username). No
// external dependencies; a background-janitor-free GC runs inline.
type RateLimiter struct {
	mu      sync.Mutex
	hits    map[string]*rateBucket
	gcEvery int
	calls   int
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{hits: map[string]*rateBucket{}, gcEvery: 512}
}

// Allow returns true and records a hit when the key is under limit for the
// current window; otherwise false plus the seconds until the window resets.
func (rl *RateLimiter) Allow(key string, limit int, window time.Duration) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.calls++
	if rl.calls%rl.gcEvery == 0 {
		cutoff := time.Now().Add(-2 * window)
		for k, b := range rl.hits {
			if b.windowStart.Before(cutoff) {
				delete(rl.hits, k)
			}
		}
	}
	b, ok := rl.hits[key]
	if !ok || time.Since(b.windowStart) >= window {
		rl.hits[key] = &rateBucket{windowStart: time.Now(), count: 1}
		return true, 0
	}
	b.count++
	if b.count > limit {
		remaining := window - time.Since(b.windowStart)
		if remaining < time.Second {
			remaining = time.Second
		}
		return false, int(remaining.Seconds()) + 1
	}
	return true, 0
}

// Reset clears a key's recorded window (used after a successful login so
// legitimate users sharing an IP or past typos never hit the lockout).
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.hits, key)
}

// clientIP is the caller's best-guess IP, honoring a proxy header only when
// it contains a plausible address (e2b preview proxy sets X-Real-IP too).
// Proxy/TLS deployment knobs (single server per process; set from ServerConfig).
// trustProxy: honor X-Forwarded-* headers — set when behind nginx/Caddy/IIS.
// publicBaseURL: absolute external URL override (e.g. https://cockpit.acme.tld)
// — skips the trust requirement and wins over headers when set.
var (
	trustProxy    bool
	publicBaseURL string
)

func clientIP(r *http.Request) string {
	// Forwarded headers are trusted ONLY when COCKPIT_TRUST_PROXY=1 — a direct
	// client could otherwise spoof XFF to escape the login/pair rate limiters.
	if trustProxy {
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			return strings.TrimSpace(xr)
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// effectiveURL returns the external (scheme, host) this request is seen as:
// publicBaseURL override > trusted X-Forwarded-Proto/Host > TLS + Host.
func effectiveURL(r *http.Request) (string, string) {
	if publicBaseURL != "" {
		u := strings.TrimRight(publicBaseURL, "/")
		scheme, rest := "https", u
		if i := strings.Index(u, "://"); i >= 0 {
			scheme, rest = u[:i], u[i+3:]
		}
		return scheme, rest
	}
	host, scheme := r.Host, "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if trustProxy {
		if xf := r.Header.Get("X-Forwarded-Host"); xf != "" {
			host = xf
		}
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
	}
	return scheme, host
}

// checkPasswordPolicy centralizes the minimum password requirements for
// user creation and password changes. Deliberately short and plain — the
// error text is shown to admins directly.
func checkPasswordPolicy(username, password string) (code, msg string, ok bool) {
	if len(password) < 10 {
		return "weak_password", "password must be at least 10 characters", false
	}
	if strings.EqualFold(strings.TrimSpace(username), strings.TrimSpace(password)) {
		return "weak_password", "password must not equal the username", false
	}
	hasLetter, hasDigitOrSym := false, false
	for _, r := range password {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			hasLetter = true
		default:
			hasDigitOrSym = true
		}
	}
	if !hasLetter || !hasDigitOrSym {
		return "weak_password", "password needs letters and at least one digit or symbol", false
	}
	return "", "", true
}
