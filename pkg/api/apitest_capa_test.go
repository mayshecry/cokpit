package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestCAPAAPIOwnerProgressAndQCVerification(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"owner","password":"password123","role":"operator"}`)
	if code != http.StatusCreated {
		t.Fatalf("create owner = %d %v", code, body)
	}
	ownerToken := loginAs(t, ts.URL, "owner", "password123")
	orderID := mustCreate(t, ts, "CAPA-API-1")
	due := fixedClock().Add(48 * time.Hour).Format(time.RFC3339)
	code, body = do(t, "POST", ts.URL+"/api/v1/capa", fmt.Sprintf(`{
		"orderId":%d, "title":"Investigate repeated failures", "owner":"owner", "dueAt":%q,
		"failReason":"Config template wrong"
	}`, orderID, due))
	if code != http.StatusCreated {
		t.Fatalf("create CAPA = %d %v", code, body)
	}
	action := body["action"].(map[string]any)
	id := int64(action["id"].(float64))

	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/capa/%d", ts.URL, id),
		`{"title":"Owner cannot reclassify"}`, ownerToken)
	if code != http.StatusForbidden {
		t.Errorf("owner classification change = %d %v, want 403", code, body)
	}
	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/capa/%d", ts.URL, id), `{
		"rootCause":" outdated template ", "actionPlan":"Version every template",
		"resolution":"Updated and validated the template", "status":"PENDING_VERIFICATION"
	}`, ownerToken)
	if code != http.StatusOK || body["action"].(map[string]any)["status"] != "PENDING_VERIFICATION" {
		t.Fatalf("owner submit = %d %v", code, body)
	}
	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/capa/%d/verify", ts.URL, id),
		`{"note":"Verified by the owner"}`, ownerToken)
	if code != http.StatusForbidden {
		t.Errorf("owner verify = %d %v, want 403", code, body)
	}
	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/capa/%d/verify", ts.URL, id),
		`{"note":"Verified against the baseline"}`)
	if code != http.StatusOK || body["action"].(map[string]any)["status"] != "CLOSED" {
		t.Fatalf("QC verify = %d %v", code, body)
	}
	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/capa/%d", ts.URL, id), "")
	if code != http.StatusOK {
		t.Fatalf("CAPA detail = %d %v", code, body)
	}
	if events := body["events"].([]any); len(events) != 3 {
		t.Errorf("events = %v, want create, submit and verification", events)
	}
	code, body = do(t, "GET", ts.URL+"/api/v1/capa?status=CLOSED", "")
	if code != http.StatusOK || len(body["actions"].([]any)) != 1 {
		t.Errorf("closed filter = %d %v", code, body)
	}
}
