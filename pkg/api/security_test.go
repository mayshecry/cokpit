package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOriginGuardRequiresExactEffectiveOrigin(t *testing.T) {
	oldTrustProxy, oldPublicURL := trustProxy, publicBaseURL
	trustProxy, publicBaseURL = false, ""
	t.Cleanup(func() { trustProxy, publicBaseURL = oldTrustProxy, oldPublicURL })

	handler := originGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		name   string
		method string
		origin string
		want   int
	}{
		{name: "no origin", method: http.MethodPost, want: http.StatusNoContent},
		{name: "exact origin", method: http.MethodPost, origin: "http://example.com:8080", want: http.StatusNoContent},
		{name: "default port normalized", method: http.MethodPost, origin: "http://example.com", want: http.StatusNoContent},
		{name: "different port", method: http.MethodPost, origin: "http://example.com:9090", want: http.StatusForbidden},
		{name: "different scheme", method: http.MethodPost, origin: "https://example.com:8080", want: http.StatusForbidden},
		{name: "different host", method: http.MethodPost, origin: "http://attacker.example:8080", want: http.StatusForbidden},
		{name: "null origin", method: http.MethodPost, origin: "null", want: http.StatusForbidden},
		{name: "read not blocked", method: http.MethodGet, origin: "https://attacker.example", want: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "http://example.com:8080/api/v1/test"
			if tt.name == "default port normalized" {
				target = "http://example.com/api/v1/test"
			}
			r := httptest.NewRequest(tt.method, target, nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestSessionCookieSecureForPublicHTTPS(t *testing.T) {
	oldTrustProxy, oldPublicURL := trustProxy, publicBaseURL
	trustProxy, publicBaseURL = false, "https://cockpit.example.com"
	t.Cleanup(func() { trustProxy, publicBaseURL = oldTrustProxy, oldPublicURL })

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	s := &Server{now: func() time.Time { return now }}
	r := httptest.NewRequest(http.MethodPost, "http://internal:8080/api/v1/auth/login", nil)
	w := httptest.NewRecorder()
	s.setSessionCookie(w, r, "token", now.Add(time.Hour))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie = %+v", cookies)
	}
}
