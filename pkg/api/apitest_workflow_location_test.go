package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCustomerWorkflowLocationOverrideOverAPI(t *testing.T) {
	ts := newTestServer(t)
	const customer = "Northwind"
	payload := `{"steps":[{"key":"assembly","title":"Assembly","desc":"Build the unit","location":"Bench 3","checks":[{"key":"parts","label":"Parts ready"}],"next":null,"pick":false,"qcGate":false}]}`

	code, body := do(t, "PUT", ts.URL+"/api/v1/workflow?customer="+customer, payload)
	if code != http.StatusOK {
		t.Fatalf("save customer workflow = %d; body %v", code, body)
	}
	if body["source"] != "customer" {
		t.Fatalf("workflow source = %v, want customer", body["source"])
	}

	code, body = do(t, "GET", ts.URL+"/api/v1/workflow?customer="+customer, "")
	if code != http.StatusOK {
		t.Fatalf("get customer workflow = %d; body %v", code, body)
	}
	steps := body["steps"].([]any)
	step := steps[0].(map[string]any)
	if step["location"] != "Bench 3" {
		t.Fatalf("step location = %v, want Bench 3", step["location"])
	}

	tooLong := strings.Repeat("x", 81)
	invalid := fmt.Sprintf(`{"steps":[{"key":"assembly","title":"Assembly","location":%q,"checks":[]}]}`, tooLong)
	code, _ = do(t, "PUT", ts.URL+"/api/v1/workflow?customer="+customer, invalid)
	if code != http.StatusBadRequest {
		t.Fatalf("oversized location save = %d, want 400", code)
	}
}
