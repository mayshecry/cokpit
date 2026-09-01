package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
	"cockpit/pkg/order"
)

func fixedClock() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }

var testAuthToken string

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "api-test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	st := db.New(conn)
	hash, err := auth.HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := st.CreateUser(ctx, "admin", hash, "Admin", auth.RoleAdmin, fixedClock()); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	svr := New(st, ServerConfig{Now: fixedClock})
	ts := httptest.NewServer(svr.Handler())
	t.Cleanup(ts.Close)

	code, body := doAs(t, "POST", ts.URL+"/api/v1/auth/login",
		`{"username":"admin","password":"secret123"}`, "")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d %v", code, body)
	}
	testAuthToken = body["token"].(string)
	return ts
}

func do(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	return doAs(t, method, url, body, testAuthToken)
}

func doAs(t *testing.T, method, url, body, token string) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("unmarshal %q: %v", raw, err)
		}
	}
	return resp.StatusCode, payload
}

func mustCreate(t *testing.T, ts *httptest.Server, number string) int64 {
	t.Helper()
	code, body := do(t, "POST", ts.URL+"/api/v1/orders",
		fmt.Sprintf(`{"orderNumber":%q}`, number))
	if code != http.StatusCreated {
		t.Fatalf("create %s: status %d body %v", number, code, body)
	}
	o := body["order"].(map[string]any)
	return int64(o["id"].(float64))
}

func TestPostCreateOrder(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, "POST", ts.URL+"/api/v1/orders", `{"orderNumber":"ORD-1"}`)
	if code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %v", code, body)
	}
	o := body["order"].(map[string]any)
	if o["status"] != string(order.StatusReceived) {
		t.Errorf("status = %v, want Received", o["status"])
	}

	target, err := time.Parse(time.RFC3339, o["targetCompletionAt"].(string))
	if err != nil {
		t.Fatalf("target parse: %v", err)
	}
	if d := target.Sub(fixedClock()); d != 24*time.Hour {
		t.Errorf("target delta = %v, want 24h", d)
	}
}

func TestPostCreateOrderValidation(t *testing.T) {
	ts := newTestServer(t)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing number", `{}`, http.StatusBadRequest},
		{"bad json", `{nope`, http.StatusBadRequest},
		{"past target", `{"orderNumber":"X","targetCompletionAt":"2020-01-01T00:00:00Z"}`, http.StatusBadRequest},
		{"duplicate", `{"orderNumber":"ORD-1"}`, http.StatusConflict},
	}
	mustCreate(t, ts, "ORD-1")
	for _, c := range cases {
		code, _ := do(t, "POST", ts.URL+"/api/v1/orders", c.body)
		if code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, code, c.want)
		}
	}
}

func TestListOrdersFilter(t *testing.T) {
	ts := newTestServer(t)
	_ = mustCreate(t, ts, "ORD-L1")
	code, body := do(t, "GET", ts.URL+"/api/v1/orders?status=Received", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	orders := body["orders"].([]any)
	if len(orders) != 1 {
		t.Errorf("Received filter = %d orders, want 1", len(orders))
	}

	code, _ = do(t, "GET", ts.URL+"/api/v1/orders?status=Bogus", "")
	if code != http.StatusBadRequest {
		t.Errorf("bad filter status = %d, want 400", code)
	}
}

func TestGetOrderDetail(t *testing.T) {
	ts := newTestServer(t)
	id := mustCreate(t, ts, "ORD-D1")
	code, body := do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d", ts.URL, id), "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	o := body["order"].(map[string]any)
	if o["onHold"].(bool) != false {
		t.Error("new order must not be on hold")
	}
	sla := o["sla"].(map[string]any)
	if sla["status"] != string(order.SLAOnTime) {
		t.Errorf("sla = %v, want ON_TIME", sla["status"])
	}
	if holds := o["activeHolds"].([]any); len(holds) != 0 {
		t.Errorf("activeHolds = %v, want none", holds)
	}
}
