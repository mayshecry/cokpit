package api

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAutomationAPIRunsAfterQCFailure(t *testing.T) {
	ts := newTestServer(t)
	orderID := mustCreate(t, ts, "AUTO-API-1")
	code, body := do(t, "POST", ts.URL+"/api/v1/automations", `{
		"name":"QC failure alert", "trigger":"QC_FAILED", "action":"NOTIFY",
		"targets":["admin"], "message":"Investigate today", "enabled":true
	}`)
	if code != http.StatusCreated {
		t.Fatalf("create automation = %d %v", code, body)
	}
	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/qc", ts.URL, orderID),
		`{"status":"FAIL","failReason":"Self-test skipped","notes":"automation test"}`)
	if code != http.StatusCreated {
		t.Fatalf("submit QC = %d %v", code, body)
	}
	code, body = do(t, "GET", ts.URL+"/api/v1/automation-runs", "")
	if code != http.StatusOK {
		t.Fatalf("list runs = %d %v", code, body)
	}
	runs := body["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("runs = %v, want one", runs)
	}
	run := runs[0].(map[string]any)
	if run["status"] != "SUCCESS" || run["triggerKey"] != "qc:1" {
		t.Errorf("run = %v", run)
	}
	code, body = do(t, "POST", ts.URL+"/api/v1/automations/evaluate", `{}`)
	if code != http.StatusOK || body["executed"].(float64) != 0 {
		t.Errorf("repeat evaluation = %d %v", code, body)
	}
	code, body = do(t, "GET", ts.URL+"/api/v1/notifications", "")
	if code != http.StatusOK {
		t.Fatal("list notifications failed")
	}
	notes := body["notifications"].([]any)
	if len(notes) != 1 || notes[0].(map[string]any)["kind"] != "automation" {
		t.Errorf("notifications = %v", notes)
	}
}

func TestAutomationAPIValidation(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, "POST", ts.URL+"/api/v1/automations", `{
		"name":"Missing target", "trigger":"QC_FAILED", "action":"ASSIGN", "targets":["ghost"], "enabled":true
	}`)
	if code != http.StatusBadRequest {
		t.Errorf("unknown target = %d %v, want 400", code, body)
	}
	code, body = do(t, "POST", ts.URL+"/api/v1/automations", `{
		"name":"Bad threshold", "trigger":"HOLD_AGING", "thresholdMinutes":0,
		"action":"NOTIFY", "targets":["admin"], "enabled":true
	}`)
	if code != http.StatusBadRequest {
		t.Errorf("bad threshold = %d %v, want 400", code, body)
	}
}
