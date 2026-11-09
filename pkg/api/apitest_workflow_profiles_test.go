package api

import (
	"fmt"
	"net/http"
	"testing"
)

func TestWorkflowProfileCanBeSelectedWhenCreatingOrder(t *testing.T) {
	ts := newTestServer(t)
	profileBody := `{"name":"Refurbish","steps":[{"key":"inspect","title":"Inspect","desc":"Check the returned device","location":"Intake table","checks":[{"key":"serial","label":"Verify serial number"}],"next":null,"pick":false,"qcGate":false}]}`
	code, body := do(t, "POST", ts.URL+"/api/v1/workflow-profiles?customer=Acme", profileBody)
	if code != http.StatusCreated {
		t.Fatalf("create workflow profile = %d; body %v", code, body)
	}
	profile := body["profile"].(map[string]any)
	profileID := int64(profile["id"].(float64))

	code, body = do(t, "POST", ts.URL+"/api/v1/orders", fmt.Sprintf(`{"orderNumber":"ORD-PROFILE","customerName":"Acme","workflowProfileId":%d}`, profileID))
	if code != http.StatusCreated {
		t.Fatalf("create order with workflow profile = %d; body %v", code, body)
	}
	created := body["order"].(map[string]any)
	if int64(created["workflowProfileId"].(float64)) != profileID || created["workflowProfileName"] != "Refurbish" {
		t.Fatalf("created order workflow profile = %v / %v", created["workflowProfileId"], created["workflowProfileName"])
	}

	code, body = do(t, "POST", ts.URL+"/api/v1/orders", fmt.Sprintf(`{"orderNumber":"ORD-WRONG-CUSTOMER","customerName":"Other","workflowProfileId":%d}`, profileID))
	if code != http.StatusBadRequest {
		t.Fatalf("cross-customer workflow assignment = %d, want 400; body %v", code, body)
	}
}
