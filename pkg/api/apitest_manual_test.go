package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func mustCreateProduct(t *testing.T, ts, code, name string) int64 {
	t.Helper()
	c, body := do(t, "POST", ts+"/api/v1/products",
		fmt.Sprintf(`{"code":%q,"name":%q,"description":"demo"}`, code, name))
	if c != http.StatusCreated {
		t.Fatalf("create product %s: %d %v", code, c, body)
	}
	return int64(body["product"].(map[string]any)["id"].(float64))
}

func mustAddBlock(t *testing.T, ts string, productID int64, title, assignee string) int64 {
	t.Helper()
	payload := fmt.Sprintf(`{"title":%q,"body":"step body"}`, title)
	if assignee != "" {
		payload = fmt.Sprintf(`{"title":%q,"body":"step body","assignee":%q}`, title, assignee)
	}
	c, body := do(t, "POST", fmt.Sprintf("%s/api/v1/products/%d/blocks", ts, productID), payload)
	if c != http.StatusCreated {
		t.Fatalf("add block %s: %d %v", title, c, body)
	}
	return int64(body["block"].(map[string]any)["id"].(float64))
}

func TestProductAndManualFlowOverAPI(t *testing.T) {
	ts := newTestServer(t)
	pid := mustCreateProduct(t, ts.URL, "PROD-100", "Laptop bundle")

	b1 := mustAddBlock(t, ts.URL, pid, "Unpack box", "")
	b2 := mustAddBlock(t, ts.URL, pid, "Install memory", "")
	b3 := mustAddBlock(t, ts.URL, pid, "Quality sticker", "")

	code, body := do(t, "GET", fmt.Sprintf("%s/api/v1/products/%d", ts.URL, pid), "")
	if code != http.StatusOK {
		t.Fatalf("get product: %d", code)
	}
	blocks := body["blocks"].([]any)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}
	if int(blocks[0].(map[string]any)["seq"].(float64)) != 0 ||
		int(blocks[2].(map[string]any)["seq"].(float64)) != 2 {
		t.Fatalf("template seqs wrong: %v", blocks)
	}

	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/products/%d/blocks/%d/move", ts.URL, pid, b1),
		`{"direction":"down"}`)
	if code != http.StatusOK {
		t.Fatalf("move down: %d %v", code, body)
	}
	blocks = body["blocks"].([]any)
	if int64(blocks[0].(map[string]any)["id"].(float64)) != b2 ||
		int64(blocks[1].(map[string]any)["id"].(float64)) != b1 {
		t.Fatalf("after move down order should be [b2,b1,b3], got %v", blocks)
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/products/%d/blocks/%d/move", ts.URL, pid, b2),
		`{"direction":"up"}`)
	if code != http.StatusConflict {
		t.Errorf("move first block up = %d, want 409", code)
	}

	code, _ = do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"plex","password":"password123","displayName":"Plex","role":"operator"}`)
	if code != http.StatusCreated {
		t.Fatalf("create operator: %d", code)
	}
	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/products/%d/blocks/%d", ts.URL, pid, b2),
		`{"assignee":"plex"}`)
	if code != http.StatusOK {
		t.Fatalf("assign block: %d %v", code, body)
	}
	if got := body["block"].(map[string]any)["assignee"]; got != "plex" {
		t.Errorf("assignee = %v, want plex", got)
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/products/%d/blocks/%d", ts.URL, pid, b2),
		`{"assignee":"nobody"}`)
	if code != http.StatusBadRequest {
		t.Errorf("unknown assignee = %d, want 400", code)
	}

	code, _ = do(t, "DELETE", fmt.Sprintf("%s/api/v1/products/%d/blocks/%d", ts.URL, pid, b3), "")
	if code != http.StatusOK {
		t.Fatalf("delete block: %d", code)
	}
	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/products/%d", ts.URL, pid), "")
	blocks = body["blocks"].([]any)
	if len(blocks) != 2 || int(blocks[1].(map[string]any)["seq"].(float64)) != 1 {
		t.Fatalf("after delete seqs must renumber, got %v", blocks)
	}

	code, _ = do(t, "POST", ts.URL+"/api/v1/products", `{"code":"PROD-100","name":"Clash"}`)
	if code != http.StatusConflict {
		t.Errorf("duplicate product = %d, want 409", code)
	}
}

func TestInstantiateAndTickManualOverAPI(t *testing.T) {
	ts := newTestServer(t)
	oid := mustCreate(t, ts, "ORD-MAN1")
	pid := mustCreateProduct(t, ts.URL, "PROD-200", "Printer")
	mustAddBlock(t, ts.URL, pid, "Unpack", "")
	b2 := mustAddBlock(t, ts.URL, pid, "Load paper", "")
	mustAddBlock(t, ts.URL, pid, "Print test page", "")

	code, _ := do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"ops","password":"password123","displayName":"Ops","role":"operator"}`)
	if code != http.StatusCreated {
		t.Fatalf("create operator: %d", code)
	}
	opsTk := loginAs(t, ts.URL, "ops", "password123")

	code, body := doAs(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid),
		fmt.Sprintf(`{"productId":%d}`, pid), opsTk)
	if code != http.StatusCreated {
		t.Fatalf("instantiate: %d %v", code, body)
	}
	manual := body["manual"].(map[string]any)
	mid := int64(manual["id"].(float64))
	if int(manual["total"].(float64)) != 3 {
		t.Fatalf("manual total = %v, want 3", manual["total"])
	}

	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid),
		fmt.Sprintf(`{"productId":%d}`, pid), opsTk)
	if code != http.StatusConflict {
		t.Errorf("duplicate manual = %d, want 409", code)
	}

	blocks := manual["blocks"].([]any)
	block1 := int64(blocks[0].(map[string]any)["id"].(float64))
	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/answer", ts.URL, mid, block1),
		`{"answer":"YES"}`, opsTk)
	if code != http.StatusOK {
		t.Fatalf("answer YES: %d %v", code, body)
	}
	b := body["block"].(map[string]any)
	if b["answer"] != "YES" || b["answeredBy"] != "ops" || b["answeredAt"] == nil {
		t.Fatalf("YES not attributed to ops: %v", b)
	}

	code, _ = do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"peep","password":"password123","displayName":"Peep","role":"viewer"}`)
	if code != http.StatusCreated {
		t.Fatalf("create viewer: %d", code)
	}
	peepTk := loginAs(t, ts.URL, "peep", "password123")
	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/answer", ts.URL, mid, b2),
		`{"answer":"NO"}`, peepTk)
	if code != http.StatusForbidden {
		t.Errorf("viewer answer = %d, want 403", code)
	}
	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/answer", ts.URL, mid, b2),
		`{"answer":"NO"}`, opsTk)
	if code != http.StatusOK {
		t.Fatalf("answer NO: %d %v", code, body)
	}

	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid), "")
	if code != http.StatusOK {
		t.Fatalf("list order manuals: %d", code)
	}
	manuals := body["manuals"].([]any)
	if len(manuals) != 1 {
		t.Fatalf("manuals = %d, want 1", len(manuals))
	}
	m := manuals[0].(map[string]any)
	if int(m["answered"].(float64)) != 2 || int(m["failed"].(float64)) != 1 {
		t.Fatalf("progress = %v/%v failed %v, want 2/3 failed 1", m["answered"], m["total"], m["failed"])
	}
	log := m["log"].([]any)
	if len(log) != 2 {
		t.Fatalf("tick log = %d entries, want 2", len(log))
	}
	first := log[0].(map[string]any)
	if first["username"] != "ops" || first["action"] != "YES" {
		t.Fatalf("first log entry = %v, want ops/YES", first)
	}

	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/answer", ts.URL, mid, block1),
		`{"answer":"CLEAR"}`, opsTk)
	if code != http.StatusOK {
		t.Fatalf("clear: %d %v", code, body)
	}
	if got, ok := body["block"].(map[string]any)["answer"]; ok && got != "" {
		t.Fatalf("cleared block answer = %v, want empty/absent", got)
	}
	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid), "")
	m = body["manuals"].([]any)[0].(map[string]any)
	if int(m["answered"].(float64)) != 1 {
		t.Fatalf("answered after clear = %v, want 1", m["answered"])
	}
	if len(m["log"].([]any)) != 3 {
		t.Fatalf("tick log after clear = %d, want 3 (log is append-only)", len(m["log"].([]any)))
	}

	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/answer", ts.URL, mid, block1),
		`{"answer":"MAYBE"}`, opsTk)
	if code != http.StatusBadRequest {
		t.Errorf("bad answer = %d, want 400", code)
	}

	code, body = do(t, "GET", ts.URL+"/api/v1/manuals", "")
	if code != http.StatusOK {
		t.Fatalf("manuals overview: %d", code)
	}
	ovs := body["manuals"].([]any)
	if len(ovs) != 1 {
		t.Fatalf("overview manuals = %d, want 1", len(ovs))
	}
	ov := ovs[0].(map[string]any)
	if ov["orderNumber"] != "ORD-MAN1" || ov["productCode"] != "PROD-200" {
		t.Fatalf("overview identity wrong: %v", ov)
	}
	if len(ov["blocks"].([]any)) != 3 {
		t.Fatalf("overview blocks = %d, want 3", len(ov["blocks"].([]any)))
	}
}

func TestManualRBACOverAPI(t *testing.T) {
	ts := newTestServer(t)

	code, _ := do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"vera","password":"password123","displayName":"Vera","role":"viewer"}`)
	if code != http.StatusCreated {
		t.Fatalf("create viewer: %d", code)
	}
	veraTk := loginAs(t, ts.URL, "vera", "password123")
	code, _ = doAs(t, "POST", ts.URL+"/api/v1/products", `{"code":"X1","name":"X"}`, veraTk)
	if code != http.StatusForbidden {
		t.Errorf("viewer create product = %d, want 403", code)
	}
	code, _ = doAs(t, "GET", ts.URL+"/api/v1/products", "", veraTk)
	if code != http.StatusOK {
		t.Errorf("viewer list products = %d, want 200", code)
	}

	code, _ = doAs(t, "GET", ts.URL+"/api/v1/products", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("no token list products = %d, want 401", code)
	}

	oid := mustCreate(t, ts, "ORD-MAN2")
	pid := mustCreateProduct(t, ts.URL, "PROD-300", "Scanner")
	mustAddBlock(t, ts.URL, pid, "Step one", "")
	code, body := do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid),
		fmt.Sprintf(`{"productId":%d}`, pid))
	if code != http.StatusCreated {
		t.Fatalf("instantiate: %d %v", code, body)
	}
	mid := int64(body["manual"].(map[string]any)["id"].(float64))
	code, _ = do(t, "DELETE", fmt.Sprintf("%s/api/v1/manuals/%d", ts.URL, mid), "")
	if code != http.StatusOK {
		t.Fatalf("delete manual: %d", code)
	}
	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid), "")
	if got := len(body["manuals"].([]any)); got != 0 {
		t.Fatalf("manuals after delete = %d, want 0", got)
	}
}

func TestFlagManualBlockOverAPI(t *testing.T) {
	ts := newTestServer(t)
	oid := mustCreate(t, ts, "ORD-FLAG")
	pid := mustCreateProduct(t, ts.URL, "PROD-400", "Laminator")
	mustAddBlock(t, ts.URL, pid, "Warm up", "")
	b2 := mustAddBlock(t, ts.URL, pid, "Feed pouch", "")

	code, _ := do(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"wkr","password":"password123","displayName":"Worker","role":"operator"}`)
	if code != http.StatusCreated {
		t.Fatalf("create operator: %d", code)
	}
	wkrTk := loginAs(t, ts.URL, "wkr", "password123")
	code, body := doAs(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid),
		fmt.Sprintf(`{"productId":%d}`, pid), wkrTk)
	if code != http.StatusCreated {
		t.Fatalf("instantiate: %d %v", code, body)
	}
	manual := body["manual"].(map[string]any)
	mid := int64(manual["id"].(float64))
	blocks := manual["blocks"].([]any)
	b1 := int64(blocks[0].(map[string]any)["id"].(float64))
	for _, bid := range []int64{b1, b2} {
		code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/answer", ts.URL, mid, bid),
			`{"answer":"YES"}`, wkrTk)
		if code != http.StatusOK {
			t.Fatalf("answer YES: %d %v", code, body)
		}
	}

	code, _ = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/flag", ts.URL, mid, b1),
		`{"reason":"seal was never checked"}`, wkrTk)
	if code != http.StatusForbidden {
		t.Errorf("operator flag = %d, want 403", code)
	}

	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/flag", ts.URL, mid, b1), `{}`)
	if code != http.StatusBadRequest {
		t.Errorf("flag without reason = %d, want 400 (%v)", code, body)
	}

	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/flag", ts.URL, mid, b1),
		`{"reason":"seal was never checked"}`)
	if code != http.StatusOK {
		t.Fatalf("flag: %d %v", code, body)
	}
	fb := body["block"].(map[string]any)
	if fb["flagged"] != true || fb["flaggedBy"] != "admin" || fb["flagReason"] != "seal was never checked" {
		t.Fatalf("flag not recorded: %v", fb)
	}
	if fb["flaggedAt"] == nil {
		t.Fatalf("flaggedAt missing: %v", fb)
	}

	code, body = doAs(t, "GET", ts.URL+"/api/v1/notifications", "", wkrTk)
	if code != http.StatusOK {
		t.Fatalf("notifications: %d", code)
	}
	notes := body["notifications"].([]any)
	if len(notes) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notes))
	}
	n := notes[0].(map[string]any)
	if n["kind"] != "flag" || n["orderId"].(float64) != float64(oid) {
		t.Fatalf("notification wrong: %v", n)
	}
	if !strings.Contains(n["body"].(string), "seal was never checked") {
		t.Fatalf("notification body missing reason: %v", n)
	}
	if body["unread"].(float64) != 1 {
		t.Fatalf("unread = %v, want 1", body["unread"])
	}

	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid), "")
	m := body["manuals"].([]any)[0].(map[string]any)
	if int(m["flagged"].(float64)) != 1 {
		t.Fatalf("manual flagged count = %v, want 1", m["flagged"])
	}
	hasFlag := false
	for _, e := range m["log"].([]any) {
		if e.(map[string]any)["action"] == "FLAG" {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Fatalf("tick log has no FLAG entry: %v", m["log"])
	}

	oid2 := mustCreate(t, ts, "ORD-FLAG2")
	pid2 := mustCreateProduct(t, ts.URL, "PROD-401", "Shredder")
	mustAddBlock(t, ts.URL, pid2, "Open tray", "")
	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid2),
		fmt.Sprintf(`{"productId":%d}`, pid2))
	if code != http.StatusCreated {
		t.Fatalf("instantiate 2: %d %v", code, body)
	}
	mid2 := int64(body["manual"].(map[string]any)["id"].(float64))
	ub := int64(body["manual"].(map[string]any)["blocks"].([]any)[0].(map[string]any)["id"].(float64))
	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/flag", ts.URL, mid2, ub),
		`{"reason":"too soon"}`)
	if code != http.StatusConflict {
		t.Errorf("flag unanswered = %d, want 409", code)
	}

	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/answer", ts.URL, mid, b1),
		`{"answer":"YES"}`, wkrTk)
	if code != http.StatusOK {
		t.Fatalf("re-answer: %d %v", code, body)
	}
	if rb := body["block"].(map[string]any); rb["flagged"] == true {
		t.Fatalf("re-answered block still flagged: %v", rb)
	}

	code, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/flag", ts.URL, mid, b2),
		`{"reason":"wrong pouch size"}`)
	if code != http.StatusOK {
		t.Fatalf("flag 2: %d", code)
	}
	code, body = do(t, "POST", fmt.Sprintf("%s/api/v1/manuals/%d/blocks/%d/flag?clear=true", ts.URL, mid, b2), `{}`)
	if code != http.StatusOK {
		t.Fatalf("unflag: %d %v", code, body)
	}
	if ub := body["block"].(map[string]any); ub["flagged"] == true {
		t.Fatalf("block still flagged after clear: %v", ub)
	}
	code, body = do(t, "GET", fmt.Sprintf("%s/api/v1/orders/%d/manuals", ts.URL, oid), "")
	m = body["manuals"].([]any)[0].(map[string]any)
	if int(m["flagged"].(float64)) != 0 {
		t.Fatalf("flagged count after unflag = %v, want 0", m["flagged"])
	}
	actions := map[string]bool{}
	for _, e := range m["log"].([]any) {
		actions[e.(map[string]any)["action"].(string)] = true
	}
	if !actions["FLAG"] || !actions["UNFLAG"] {
		t.Fatalf("tick log missing FLAG/UNFLAG history: %v", actions)
	}
}
