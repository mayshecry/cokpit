package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self' https://cdnjs.cloudflare.com; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"font-src 'self' data:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"worker-src 'self'; " +
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
		if strings.HasPrefix(r.URL.Path, "/api/") {

			h.Set("Cache-Control", "no-store")
			h.Set("Pragma", "no-cache")
		}

		if scheme, _ := effectiveURL(r); scheme == "https" {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if origin := r.Header.Get("Origin"); origin != "" && !sameEffectiveOrigin(origin, r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameEffectiveOrigin(origin string, r *http.Request) bool {
	actual, err := url.Parse(origin)
	if err != nil || actual.Scheme == "" || actual.Host == "" || actual.User != nil ||
		actual.RawQuery != "" || actual.Fragment != "" || (actual.Path != "" && actual.Path != "/") {
		return false
	}
	scheme, host := effectiveURL(r)
	expected, err := url.Parse(scheme + "://" + host)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	return strings.EqualFold(actual.Scheme, expected.Scheme) && originHostKey(actual) == originHostKey(expected)
}

func originHostKey(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return strings.ToLower(u.Hostname()) + "|" + port
}

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

func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.hits, key)
}

var (
	trustProxy    bool
	publicBaseURL string
)

func clientIP(r *http.Request) string {

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

func checkPasswordPolicy(username, password string) (code, msg string, ok bool) {
	if len(password) < 10 {
		return "weak_password", "password must be at least 10 characters", false
	}
	if len(password) > 256 {
		return "weak_password", "password must be at most 256 characters", false
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
