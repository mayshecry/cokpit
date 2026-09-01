package api

import (
	"fmt"
	"net/http"
	"testing"

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

	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/users/%d/password", ts.URL, id),
		`{"password":"newpassword123"}`, testAuthToken)
	if code != http.StatusOK {
		t.Errorf("reset password = %d", code)
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
