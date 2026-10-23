package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"cockpit/pkg/auth"
)

func loginAs(t *testing.T, ts string, username, password string) string {
	t.Helper()
	code, body := doAs(t, "POST", ts+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), "")
	if code != http.StatusOK {
		t.Fatalf("login %s: %d %v", username, code, body)
	}
	return body["token"].(string)
}

func TestLoginFlow(t *testing.T) {
	ts := newTestServer(t)
	tk := loginAs(t, ts.URL, "admin", "secret123")
	if tk == "" {
		t.Fatal("expected a token")
	}
	code, body := doAs(t, "GET", ts.URL+"/api/v1/auth/me", "", tk)
	if code != http.StatusOK {
		t.Fatalf("me: %d", code)
	}
	user := body["user"].(map[string]any)
	if user["username"] != "admin" || user["role"] != string(auth.RoleAdmin) {
		t.Errorf("me user = %v", user)
	}
	if _, has := user["passwordHash"]; has {
		t.Error("password hash must not be exposed")
	}

	code, _ = doAs(t, "POST", ts.URL+"/api/v1/auth/login", `{"username":"admin","password":"wrong"}`, "")
	if code != http.StatusUnauthorized {
		t.Errorf("bad password = %d, want 401", code)
	}

	code, _ = doAs(t, "GET", ts.URL+"/api/v1/orders", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", code)
	}

	code, body = doAs(t, "POST", ts.URL+"/api/v1/auth/logout", "", tk)
	if code != http.StatusOK {
		t.Fatalf("logout: %d %v", code, body)
	}
	code, _ = doAs(t, "GET", ts.URL+"/api/v1/auth/me", "", tk)
	if code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", code)
	}
}

func TestBrowserCookieSession(t *testing.T) {
	ts, _ := newActivityServer(t)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret123"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var login map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("login Cache-Control = %q", got)
	}
	var session *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName {
			session = cookie
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" {
		t.Fatalf("session cookie = %+v", session)
	}
	if session.Secure {
		t.Fatal("plain-HTTP test session must not be marked Secure")
	}
	token := login["token"].(string)

	code, _ := doAs(t, http.MethodGet, ts.URL+"/api/v1/events?token="+token, "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("SSE accepted a URL token: %d", code)
	}

	sseReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/events", nil)
	sseReq.AddCookie(session)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("cookie SSE: %v", err)
	}
	if sseResp.StatusCode != http.StatusOK || !strings.HasPrefix(sseResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("cookie SSE = %d %q", sseResp.StatusCode, sseResp.Header.Get("Content-Type"))
	}
	sseResp.Body.Close()

	meReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
	meReq.AddCookie(session)
	meResp, err := http.DefaultClient.Do(meReq)
	if err != nil {
		t.Fatal(err)
	}
	meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("cookie-only /auth/me = %d", meResp.StatusCode)
	}

	logoutReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	logoutReq.AddCookie(session)
	logoutResp, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("cookie logout = %d", logoutResp.StatusCode)
	}
	cleared := false
	for _, cookie := range logoutResp.Cookies() {
		if cookie.Name == sessionCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not expire the session cookie")
	}
	meReq, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
	meReq.AddCookie(session)
	meResp, err = http.DefaultClient.Do(meReq)
	if err != nil {
		t.Fatal(err)
	}
	meResp.Body.Close()
	if meResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked cookie session = %d", meResp.StatusCode)
	}
}

func TestLegacyBearerUpgradesToCookie(t *testing.T) {
	ts, _ := newActivityServer(t)
	token := loginAs(t, ts.URL, "admin", "secret123")
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy /auth/me = %d", resp.StatusCode)
	}
	found := false
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value == token && cookie.HttpOnly {
			found = true
		}
	}
	if !found {
		t.Fatal("legacy Bearer session was not upgraded to an HttpOnly cookie")
	}
}

func TestRolePermissions(t *testing.T) {
	ts := newTestServer(t)

	code, body := doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"alice","password":"password123","displayName":"Alice","role":"viewer"}`, testAuthToken)
	if code != http.StatusCreated {
		t.Fatalf("create viewer: %d %v", code, body)
	}
	code, body = doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"bob","password":"password123","displayName":"Bob","role":"operator"}`, testAuthToken)
	if code != http.StatusCreated {
		t.Fatalf("create operator: %d %v", code, body)
	}

	viewerTk := loginAs(t, ts.URL, "alice", "password123")
	opTk := loginAs(t, ts.URL, "bob", "password123")

	code, _ = doAs(t, "GET", ts.URL+"/api/v1/orders", "", viewerTk)
	if code != http.StatusOK {
		t.Errorf("viewer list = %d, want 200", code)
	}
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/orders", `{"orderNumber":"ORD-P1"}`, viewerTk)
	if code != http.StatusForbidden {
		t.Errorf("viewer create = %d, want 403", code)
	}
	code, _ = doAs(t, "GET", ts.URL+"/api/v1/users", "", viewerTk)
	if code != http.StatusForbidden {
		t.Errorf("viewer users = %d, want 403", code)
	}

	code, _ = doAs(t, "POST", ts.URL+"/api/v1/orders", `{"orderNumber":"ORD-P2"}`, opTk)
	if code != http.StatusCreated {
		t.Errorf("operator create = %d, want 201", code)
	}
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"carol","password":"password123","role":"viewer"}`, opTk)
	if code != http.StatusForbidden {
		t.Errorf("operator users = %d, want 403", code)
	}
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/orders/99999/qc",
		`{"status":"PASS"}`, opTk)
	if code != http.StatusForbidden {
		t.Errorf("operator qc = %d, want 403", code)
	}
}

func TestUserManagement(t *testing.T) {
	ts := newTestServer(t)

	code, body := doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"carol","password":"password123","displayName":"Carol","role":"qc"}`, testAuthToken)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, body)
	}
	id := int64(body["user"].(map[string]any)["id"].(float64))

	code, _ = doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"carol","password":"password123","role":"viewer"}`, testAuthToken)
	if code != http.StatusConflict {
		t.Errorf("duplicate user = %d, want 409", code)
	}
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"x","password":"short","role":"viewer"}`, testAuthToken)
	if code != http.StatusBadRequest {
		t.Errorf("short password = %d, want 400", code)
	}
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"y","password":"password123","role":"superuser"}`, testAuthToken)
	if code != http.StatusBadRequest {
		t.Errorf("bad role = %d, want 400", code)
	}

	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/users/%d/role", ts.URL, id),
		`{"role":"operator"}`, testAuthToken)
	if code != http.StatusOK || body["user"].(map[string]any)["role"] != string(auth.RoleOperator) {
		t.Errorf("set role = %d %v", code, body)
	}

	oldToken := loginAs(t, ts.URL, "carol", "password123")
	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/users/%d/password", ts.URL, id),
		`{"password":"aaaaaaaaaaaa"}`, testAuthToken)
	if code != http.StatusBadRequest {
		t.Errorf("weak reset password = %d, want 400", code)
	}
	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/users/%d/password", ts.URL, id),
		`{"password":"newpassword123"}`, testAuthToken)
	if code != http.StatusOK {
		t.Errorf("reset password = %d", code)
	}
	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/auth/me", "", oldToken); code != http.StatusUnauthorized {
		t.Errorf("old session survived password reset: %d", code)
	}
	tk := loginAs(t, ts.URL, "carol", "newpassword123")
	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/auth/me", "", tk); code != http.StatusOK {
		t.Errorf("login with new password failed")
	}

	code, _ = doAs(t, "DELETE", fmt.Sprintf("%s/api/v1/users/%d", ts.URL, id), "", testAuthToken)
	if code != http.StatusOK {
		t.Errorf("delete user = %d", code)
	}
	code, _ = doAs(t, "GET", ts.URL+"/api/v1/users", "", testAuthToken)
	if code != http.StatusOK {
		t.Fatalf("list users: %d", code)
	}
}
