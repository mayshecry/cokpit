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
func TestChecklistsOverviewOverAPI(t *testing.T) {
	ts := newTestServer(t)
	id1 := mustCreate(t, ts, "ORD-CKO1")
	id2 := mustCreate(t, ts, "ORD-CKO2")

	code, body := do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/checklist", ts.URL, id1),
		`{"items":[{"artikel":"A-100","omschrijving":"Laptop","locatie":"DBC-01","aantal":2},{"artikel":"B-200","omschrijving":"Mouse","locatie":"DBC-02","aantal":5}]}`)
	if code != http.StatusOK {
		t.Fatalf("set checklist = %d; body %v", code, body)
	}
	items := body["items"].([]any)
	firstID := int64(items[0].(map[string]any)["id"].(float64))

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/checklist/%d/tick", ts.URL, firstID), "{}")
	if code != http.StatusOK {
		t.Fatalf("tick = %d", code)
	}

	code, body = do(t, "GET", ts.URL+"/api/v1/checklists", "")
	if code != http.StatusOK {
		t.Fatalf("list checklists = %d; body %v", code, body)
	}
	list := body["checklists"].([]any)
	found := false
	for _, raw := range list {
		cl := raw.(map[string]any)
		if int64(cl["orderId"].(float64)) != id1 {
			continue
		}
		found = true
		if cl["orderNumber"] != "ORD-CKO1" {
			t.Errorf("orderNumber = %v, want ORD-CKO1", cl["orderNumber"])
		}
		if int(cl["total"].(float64)) != 2 || int(cl["done"].(float64)) != 1 {
			t.Errorf("progress = %v/%v, want 1/2", cl["done"], cl["total"])
		}
		its := cl["items"].([]any)
		if len(its) != 2 {
			t.Fatalf("items = %d, want 2", len(its))
		}
		if its[0].(map[string]any)["checkedBy"] != "admin" {
			t.Errorf("items[0].checkedBy = %v, want admin", its[0].(map[string]any)["checkedBy"])
		}
	}
	if !found {
		t.Fatalf("order %d missing from overview list (%d entries)", id1, len(list))
	}
	for _, raw := range list {
		if int64(raw.(map[string]any)["orderId"].(float64)) == id2 {
			t.Errorf("order %d without checklist must not be listed", id2)
		}
	}
}

func TestChecklistsDemoSeedOverAPI(t *testing.T) {
	ts := newTestServer(t)
	const wantDemo = 3

	code, body := do(t, "GET", ts.URL+"/api/v1/checklists", "")
	if code != http.StatusOK {
		t.Fatalf("list checklists = %d; body %v", code, body)
	}
	if got := len(body["checklists"].([]any)); got != 0 {
		t.Fatalf("empty store listed %d checklists, want 0", got)
	}

	code, body = do(t, "POST", ts.URL+"/api/v1/checklists/demo", "{}")
	if code != http.StatusOK {
		t.Fatalf("seed demo = %d; body %v", code, body)
	}
	if int(body["created"].(float64)) != wantDemo {
		t.Fatalf("created = %v, want %d", body["created"], wantDemo)
	}

	code, body = do(t, "GET", ts.URL+"/api/v1/checklists", "")
	if code != http.StatusOK {
		t.Fatalf("list after seed = %d", code)
	}
	list := body["checklists"].([]any)
	if len(list) != wantDemo {
		t.Fatalf("seeded %d checklists, want %d", len(list), wantDemo)
	}

	var demoPreticked bool
	for _, raw := range list {
		cl := raw.(map[string]any)
		if cl["orderNumber"] != "DEMO-1002" {
			continue
		}
		its := cl["items"].([]any)
		if len(its) < 2 {
			t.Fatalf("DEMO-1002 has %d items, want >= 2", len(its))
		}
		it1 := its[1].(map[string]any)
		if it1["checkedBy"] != "demo" {
			t.Errorf("pre-ticked demo line checkedBy = %v, want demo", it1["checkedBy"])
		}
		if it1["checkedAt"] == nil {
			t.Error("pre-ticked demo line must have checkedAt")
		}
		if int(cl["done"].(float64)) != 1 || int(cl["total"].(float64)) != len(its) {
			t.Errorf("DEMO-1002 progress = %v/%v, want 1/%d", cl["done"], cl["total"], len(its))
		}
		demoPreticked = true
	}
	if !demoPreticked {
		t.Fatal("DEMO-1002 missing from seeded overview")
	}

	code, body = do(t, "POST", ts.URL+"/api/v1/checklists/demo", "{}")
	if code != http.StatusOK {
		t.Fatalf("second seed = %d", code)
	}
	if int(body["created"].(float64)) != 0 {
		t.Fatalf("second seed created %v, want 0", body["created"])
	}
	code, body = do(t, "GET", ts.URL+"/api/v1/checklists", "")
	if code != http.StatusOK {
		t.Fatalf("second list = %d", code)
	}
	if got := len(body["checklists"].([]any)); got != wantDemo {
		t.Fatalf("second list has %d checklists, want %d (re-seed?)", got, wantDemo)
	}

	code, body = do(t, "POST", ts.URL+"/api/v1/orders", `{"orderNumber":"ORD-REAL"}`)
	if code != http.StatusCreated {
		t.Fatalf("create order = %d; body %v", code, body)
	}
	id := int64(body["order"].(map[string]any)["id"].(float64))
	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/checklist", ts.URL, id),
		`{"items":[{"artikel":"REAL-1","omschrijving":"Real line","locatie":"PICK-Z99","aantal":1}]}`)
	if code != http.StatusOK {
		t.Fatalf("set checklist = %d", code)
	}
	code, body = do(t, "GET", ts.URL+"/api/v1/checklists", "")
	if code != http.StatusOK {
		t.Fatalf("list after upload = %d", code)
	}
	list = body["checklists"].([]any)
	if len(list) != wantDemo+1 {
		t.Fatalf("after real upload: %d checklists, want %d", len(list), wantDemo+1)
	}
	found := false
	for _, raw := range list {
		if int64(raw.(map[string]any)["orderId"].(float64)) == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("order %d with real checklist missing from overview", id)
	}

	code, _ = doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"vera","password":"password123","displayName":"Vera","role":"viewer"}`, testAuthToken)
	if code != http.StatusCreated {
		t.Fatalf("create viewer: %d", code)
	}
	veraTk := loginAs(t, ts.URL, "vera", "password123")
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/checklists/demo", "{}", veraTk)
	if code != http.StatusForbidden {
		t.Errorf("viewer seed demo = %d, want 403", code)
	}
}
