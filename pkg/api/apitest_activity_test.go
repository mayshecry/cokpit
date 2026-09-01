package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
	"cockpit/pkg/order"
)

func newActivityServer(t *testing.T) (*httptest.Server, *db.Store) {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "activity-test.db"))
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
		t.Fatalf("seed admin: %v", err)
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
	return ts, st
}

func seedOperator(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	code, body := do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"op","password":"password123","displayName":"Olive","role":"operator"}`)
	if code != http.StatusCreated {
		t.Fatalf("create operator: %d %v", code, body)
	}
	return loginAs(t, ts.URL, "op", "password123")
}

func TestAttentionProjection(t *testing.T) {
	ts, st := newActivityServer(t)
	ctx := context.Background()

	if _, err := st.CreateOrder(ctx, "ORD-BREACH", fixedClock().Add(-3*time.Hour), fixedClock().Add(-27*time.Hour), "admin"); err != nil {
		t.Fatalf("seed breach: %v", err)
	}
	_ = mustCreateWithTarget(t, ts, "ORD-WARN", fixedClock().Add(2*time.Hour))
	freshID := mustCreate(t, ts, "ORD-FRESH")
	doneID := mustCreate(t, ts, "ORD-DONE")
	if _, err := st.TransitionOrder(ctx, doneID, "Processing", "admin", fixedClock()); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, err := st.TransitionOrder(ctx, doneID, "QC_Review", "admin", fixedClock()); err != nil {
		t.Fatalf("transition qc: %v", err)
	}
	if _, err := st.SubmitQC(ctx, doneID, order.QCPass, "admin", "", "admin", fixedClock()); err != nil {
		t.Fatalf("qc pass: %v", err)
	}
	if _, err := st.TransitionOrder(ctx, doneID, "Completed", "admin", fixedClock()); err != nil {
		t.Fatalf("complete: %v", err)
	}

	code, body := do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/holds", ts.URL, freshID),
		`{"reason":"waiting for parts"}`)
	if code != http.StatusCreated {
		t.Fatalf("hold: %d %v", code, body)
	}

	code, body = do(t, "GET", ts.URL+"/api/v1/attention", "")
	if code != http.StatusOK {
		t.Fatalf("attention: %d %v", code, body)
	}
	p := body["attention"].(map[string]any)
	if p["slaBreached"].(float64) != 1 || p["slaWarning"].(float64) != 1 ||
		p["onHold"].(float64) != 1 || p["awaitingQc"].(float64) != 0 {
		t.Fatalf("counters = %v", p)
	}

	cards := p["cards"].([]any)
	if len(cards) != 3 {
		t.Fatalf("cards = %v", cards)
	}
	byNum := map[string]map[string]any{}
	for _, c := range cards {
		m := c.(map[string]any)
		byNum[m["orderNumber"].(string)] = m
	}
	if byNum["ORD-BREACH"]["reason"] != "SLA_BREACHED" || byNum["ORD-BREACH"]["severity"] != "high" {
		t.Errorf("breach card = %v", byNum["ORD-BREACH"])
	}
	if byNum["ORD-WARN"]["reason"] != "SLA_WARNING" {
		t.Errorf("warn card = %v", byNum["ORD-WARN"])
	}
	if byNum["ORD-FRESH"]["reason"] != "ON_HOLD" || byNum["ORD-FRESH"]["holdCount"].(float64) != 1 {
		t.Errorf("held card = %v", byNum["ORD-FRESH"])
	}
	if _, ok := byNum["ORD-DONE"]; ok {
		t.Error("completed order must not be in attention")
	}
}

func mustCreateWithTarget(t *testing.T, ts *httptest.Server, number string, target time.Time) int64 {
	t.Helper()
	code, body := do(t, "POST", ts.URL+"/api/v1/orders",
		fmt.Sprintf(`{"orderNumber":%q,"targetCompletionAt":%q}`, number, target.UTC().Format(time.RFC3339)))
	if code != http.StatusCreated {
		t.Fatalf("create %s: %d %v", number, code, body)
	}
	o := body["order"].(map[string]any)
	return int64(o["id"].(float64))
}

func TestScanLookupAndAudit(t *testing.T) {
	ts, _ := newActivityServer(t)
	opTk := seedOperator(t, ts)
	orderID := mustCreate(t, ts, "ORD-SCAN-1")

	code, body := doAs(t, "POST", ts.URL+"/api/v1/scan",
		`{"code":"ORD-SCAN-1"}`, opTk)
	if code != http.StatusOK {
		t.Fatalf("scan: %d %v", code, body)
	}
	if body["matched"] != true || body["action"] != "LOOKUP" {
		t.Fatalf("scan result = %v", body)
	}
	ev := body["event"].(map[string]any)
	if ev["scannedBy"] != "op" || ev["code"] != "ORD-SCAN-1" {
		t.Errorf("event = %v", ev)
	}
	if _, ok := body["detail"].(map[string]any); !ok {
		t.Errorf("detail missing: %v", body)
	}

	code, _ = doAs(t, "POST", ts.URL+"/api/v1/scan", `{"code":"NOPE-404"}`, opTk)
	if code != http.StatusNotFound {
		t.Errorf("unknown code = %d, want 404", code)
	}

	code, _ = doAs(t, "POST", ts.URL+"/api/v1/scan", `{"code":"ORD-SCAN-1"}`, opTk)
	if code != http.StatusOK {
		t.Fatalf("rescan: %d", code)
	}

	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/scans", ts.URL, orderID), "")
	if code != http.StatusOK {
		t.Fatalf("scans: %d %v", code, body)
	}
	events := body["scanEvents"].([]any)
	if len(events) != 2 {
		t.Fatalf("scan events = %d, want 2", len(events))
	}
	for _, e := range events {
		if e.(map[string]any)["scannedBy"] != "op" {
			t.Errorf("scan actor = %v, want op", e)
		}
	}

	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/audit", ts.URL, orderID), "")
	if code != http.StatusOK {
		t.Fatalf("audit: %d %v", code, body)
	}
	logs := body["auditLogs"].([]any)
	scans := 0
	for _, l := range logs {
		if l.(map[string]any)["action"] == "barcode scanned: ORD-SCAN-1" {
			scans++
		}
	}
	if scans != 2 {
		t.Errorf("scan audit entries = %d, want 2", scans)
	}
}

func TestScanPermissionGated(t *testing.T) {
	ts, _ := newActivityServer(t)
	mustCreate(t, ts, "ORD-S")

	code, _ := doAs(t, "POST", ts.URL+"/api/v1/scan", `{"code":"ORD-S"}`, "")
	if code != http.StatusUnauthorized {
		t.Errorf("anonymous scan = %d, want 401", code)
	}

	code, body := do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"vic","password":"password123","role":"viewer"}`)
	if code != http.StatusCreated {
		t.Fatalf("create viewer: %d %v", code, body)
	}
	viewerTk := loginAs(t, ts.URL, "vic", "password123")
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/scan", `{"code":"ORD-S"}`, viewerTk)
	if code != http.StatusForbidden {
		t.Errorf("viewer scan = %d, want 403", code)
	}
}

func TestCommentsMentionsAndNotifications(t *testing.T) {
	ts, st := newActivityServer(t)
	opTk := seedOperator(t, ts)
	orderID := mustCreate(t, ts, "ORD-C-1")

	code, _ := doAs(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/comments", ts.URL, orderID), "", opTk)
	if code != http.StatusOK {
		t.Fatalf("list comments = %d, want 200", code)
	}

	xss := "<script>alert('x')</script> ping @admin and @ghost and @admin"
	code, body := doAs(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/comments", ts.URL, orderID),
		fmt.Sprintf(`{"body":%q}`, xss), opTk)
	if code != http.StatusCreated {
		t.Fatalf("add comment: %d %v", code, body)
	}
	comment := body["comment"].(map[string]any)
	if comment["body"] != xss {
		t.Errorf("body = %v", comment["body"])
	}
	mentions := comment["mentions"].([]any)
	if len(mentions) != 1 || mentions[0] != "admin" {
		t.Errorf("mentions = %v, want [admin] (deduped, ghost not a user)", mentions)
	}

	code, body = doAs(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/comments", ts.URL, orderID), "", opTk)
	if code != http.StatusOK {
		t.Fatalf("reload comments: %d", code)
	}
	stored := body["comments"].([]any)[0].(map[string]any)
	if got := stored["mentions"].([]any); len(got) != 1 || got[0] != "admin" {
		t.Errorf("mentions persisted = %v, want [admin]", got)
	}

	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/comments", ts.URL, orderID),
		`{"body":"self note @admin"}`, testAuthToken)
	if code != http.StatusCreated {
		t.Fatalf("self-mention comment: %d", code)
	}
	code, body = doAs(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/comments", ts.URL, orderID), "", opTk)
	last := body["comments"].([]any)[1].(map[string]any)
	if got := last["mentions"].([]any); len(got) != 1 || got[0] != "admin" {
		t.Errorf("self-mention persisted = %v, want [admin]", got)
	}
	code, body = do(t, "GET", ts.URL+"/api/v1/notifications", "")
	if n := body["unread"].(float64); n != 1 {
		t.Errorf("self-mention must not notify: unread = %v, want 1", n)
	}

	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/comments", ts.URL, orderID),
		`{"body":"@admin again"}`, opTk)
	if code != http.StatusCreated {
		t.Fatalf("second comment: %d", code)
	}

	code, body = do(t, "GET", ts.URL+"/api/v1/notifications", "")
	if code != http.StatusOK {
		t.Fatalf("notifications: %d %v", code, body)
	}
	if body["unread"].(float64) != 2 {
		t.Fatalf("admin unread = %v, want 2", body["unread"])
	}
	notes := body["notifications"].([]any)
	if len(notes) != 2 {
		t.Fatalf("admin notifications = %d, want 2", len(notes))
	}
	first := notes[0].(map[string]any)
	if first["kind"] != "mention" || first["orderNumber"] != "ORD-C-1" {
		t.Errorf("notification = %v", first)
	}

	ids := []int64{}
	for _, n := range notes {
		ids = append(ids, int64(n.(map[string]any)["id"].(float64)))
	}
	payload, _ := json.Marshal(map[string]any{"ids": ids})
	code, body = do(t, "POST", ts.URL+"/api/v1/notifications/read", string(payload))
	if code != http.StatusOK {
		t.Fatalf("mark read: %d %v", code, body)
	}
	if body["marked"].(float64) != 2 {
		t.Errorf("marked = %v, want 2", body["marked"])
	}

	code, body = do(t, "GET", ts.URL+"/api/v1/notifications", "")
	if code != http.StatusOK || body["unread"].(float64) != 0 {
		t.Fatalf("read state not persisted: %d %v", code, body)
	}

	if err := st.Notify(context.Background(), "admin", "mention", orderID, "ORD-C-1", 99999, "dup", fixedClock()); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := st.Notify(context.Background(), "admin", "mention", orderID, "ORD-C-1", 99999, "dup", fixedClock()); err != nil {
		t.Fatalf("notify again: %v", err)
	}
	_, unread, err := st.Notifications(context.Background(), "admin", false, 100)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if unread != 1 {
		t.Errorf("duplicate notification stored, unread = %d, want 1", unread)
	}
}

func TestCommentsPermissionGated(t *testing.T) {
	ts, _ := newActivityServer(t)
	orderID := mustCreate(t, ts, "ORD-C-2")

	code, body := do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"vic2","password":"password123","role":"viewer"}`)
	if code != http.StatusCreated {
		t.Fatalf("create viewer: %d %v", code, body)
	}
	viewerTk := loginAs(t, ts.URL, "vic2", "password123")

	url := fmt.Sprintf("%s/api/v1/orders/%d/comments", ts.URL, orderID)
	code, _ = doAs(t, "POST", url, `{"body":"hi @admin"}`, viewerTk)
	if code != http.StatusForbidden {
		t.Errorf("viewer comment = %d, want 403", code)
	}
	code, _ = doAs(t, "POST", url, `{"body":"hi @admin"}`, "")
	if code != http.StatusUnauthorized {
		t.Errorf("anonymous comment = %d, want 401", code)
	}
	code, _ = doAs(t, "POST", url, `{"body":"   "}`, testAuthToken)
	if code != http.StatusBadRequest {
		t.Errorf("empty comment = %d, want 400", code)
	}
	code, _ = doAs(t, "GET", url, "", viewerTk)
	if code != http.StatusOK {
		t.Errorf("viewer read comments = %d, want 200", code)
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/999/comments", ts.URL), `{"body":"x"}`)
	if code != http.StatusNotFound {
		t.Errorf("comment on missing order = %d, want 404", code)
	}
}
