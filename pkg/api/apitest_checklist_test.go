package api

import (
	"fmt"
	"net/http"
	"testing"
)

func TestChecklistFlowOverAPI(t *testing.T) {
	ts := newTestServer(t)
	id := mustCreate(t, ts, "ORD-CK1")

	code, _ := do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/checklist", ts.URL, id), "")
	if code != http.StatusOK {
		t.Fatalf("empty checklist = %d, want 200", code)
	}

	code, body := do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/checklist", ts.URL, id),
		`{"items":[{"artikel":"A-100","omschrijving":"Laptop","locatie":"DBC-01","aantal":2},{"artikel":"B-200","omschrijving":"Mouse","locatie":"DBC-02","aantal":5}]}`)
	if code != http.StatusOK {
		t.Fatalf("set checklist = %d; body %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	first := items[0].(map[string]any)
	itemID := int64(first["id"].(float64))
	if first["checkedBy"] != nil {
		t.Errorf("fresh item checkedBy = %v, want nil", first["checkedBy"])
	}

	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/checklist/%d/tick", ts.URL, itemID), "{}")
	if code != http.StatusOK {
		t.Fatalf("tick = %d; body %v", code, body)
	}
	item := body["item"].(map[string]any)
	if item["checkedBy"] != "admin" {
		t.Errorf("checkedBy = %v, want admin", item["checkedBy"])
	}
	if item["checkedAt"] == nil {
		t.Error("checkedAt must be set after tick")
	}

	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/checklist", ts.URL, id), "")
	if code != http.StatusOK {
		t.Fatalf("get after tick = %d", code)
	}
	items = body["items"].([]any)
	it0 := items[0].(map[string]any)
	if it0["checkedBy"] != "admin" {
		t.Errorf("persisted checkedBy = %v, want admin", it0["checkedBy"])
	}

	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/checklist/%d/untick", ts.URL, itemID), "{}")
	if code != http.StatusOK {
		t.Fatalf("untick = %d", code)
	}

	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/checklist", ts.URL, id), "")
	if code != http.StatusOK {
		t.Fatalf("get after untick = %d", code)
	}
	items = body["items"].([]any)
	it0 = items[0].(map[string]any)
	if it0["checkedBy"] != nil {
		t.Errorf("checkedBy after untick = %v, want nil", it0["checkedBy"])
	}

	code, _ = do(t, "POST", ts.URL+"/api/v1/checklist/99999/tick", "{}")
	if code != http.StatusNotFound {
		t.Errorf("tick missing item = %d, want 404", code)
	}
}

func TestChecklistRequiresAuth(t *testing.T) {
	ts := newTestServer(t)
	code, _ := doAs(t, "GET", ts.URL+"/api/v1/orders/99999/checklist", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", code)
	}
}