package api

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cockpit/pkg/auth"
)

var allPermissionConstants = []auth.Permission{
	auth.PermOrderList, auth.PermOrderCreate, auth.PermOrderView, auth.PermOrderTransition,
	auth.PermHoldCreate, auth.PermHoldResolve, auth.PermQCSubmit, auth.PermAuditView,
	auth.PermUsersManage, auth.PermUsersList, auth.PermScanUse, auth.PermPickUse,
	auth.PermCommentRead, auth.PermCommentPost, auth.PermNotifyRead,
	auth.PermManualManage, auth.PermManualsApprove, auth.PermCustomersManage,
	auth.PermSIList, auth.PermSIView, auth.PermSICreate, auth.PermSIUpdate,
	auth.PermSITransition, auth.PermSIProjects, auth.PermSIManage,
	auth.PermProcessView, auth.PermProcessAdvance, auth.PermProcessEscalate,
	auth.PermProcessManage, auth.PermIntegrationsManage,
}

func TestPermissionCatalogueIsComplete(t *testing.T) {
	seen := map[auth.Permission]bool{}
	for _, p := range auth.AllPermissions {
		if p == "" {
			t.Fatal("empty permission in catalogue")
		}
		if seen[p] {
			t.Errorf("duplicate permission %q in catalogue", p)
		}
		seen[p] = true
	}
	for _, p := range allPermissionConstants {
		if !auth.IsValidPermission(p) {
			t.Errorf("permission %q is not registered in AllPermissions — it can never be granted", p)
		}
	}
	for _, p := range auth.AllPermissions {
		if !auth.IsValidPermission(p) {
			t.Errorf("catalogue entry %q does not validate", p)
		}
	}
}

func TestRoleMatrixUsesOnlyKnownPermissions(t *testing.T) {
	for role, perms := range auth.RolePermissionMatrix() {
		for _, p := range perms {
			if !auth.IsValidPermission(p) {
				t.Errorf("role %s has unknown permission %q", role, p)
			}
		}
	}
	for _, role := range auth.ValidRoles {
		if _, ok := auth.RolePermissionMatrix()[role]; !ok {
			t.Errorf("role %s missing from matrix", role)
		}
	}
}

func TestAdminRoleCoversEverything(t *testing.T) {
	admin := auth.PermissionStrings(auth.PermissionsFor(auth.RoleAdmin))
	have := map[string]bool{}
	for _, p := range admin {
		have[p] = true
	}
	for _, p := range auth.AllPermissions {
		if !have[string(p)] {
			t.Errorf("admin role is missing %q", p)
		}
	}
	for _, p := range allPermissionConstants {
		if !auth.HasPermission(auth.RoleAdmin, p) {
			t.Errorf("admin HasPermission(%q) = false", p)
		}
	}
}

// Documented role capabilities (README §5.3).
func TestDocumentedRoleCapabilities(t *testing.T) {
	cases := []struct {
		role auth.Role
		perm auth.Permission
		want bool
	}{
		{auth.RoleViewer, auth.PermOrderList, true},
		{auth.RoleViewer, auth.PermOrderCreate, false},
		{auth.RoleViewer, auth.PermProcessView, true},
		{auth.RoleViewer, auth.PermUsersList, false},
		{auth.RoleOperator, auth.PermOrderCreate, true},
		{auth.RoleOperator, auth.PermQCSubmit, false},
		{auth.RoleOperator, auth.PermProcessAdvance, true},
		{auth.RoleOperator, auth.PermUsersManage, false},
		{auth.RoleQC, auth.PermQCSubmit, true},
		{auth.RoleQC, auth.PermManualsApprove, false},
		{auth.RoleQC, auth.PermProcessEscalate, true},
		{auth.RoleNPI, auth.PermSICreate, true},
		{auth.RoleNPI, auth.PermProcessManage, true},
		{auth.RoleNPI, auth.PermIntegrationsManage, true},
		{auth.RoleNPI, auth.PermOrderList, false},
		{auth.RoleSC, auth.PermManualsApprove, true},
		{auth.RoleSC, auth.PermCustomersManage, true},
		{auth.RoleSC, auth.PermUsersList, true},
		{auth.RoleSC, auth.PermUsersManage, false},
		{auth.RoleAdmin, auth.PermUsersManage, true},
		{auth.RoleAdmin, auth.PermProcessManage, true},
	}
	for _, c := range cases {
		if got := auth.HasPermission(c.role, c.perm); got != c.want {
			t.Errorf("HasPermission(%s, %s) = %v, want %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestPermissionCatalogueEndpoint(t *testing.T) {
	ts := newTestServer(t)

	code, body := doAs(t, "GET", ts.URL+"/api/v1/auth/permissions", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("anonymous catalogue = %d, want 401", code)
	}

	tk := loginAs(t, ts.URL, "admin", "secret123")
	code, body = doAs(t, "GET", ts.URL+"/api/v1/auth/permissions", "", tk)
	if code != http.StatusOK {
		t.Fatalf("catalogue = %d %v", code, body)
	}
	perms, _ := body["permissions"].([]any)
	if len(perms) != len(auth.AllPermissions) {
		t.Errorf("catalogue has %d permissions, want %d", len(perms), len(auth.AllPermissions))
	}
	roles, _ := body["roles"].(map[string]any)
	for _, role := range auth.ValidRoles {
		if _, ok := roles[string(role)]; !ok {
			t.Errorf("catalogue is missing role %q", role)
		}
	}
	adminPerms, _ := roles[string(auth.RoleAdmin)].([]any)
	if len(adminPerms) != len(auth.AllPermissions) {
		t.Errorf("admin row has %d permissions, want the full catalogue (%d)", len(adminPerms), len(auth.AllPermissions))
	}
}

func createRoleUser(t *testing.T, ts *httptest.Server, adminToken, username, role string) int64 {
	t.Helper()
	code, body := doAs(t, "POST", ts.URL+"/api/v1/users",
		fmt.Sprintf(`{"username":%q,"password":"password123","displayName":%q,"role":%q}`, username, username, role), adminToken)
	if code != http.StatusCreated {
		t.Fatalf("create %s: %d %v", username, code, body)
	}
	return int64(body["user"].(map[string]any)["id"].(float64))
}

func createDepartment(t *testing.T, ts *httptest.Server, adminToken, name string) int64 {
	t.Helper()
	code, body := doAs(t, "POST", ts.URL+"/api/v1/departments",
		fmt.Sprintf(`{"name":%q,"description":"test"}`, name), adminToken)
	if code != http.StatusCreated {
		t.Fatalf("create department %s: %d %v", name, code, body)
	}
	return int64(body["department"].(map[string]any)["id"].(float64))
}

func setDepartmentPerms(t *testing.T, ts *httptest.Server, adminToken string, deptID int64, perms string) (int, map[string]any) {
	t.Helper()
	return doAs(t, "POST", fmt.Sprintf("%s/api/v1/departments/%d/permissions", ts.URL, deptID),
		`{"permissions":`+perms+`}`, adminToken)
}

func addUserToDepartment(t *testing.T, ts *httptest.Server, adminToken string, deptID, userID int64) (int, map[string]any) {
	t.Helper()
	return doAs(t, "POST", fmt.Sprintf("%s/api/v1/departments/%d/users", ts.URL, deptID),
		fmt.Sprintf(`{"userId":%d}`, userID), adminToken)
}

func TestDepartmentGrantsAreAdditiveAndEffective(t *testing.T) {
	ts := newTestServer(t)
	aliceID := createRoleUser(t, ts, testAuthToken, "alice", "viewer")
	deptID := createDepartment(t, ts, testAuthToken, "Production")

	code, body := addUserToDepartment(t, ts, testAuthToken, deptID, aliceID)
	if code != http.StatusOK {
		t.Fatalf("add member: %d %v", code, body)
	}

	aliceTk := loginAs(t, ts.URL, "alice", "password123")
	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/qc/queue", "", aliceTk); code != http.StatusForbidden {
		t.Errorf("viewer without grant reached qc queue: %d, want 403", code)
	}

	if code, body := setDepartmentPerms(t, ts, testAuthToken, deptID, `["qc:submit"]`); code != http.StatusOK {
		t.Fatalf("set perms: %d %v", code, body)
	}

	// The grant must apply to the already issued session: permissions are
	// re-read on every request, no re-login needed.
	if code, body := doAs(t, "GET", ts.URL+"/api/v1/qc/queue", "", aliceTk); code != http.StatusOK {
		t.Errorf("viewer with qc:submit grant = %d %v, want 200", code, body)
	}
	// Additive: the role permission is still there too.
	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/orders", "", aliceTk); code != http.StatusOK {
		t.Errorf("viewer lost orders:list after grant: %d", code)
	}
	// ...and the grant cannot subtract: a role permission must not vanish.
	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/orders/1/audit", "", aliceTk); code == http.StatusForbidden {
		t.Error("viewer lost audit:view after grant")
	}

	code, body = doAs(t, "GET", ts.URL+"/api/v1/auth/me", "", aliceTk)
	if code != http.StatusOK {
		t.Fatalf("me: %d", code)
	}
	user := body["user"].(map[string]any)
	granted, _ := user["permissions"].([]any)
	found := false
	for _, p := range granted {
		if p == "qc:submit" {
			found = true
		}
	}
	if !found {
		t.Errorf("/auth/me does not report the department grant: %v", granted)
	}

	if code, body := setDepartmentPerms(t, ts, testAuthToken, deptID, `[]`); code != http.StatusOK {
		t.Fatalf("clear perms: %d %v", code, body)
	}
	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/qc/queue", "", aliceTk); code != http.StatusForbidden {
		t.Errorf("revoked grant still active: %d, want 403", code)
	}
}

func TestDepartmentCanGrantApprovalAndCustomerPermissions(t *testing.T) {
	ts := newTestServer(t)
	deptID := createDepartment(t, ts, testAuthToken, "Sales")

	code, body := setDepartmentPerms(t, ts, testAuthToken, deptID, `["manuals:approve","customers:manage"]`)
	if code != http.StatusOK {
		t.Fatalf("set perms = %d %v", code, body)
	}
	code, body = doAs(t, "GET", ts.URL+"/api/v1/departments", "", testAuthToken)
	if code != http.StatusOK {
		t.Fatalf("list departments = %d", code)
	}
	stored := map[string]bool{}
	for _, raw := range body["departments"].([]any) {
		d := raw.(map[string]any)
		if int64(d["id"].(float64)) != deptID {
			continue
		}
		for _, p := range d["permissions"].([]any) {
			stored[p.(string)] = true
		}
	}
	if !stored["manuals:approve"] || !stored["customers:manage"] {
		t.Fatalf("approval permissions were not persisted: %v", stored)
	}
}

func TestDepartmentRejectsUnknownPermission(t *testing.T) {
	ts := newTestServer(t)
	deptID := createDepartment(t, ts, testAuthToken, "Bogus")

	code, body := setDepartmentPerms(t, ts, testAuthToken, deptID, `["orders:list","config:manage"]`)
	if code != http.StatusBadRequest {
		t.Fatalf("unknown permission = %d %v, want 400", code, body)
	}
	if body["error"].(map[string]any)["code"] != "invalid_permission" {
		t.Errorf("error code = %v, want invalid_permission", body["error"])
	}
	code, body = doAs(t, "GET", ts.URL+"/api/v1/departments", "", testAuthToken)
	if code != http.StatusOK {
		t.Fatalf("list departments = %d", code)
	}
	for _, raw := range body["departments"].([]any) {
		d := raw.(map[string]any)
		if int64(d["id"].(float64)) != deptID {
			continue
		}
		if perms, _ := d["permissions"].([]any); len(perms) != 0 {
			t.Errorf("rejected request still changed permissions: %v", perms)
		}
	}
}

func TestDepartmentMembershipValidatesIDs(t *testing.T) {
	ts := newTestServer(t)
	deptID := createDepartment(t, ts, testAuthToken, "Ghosts")
	if code, _ := addUserToDepartment(t, ts, testAuthToken, deptID, 999999); code != http.StatusNotFound {
		t.Errorf("unknown user = %d, want 404", code)
	}
	aliceID := createRoleUser(t, ts, testAuthToken, "alice", "viewer")
	if code, _ := addUserToDepartment(t, ts, testAuthToken, 999999, aliceID); code != http.StatusNotFound {
		t.Errorf("unknown department = %d, want 404", code)
	}
}

func TestDepartmentGrantsCannotEscalateToAdmin(t *testing.T) {
	ts := newTestServer(t)
	adminID := int64(0)
	code, body := doAs(t, "GET", ts.URL+"/api/v1/auth/me", "", testAuthToken)
	if code != http.StatusOK {
		t.Fatalf("me: %d", code)
	}
	adminID = int64(body["user"].(map[string]any)["id"].(float64))

	bobID := createRoleUser(t, ts, testAuthToken, "bob", "operator")
	victimID := createRoleUser(t, ts, testAuthToken, "victim", "viewer")
	deptID := createDepartment(t, ts, testAuthToken, "Ops desk")
	if code, body := setDepartmentPerms(t, ts, testAuthToken, deptID, `["users:manage"]`); code != http.StatusOK {
		t.Fatalf("grant users:manage: %d %v", code, body)
	}
	if code, body := addUserToDepartment(t, ts, testAuthToken, deptID, bobID); code != http.StatusOK {
		t.Fatalf("add bob: %d %v", code, body)
	}

	bobTk := loginAs(t, ts.URL, "bob", "password123")

	// A department-granted users:manage really is users:manage...
	if code, body := doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"delegate-first","password":"password123","role":"viewer"}`, bobTk); code != http.StatusCreated {
		t.Fatalf("delegated user management = %d %v, want 201", code, body)
	}
	// ...and the plain users:list permission was not handed out as a side effect.
	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/users", "", bobTk); code != http.StatusForbidden {
		t.Errorf("users:manage leaked users:list: %d, want 403", code)
	}
	// ...but it must never touch administrator accounts.
	cases := []struct {
		name   string
		method string
		url    string
		body   string
	}{
		{"create admin", "POST", ts.URL + "/api/v1/users",
			`{"username":"sneaky","password":"password123","role":"admin"}`},
		{"promote self", "POST", fmt.Sprintf("%s/api/v1/users/%d/role", ts.URL, bobID), `{"role":"admin"}`},
		{"demote admin", "POST", fmt.Sprintf("%s/api/v1/users/%d/role", ts.URL, adminID), `{"role":"viewer"}`},
		{"reset admin password", "POST", fmt.Sprintf("%s/api/v1/users/%d/password", ts.URL, adminID), `{"password":"newpassword123"}`},
		{"delete admin", "DELETE", fmt.Sprintf("%s/api/v1/users/%d", ts.URL, adminID), ""},
	}
	for _, c := range cases {
		if code, body := doAs(t, c.method, c.url, c.body, bobTk); code != http.StatusForbidden {
			t.Errorf("%s: status = %d (%v), want 403", c.name, code, body)
		}
	}

	// Ordinary accounts stay manageable for the delegate.
	if code, body := doAs(t, "POST", ts.URL+"/api/v1/users",
		`{"username":"newbie","password":"password123","role":"viewer"}`, bobTk); code != http.StatusCreated {
		t.Errorf("delegate cannot create a viewer: %d %v", code, body)
	}
	if code, _ := doAs(t, "POST", fmt.Sprintf("%s/api/v1/users/%d/password", ts.URL, victimID),
		`{"password":"newpassword123"}`, bobTk); code != http.StatusOK {
		t.Errorf("delegate cannot reset a viewer password: %d", code)
	}
}

func TestLastAdministratorIsProtected(t *testing.T) {
	ts := newTestServer(t)
	code, body := doAs(t, "GET", ts.URL+"/api/v1/auth/me", "", testAuthToken)
	if code != http.StatusOK {
		t.Fatalf("me: %d", code)
	}
	adminID := int64(body["user"].(map[string]any)["id"].(float64))
	secondAdminID := createRoleUser(t, ts, testAuthToken, "root2", "admin")

	// Two admins: demoting one is fine.
	if code, body := doAs(t, "POST", fmt.Sprintf("%s/api/v1/users/%d/role", ts.URL, secondAdminID),
		`{"role":"sc"}`, testAuthToken); code != http.StatusOK {
		t.Fatalf("demote second admin = %d %v", code, body)
	}
	// One admin left: it can no longer demote itself.
	code, body = doAs(t, "POST", fmt.Sprintf("%s/api/v1/users/%d/role", ts.URL, adminID),
		`{"role":"viewer"}`, testAuthToken)
	if code != http.StatusConflict {
		t.Fatalf("demote last admin = %d %v, want 409", code, body)
	}
	if body["error"].(map[string]any)["code"] != "last_admin" {
		t.Errorf("error code = %v, want last_admin", body["error"])
	}

	// And it cannot be deleted by another administrator either.
	promoteAgain := createRoleUser(t, ts, testAuthToken, "root3", "admin")
	root3Tk := loginAs(t, ts.URL, "root3", "password123")
	if code, _ := doAs(t, "DELETE", fmt.Sprintf("%s/api/v1/users/%d", ts.URL, adminID), "", root3Tk); code != http.StatusOK {
		t.Errorf("delete admin with two admins = %d, want 200", code)
	}
	if code, _ := doAs(t, "DELETE", fmt.Sprintf("%s/api/v1/users/%d", ts.URL, promoteAgain), "", root3Tk); code != http.StatusConflict {
		t.Errorf("delete self = %d, want 409", code)
	}
}

func TestAdminPairCodeCannotTargetAdministrators(t *testing.T) {
	ts := newTestServerWithGuard(t)
	bobID := createRoleUser(t, ts, testAuthToken, "bob", "operator")
	deptID := createDepartment(t, ts, testAuthToken, "Support")
	if code, body := setDepartmentPerms(t, ts, testAuthToken, deptID, `["users:manage"]`); code != http.StatusOK {
		t.Fatalf("grant users:manage: %d %v", code, body)
	}
	if code, body := addUserToDepartment(t, ts, testAuthToken, deptID, bobID); code != http.StatusOK {
		t.Fatalf("add bob: %d %v", code, body)
	}
	bobTk := loginAs(t, ts.URL, "bob", "password123")

	code, body := doAs(t, "POST", ts.URL+"/api/v1/admin/pair-code", `{"username":"admin"}`, bobTk)
	if code != http.StatusForbidden {
		t.Errorf("pair code for admin = %d %v, want 403", code, body)
	}
	if code, body := doAs(t, "GET", ts.URL+"/api/v1/admin/pair-qr?username=admin", "", bobTk); code != http.StatusForbidden {
		t.Errorf("pair QR for admin = %d %v, want 403", code, body)
	}
	// Pairing an ordinary account is still allowed for the delegate.
	if code, body := doAs(t, "POST", ts.URL+"/api/v1/admin/pair-code", `{"username":"bob"}`, bobTk); code != http.StatusOK {
		t.Errorf("pair code for own role = %d %v, want 200", code, body)
	}
}

// TestUserDepartmentPermissionsRequireGrant ensures department listings cannot
// be used as a back door: only users:list/users:manage holders see them.
func TestDepartmentRoutesNeedPermissions(t *testing.T) {
	ts := newTestServer(t)
	aliceID := createRoleUser(t, ts, testAuthToken, "alice", "operator")
	deptID := createDepartment(t, ts, testAuthToken, "Warehouse")
	aliceTk := loginAs(t, ts.URL, "alice", "password123")

	if code, _ := doAs(t, "GET", ts.URL+"/api/v1/departments", "", aliceTk); code != http.StatusForbidden {
		t.Errorf("operator list departments = %d, want 403", code)
	}
	if code, _ := doAs(t, "POST", ts.URL+"/api/v1/departments", `{"name":"Nope"}`, aliceTk); code != http.StatusForbidden {
		t.Errorf("operator create department = %d, want 403", code)
	}
	if code, _ := setDepartmentPerms(t, ts, aliceTk, deptID, `["users:manage"]`); code != http.StatusForbidden {
		t.Errorf("operator set department perms = %d, want 403", code)
	}
	if code, _ := addUserToDepartment(t, ts, aliceTk, deptID, aliceID); code != http.StatusForbidden {
		t.Errorf("operator self-join department = %d, want 403", code)
	}
	if code, _ := doAs(t, "GET", fmt.Sprintf("%s/api/v1/users/%d/departments", ts.URL, aliceID), "", aliceTk); code != http.StatusForbidden {
		t.Errorf("operator read own departments = %d, want 403", code)
	}
}

// TestFrontendRoleMapMatchesServer keeps frontend/js/core.js from drifting away
// from pkg/auth (the browser copy is a rendering aid, but a stale copy hides
// buttons the user is actually allowed to press).
func TestFrontendRoleMapMatchesServer(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "js", "core.js"))
	if err != nil {
		t.Skipf("frontend not available: %v", err)
	}
	fe, err := parseFrontendRolePerms(string(raw))
	if err != nil {
		t.Fatalf("parse frontend role table: %v", err)
	}
	srv := auth.RolePermissionMatrix()
	for _, role := range auth.ValidRoles {
		want := map[string]bool{}
		for _, p := range srv[role] {
			want[string(p)] = true
		}
		got := map[string]bool{}
		for _, p := range fe[string(role)] {
			got[p] = true
		}
		for p := range want {
			if !got[p] {
				t.Errorf("frontend role %s is missing %s", role, p)
			}
		}
		for p := range got {
			if !want[p] {
				t.Errorf("frontend role %s has stale permission %s (not in the server matrix)", role, p)
			}
		}
	}
}

func newTestServerWithGuard(t *testing.T) *httptest.Server {
	t.Helper()
	guard := NewClientGuard(true, filepath.Join(t.TempDir(), "client-keys.json"), log.New(os.Stderr, "[guard] ", 0))
	if err := guard.Init(); err != nil {
		t.Fatalf("guard init: %v", err)
	}
	return newTestServerConfig(t, ServerConfig{Now: fixedClock, ClientGuard: guard})
}

var feRoleLine = regexp.MustCompile(`(?m)^\s*(viewer|operator|qc|npi|sc|admin)\s*:\s*\[([^\]]*)\]`)
var fePermLiteral = regexp.MustCompile(`'([a-z]+:[a-z]+)'`)

// parseFrontendRolePerms extracts the fallback role -> permission table from
// frontend/js/core.js.
func parseFrontendRolePerms(src string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, m := range feRoleLine.FindAllStringSubmatch(src, -1) {
		role := m[1]
		if _, seen := out[role]; seen {
			continue
		}
		var perms []string
		for _, p := range fePermLiteral.FindAllStringSubmatch(m[2], -1) {
			perms = append(perms, p[1])
		}
		if len(perms) == 0 {
			return nil, fmt.Errorf("role %q parsed with no permissions", role)
		}
		out[role] = perms
	}
	if len(out) != len(auth.ValidRoles) {
		return nil, fmt.Errorf("parsed %d roles, want %d (%s)", len(out), len(auth.ValidRoles), strings.Join(mapKeys(out), ","))
	}
	return out, nil
}

func mapKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
