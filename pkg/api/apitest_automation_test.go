package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationRuleScopeAndTeams(t *testing.T) {
	ts := newTestServer(t)
	body := `{
		"name": "Kenter SLA watchdog",
		"trigger": "SLA_BREACHED",
		"action": "NOTIFY",
		"targets": ["admin"],
		"teams": [],
		"customers": ["Kenter", "Dela"],
		"excludedCustomers": ["Internal"],
		"departments": ["Warehouse"],
		"products": [],
		"customerTargets": {"Kenter": ["admin"]},
		"message": "please pick this up",
		"enabled": true
	}`
	code, resp := do(t, "POST", ts.URL+"/api/v1/automations", body)
	if code != http.StatusCreated {
		t.Fatalf("status = %d, body %v", code, resp)
	}
	rule := resp["rule"].(map[string]any)
	if got, _ := rule["customers"].([]any); len(got) != 2 {
		t.Errorf("customers = %v", rule["customers"])
	}
	if got, _ := rule["excludedCustomers"].([]any); len(got) != 1 || got[0] != "Internal" {
		t.Errorf("excludedCustomers = %v", rule["excludedCustomers"])
	}
	if got, _ := rule["departments"].([]any); len(got) != 1 || got[0] != "Warehouse" {
		t.Errorf("departments = %v", rule["departments"])
	}
	if m, _ := rule["customerTargets"].(map[string]any); m == nil || len(m["Kenter"].([]any)) != 1 {
		t.Errorf("customerTargets = %v", rule["customerTargets"])
	}
}

func TestAutomationAssignMultiplePeopleMessage(t *testing.T) {
	ts := newTestServer(t)
	code, resp := do(t, "POST", ts.URL+"/api/v1/automations", `{
		"name": "Assign to many", "trigger": "SLA_BREACHED", "action": "ASSIGN",
		"targets": ["admin", "admin2"], "enabled": true
	}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %v", code, resp)
	}
	msg := resp["error"].(map[string]any)["message"].(string)
	if !strings.Contains(msg, "exactly one person or team") {
		t.Errorf("message = %q", msg)
	}
	if !strings.Contains(msg, "NOTIFY") {
		t.Errorf("message should point at NOTIFY as the alternative: %q", msg)
	}
	if code := resp["error"].(map[string]any)["code"]; code != "invalid_automation_rule" {
		t.Errorf("code = %v", code)
	}
}

func TestAutomationUnknownTeam(t *testing.T) {
	ts := newTestServer(t)
	code, resp := do(t, "POST", ts.URL+"/api/v1/automations", `{
		"name": "Ghost team", "trigger": "SLA_BREACHED", "action": "NOTIFY",
		"teams": ["No such department"], "enabled": true
	}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %v", code, resp)
	}
	code2, _ := resp["error"].(map[string]any)["code"]
	if code2 != "unknown_department" {
		t.Errorf("code = %v, want unknown_department", code2)
	}
}

func TestAutomationPreviewEndpoint(t *testing.T) {
	ts := newTestServer(t)
	// One order that is already past its target.
	code, _ := do(t, "POST", ts.URL+"/api/v1/orders", fmt.Sprintf(
		`{"orderNumber":"PREV-1","customerName":"Kenter","targetCompletionAt":%q}`,
		fixedClock().Add(-2*60*60*1e9).UTC().Format("2006-01-02T15:04:05Z")))
	if code != http.StatusCreated {
		t.Fatalf("create order: %d", code)
	}

	code, resp := do(t, "POST", ts.URL+"/api/v1/automations/preview", `{
		"name": "Preview", "trigger": "SLA_BREACHED", "action": "NOTIFY",
		"targets": ["admin"], "customers": ["Kenter"], "enabled": true
	}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, resp)
	}
	if matched, _ := resp["matched"].(float64); matched < 1 {
		t.Fatalf("matched = %v, want at least the Kenter order", resp["matched"])
	}
	candidates, _ := resp["candidates"].([]any)
	if len(candidates) == 0 {
		t.Fatalf("no candidates: %v", resp)
	}
	first := candidates[0].(map[string]any)
	if first["customerName"] != "Kenter" {
		t.Errorf("customerName = %v", first["customerName"])
	}
	if recips, _ := first["recipients"].([]any); len(recips) != 1 || recips[0] != "admin" {
		t.Errorf("recipients = %v", first["recipients"])
	}
}

func TestAutomationPreviewRequiresPermission(t *testing.T) {
	ts := newTestServer(t)
	code, _ := doAs(t, "POST", ts.URL+"/api/v1/automations/preview", `{"name":"x","trigger":"SLA_BREACHED","action":"NOTIFY","targets":["admin"]}`, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous preview status = %d, want 401", code)
	}
}
