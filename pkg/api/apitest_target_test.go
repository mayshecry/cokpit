package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"cockpit/pkg/order"
)


func TestImportOrderPastTarget(t *testing.T) {
	ts := newTestServer(t)
	past := fixedClock().Add(-48 * time.Hour).UTC().Format(time.RFC3339)

	code, body := do(t, "POST", ts.URL+"/api/v1/process/orders/import", fmt.Sprintf(
		`{"orderNumber":"IMP-PAST","customerName":"Acme","targetCompletionAt":%q}`, past))
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("status = %d, want 2xx; body %v", code, body)
	}
	o := body["order"].(map[string]any)
	if got := o["targetCompletionAt"].(string); got != past {
		t.Errorf("imported targetCompletionAt = %s, want %s (kept, not replaced with now+SLA)", got, past)
	}
	if pastFlag, ok := body["targetIsPast"].(bool); !ok || !pastFlag {
		t.Errorf("targetIsPast = %v, want true", body["targetIsPast"])
	}
}

func TestImportOrderRejectsImplausibleTarget(t *testing.T) {
	ts := newTestServer(t)
	for _, target := range []string{"1990-01-01T00:00:00Z", "2999-01-01T00:00:00Z"} {
		code, body := do(t, "POST", ts.URL+"/api/v1/process/orders/import", fmt.Sprintf(
			`{"orderNumber":"IMP-BAD","customerName":"Acme","targetCompletionAt":%q}`, target))
		if code != http.StatusBadRequest {
			t.Errorf("target %s: status = %d, want 400; body %v", target, code, body)
		}
		if code, _ := do(t, "GET", ts.URL+"/api/v1/orders?search=IMP-BAD", ""); code != http.StatusOK {
			t.Fatalf("list: %d", code)
		}
	}
}

// Regression guard for the SLA rendering of an overdue registration.
func TestPastTargetSLAIsBreached(t *testing.T) {
	ts := newTestServer(t)
	past := fixedClock().Add(-time.Hour).UTC()
	code, body := do(t, "POST", ts.URL+"/api/v1/orders", fmt.Sprintf(
		`{"orderNumber":"ORD-LATE","targetCompletionAt":%q}`, past.Format(time.RFC3339)))
	if code != http.StatusCreated {
		t.Fatalf("status = %d, body %v", code, body)
	}
	sla, _ := body["sla"].(map[string]any)
	if sla == nil || sla["status"] != string(order.SLABreached) {
		t.Fatalf("sla = %v, want BREACHED", body["sla"])
	}
}
