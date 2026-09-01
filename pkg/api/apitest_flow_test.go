package api

import (
	"fmt"
	"net/http"
	"testing"
)

func TestTransitionFlowOverAPI(t *testing.T) {
	ts := newTestServer(t)
	id := mustCreate(t, ts, "ORD-T1")

	code, body := do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/transition", ts.URL, id),
		`{"status":"Completed","performedBy":"alice"}`)
	if code != http.StatusConflict {
		t.Fatalf("jump status = %d, want 409; body %v", code, body)
	}

	for _, st := range []string{"Processing", "QC_Review"} {
		code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/transition", ts.URL, id),
			fmt.Sprintf(`{"status":%q,"performedBy":"alice"}`, st))
		if code != http.StatusOK {
			t.Fatalf("transition %s status = %d", st, code)
		}
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/transition", ts.URL, id),
		`{"status":"Completed"}`)
	if code != http.StatusConflict {
		t.Errorf("complete w/o qc = %d, want 409", code)
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/qc", ts.URL, id),
		`{"status":"PASS","inspectorId":"insp-9","notes":"ok"}`)
	if code != http.StatusCreated {
		t.Fatalf("qc pass = %d, want 201", code)
	}
	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/transition", ts.URL, id),
		`{"status":"Completed","performedBy":"alice"}`)
	if code != http.StatusOK {
		t.Fatalf("complete = %d; body %v", code, body)
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/transition", ts.URL, id),
		`{"status":"Processing"}`)
	if code != http.StatusConflict {
		t.Errorf("transitioning completed = %d, want 409", code)
	}
	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/holds", ts.URL, id),
		`{"reason":"late issue"}`)
	if code != http.StatusConflict {
		t.Errorf("hold on completed = %d, want 409", code)
	}
}

func TestHoldFlowOverAPI(t *testing.T) {
	ts := newTestServer(t)
	id := mustCreate(t, ts, "ORD-H1")
	code, _ := do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/transition", ts.URL, id),
		`{"status":"Processing"}`)
	if code != http.StatusOK {
		t.Fatalf("to processing: %d", code)
	}

	code, body := do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/holds", ts.URL, id),
		`{"reason":"awaiting parts","createdBy":"bob"}`)
	if code != http.StatusCreated {
		t.Fatalf("place hold = %d; body %v", code, body)
	}
	orderAfter := body["order"].(map[string]any)
	if orderAfter["status"] != "Held_Processing" {
		t.Errorf("held status = %v, want Held_Processing", orderAfter["status"])
	}
	holdID := int64(body["hold"].(map[string]any)["id"].(float64))

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/transition", ts.URL, id),
		`{"status":"QC_Review"}`)
	if code != http.StatusConflict {
		t.Errorf("transition while held = %d, want 409", code)
	}

	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/holds/%d/resolve", ts.URL, holdID), `{}`)
	if code != http.StatusOK {
		t.Fatalf("resolve = %d; body %v", code, body)
	}
	if orderAfter := body["order"].(map[string]any); orderAfter["status"] != "Processing" {
		t.Errorf("restored status = %v, want Processing", orderAfter["status"])
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/holds/%d/resolve", ts.URL, holdID), `{}`)
	if code != http.StatusConflict {
		t.Errorf("double resolve = %d, want 409", code)
	}
}

func TestAuditEndpoint(t *testing.T) {
	ts := newTestServer(t)
	id := mustCreate(t, ts, "ORD-A1")
	code, body := do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/audit", ts.URL, id), "")
	if code != http.StatusOK {
		t.Fatalf("audit = %d", code)
	}
	logs := body["auditLogs"].([]any)
	if len(logs) != 1 {
		t.Errorf("audit entries = %d, want 1", len(logs))
	}

	code, _ = do(t, "GET", ts.URL+"/api/v1/orders/99999/audit", "")
	if code != http.StatusNotFound {
		t.Errorf("missing audit = %d, want 404", code)
	}
}

func TestNotFound(t *testing.T) {
	ts := newTestServer(t)
	code, _ := do(t, "GET", ts.URL+"/api/v1/orders/99999", "")
	if code != http.StatusNotFound {
		t.Errorf("GET missing order = %d, want 404", code)
	}
	code, _ = do(t, "POST", ts.URL+"/api/v1/orders/99999/qc",
		`{"status":"PASS","inspectorId":"i"}`)
	if code != http.StatusNotFound {
		t.Errorf("POST qc missing order = %d, want 404", code)
	}
	code, _ = do(t, "POST", ts.URL+"/api/v1/orders/99999/holds",
		`{"reason":"x"}`)
	if code != http.StatusNotFound {
		t.Errorf("POST holds missing order = %d, want 404", code)
	}
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, "GET", ts.URL+"/healthz", "")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Errorf("healthz = %d %v", code, body)
	}
}

func TestBadRequestErrorShape(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, "POST", ts.URL+"/api/v1/orders", `{}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d", code)
	}
	errBody, ok := body["error"].(map[string]any)
	if !ok || errBody["code"] != "bad_request" {
		t.Errorf("error envelope = %v", body["error"])
	}
}
