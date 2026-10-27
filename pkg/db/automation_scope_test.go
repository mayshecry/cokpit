package db

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cockpit/pkg/automation"
	"cockpit/pkg/order"
)

// teamOf creates a department and returns its id.
func teamOf(t *testing.T, s *Store, name string, members ...string) int64 {
	t.Helper()
	ctx := context.Background()
	dep, err := s.CreateDepartment(ctx, name, "", fixedNow())
	if err != nil {
		t.Fatalf("create department %s: %v", name, err)
	}
	for _, username := range members {
		createAutomationUser(t, s, username)
		var id int64
		if err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
			t.Fatalf("lookup user %s: %v", username, err)
		}
		if err := s.AddUserToDepartment(ctx, id, dep.ID); err != nil {
			t.Fatalf("add %s to %s: %v", username, name, err)
		}
	}
	return dep.ID
}

// orderForCustomer creates an order and records its customer name.
func orderForCustomer(t *testing.T, s *Store, number, customer string, target time.Time) order.Order {
	t.Helper()
	ctx := context.Background()
	o, err := s.CreateOrder(ctx, number, target, fixedNow(), "tester")
	if err != nil {
		t.Fatalf("create order %s: %v", number, err)
	}
	if customer != "" {
		if _, err := s.UpdateOmnitrackerInfo(ctx, o.ID, order.OmnitrackerInfoRequest{CustomerName: customer}, fixedNow()); err != nil {
			t.Fatalf("set customer %s: %v", number, err)
		}
	}
	return o
}

func TestAutomationCustomerScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "alice")
	kenter := orderForCustomer(t, s, "SC-1", "Kenter", now.Add(-2*time.Hour))
	dela := orderForCustomer(t, s, "SC-2", "Dela", now.Add(-3*time.Hour))
	internal := orderForCustomer(t, s, "SC-3", "Kenter Internal", now.Add(-4*time.Hour))

	rule, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Kenter only", Trigger: automation.TriggerSLABreach, Action: automation.ActionNotify,
		Targets: []string{"alice"}, Customers: []string{"Kenter"}, Enabled: true,
	}, "admin", now)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	_, err = s.EvaluateAutomations(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	runs, err := s.ListAutomationRuns(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, run := range runs {
		if run.RuleID == rule.ID {
			got[run.OrderID] = true
		}
	}
	if !got[kenter.ID] {
		t.Error("Kenter order should have matched the Kenter-scoped rule")
	}
	if got[dela.ID] {
		t.Error("Dela order must not match a Kenter-scoped rule")
	}
	if got[internal.ID] {
		t.Error("a different customer must not match a Kenter-scoped rule")
	}
}

func TestAutomationExcludedCustomerWins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "alice")
	included := orderForCustomer(t, s, "EX-1", "Kenter", now.Add(-2*time.Hour))
	excluded := orderForCustomer(t, s, "EX-2", "Dela", now.Add(-2*time.Hour))

	rule, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "All but Dela", Trigger: automation.TriggerSLABreach, Action: automation.ActionNotify,
		Targets: []string{"alice"}, Customers: []string{"Kenter"}, ExcludedCustomers: []string{"Dela"},
		Enabled: true,
	}, "admin", now)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := s.EvaluateAutomations(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	runs, _ := s.ListAutomationRuns(ctx, 50)
	for _, run := range runs {
		if run.RuleID != rule.ID {
			continue
		}
		if run.OrderID == excluded.ID {
			t.Error("an excluded customer must never be acted on")
		}
		if run.OrderID == included.ID {
			return
		}
	}
	t.Fatal("the included customer should have produced a run")
}

func TestAutomationPerCustomerTargets(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "alice")
	createAutomationUser(t, s, "bob")
	kenter := orderForCustomer(t, s, "RT-1", "Kenter", now.Add(-2*time.Hour))
	dela := orderForCustomer(t, s, "RT-2", "Dela", now.Add(-2*time.Hour))

	if _, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Route per customer", Trigger: automation.TriggerSLABreach, Action: automation.ActionNotify,
		Targets:         []string{"alice"},
		CustomerTargets: map[string][]string{"Kenter": {"bob"}},
		Enabled:         true,
	}, "admin", now); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := s.EvaluateAutomations(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if notes, _, err := s.Notifications(ctx, "bob", false, 10); err != nil || len(notes) != 1 || notes[0].OrderID != kenter.ID {
		t.Fatalf("bob notifications = %+v, %v; want the Kenter order", notes, err)
	}
	if notes, _, err := s.Notifications(ctx, "alice", false, 10); err != nil || len(notes) != 1 || notes[0].OrderID != dela.ID {
		t.Fatalf("alice notifications = %+v, %v; want the Dela order", notes, err)
	}
}

func TestAutomationNotifyTeamReachesEveryMember(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	teamOf(t, s, "Warehouse", "wout", "willem")
	orderForCustomer(t, s, "TM-1", "Kenter", now.Add(-2*time.Hour))

	if _, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Notify warehouse", Trigger: automation.TriggerSLABreach, Action: automation.ActionNotify,
		Teams: []string{"Warehouse"}, Enabled: true,
	}, "admin", now); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := s.EvaluateAutomations(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	for _, username := range []string{"wout", "willem"} {
		if notes, _, err := s.Notifications(ctx, username, false, 10); err != nil || len(notes) != 1 {
			t.Errorf("%s notifications = %+v, %v; want one", username, notes, err)
		}
	}
}

func TestAutomationAssignTeamPicksLeastBusy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	teamOf(t, s, "Field", "finn", "fleur")
	// finn already owns two open orders, fleur none.
	for i, number := range []string{"BUSY-1", "BUSY-2"} {
		o, err := s.CreateOrder(ctx, number, now.Add(24*time.Hour), now, "tester")
		if err != nil {
			t.Fatalf("create %s: %v", number, err)
		}
		if _, err := s.SetAssignee(ctx, o.ID, "finn", "tester", now); err != nil {
			t.Fatalf("assign %s: %v", number, err)
		}
		_ = i
	}
	late := orderForCustomer(t, s, "FIELD-1", "Kenter", now.Add(-2*time.Hour))

	if _, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Assign field team", Trigger: automation.TriggerSLABreach, Action: automation.ActionAssign,
		Teams: []string{"Field"}, Enabled: true,
	}, "admin", now); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := s.EvaluateAutomations(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	got, err := s.GetOrder(ctx, late.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assignee != "fleur" {
		t.Fatalf("assignee = %q, want fleur (the least busy member)", got.Assignee)
	}
}

func TestAutomationAssignTeamWithoutMembersFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	teamOf(t, s, "Empty")
	orderForCustomer(t, s, "EMPTY-1", "Kenter", now.Add(-2*time.Hour))

	if _, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Assign to empty team", Trigger: automation.TriggerSLABreach, Action: automation.ActionAssign,
		Teams: []string{"Empty"}, Enabled: true,
	}, "admin", now); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := s.EvaluateAutomations(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	runs, _ := s.ListAutomationRuns(ctx, 10)
	if len(runs) != 1 {
		t.Fatalf("runs = %+v, want one", runs)
	}
	if runs[0].Status != automation.RunFailed {
		t.Fatalf("status = %s, want FAILED", runs[0].Status)
	}
	if !strings.Contains(runs[0].Error, "no members") {
		t.Errorf("error = %q, want it to explain the empty team", runs[0].Error)
	}
}

func TestAutomationUnknownTeamIsRejected(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateAutomationRule(context.Background(), automation.Rule{
		Name: "Ghost team", Trigger: automation.TriggerSLABreach, Action: automation.ActionNotify,
		Teams: []string{"Nope"}, Enabled: true,
	}, "admin", fixedNow())
	if !errors.Is(err, ErrUnknownDepartment) {
		t.Fatalf("err = %v, want ErrUnknownDepartment", err)
	}
}

func TestAutomationDepartmentAndProductScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "alice")
	product, err := s.CreateProduct(ctx, "PRD-WH", "Warehouse kit", "", "Warehouse", "admin", now)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	other, err := s.CreateProduct(ctx, "PRD-FD", "Field kit", "", "Field", "admin", now)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	warehouseOrder := orderForCustomer(t, s, "DEP-1", "Kenter", now.Add(-2*time.Hour))
	fieldOrder := orderForCustomer(t, s, "DEP-2", "Kenter", now.Add(-2*time.Hour))
	bareOrder := orderForCustomer(t, s, "DEP-3", "Kenter", now.Add(-2*time.Hour))
	if _, err := s.InstantiateManual(ctx, warehouseOrder.ID, product.ID, "tester", now); err != nil {
		t.Fatalf("manual: %v", err)
	}
	if _, err := s.InstantiateManual(ctx, fieldOrder.ID, other.ID, "tester", now); err != nil {
		t.Fatalf("manual: %v", err)
	}

	cases := []struct {
		name       string
		rule       automation.Rule
		wantHit    []int64
		wantNoHit  []int64
		scaleTotal int
	}{
		{
			name:      "department",
			rule:      automation.Rule{Departments: []string{"Warehouse"}},
			wantHit:   []int64{warehouseOrder.ID},
			wantNoHit: []int64{fieldOrder.ID, bareOrder.ID},
		},
		{
			name:      "product",
			rule:      automation.Rule{Products: []string{"PRD-FD"}},
			wantHit:   []int64{fieldOrder.ID},
			wantNoHit: []int64{warehouseOrder.ID, bareOrder.ID},
		},
	}
	for i, c := range cases {
		rule := c.rule
		rule.Name = "scope " + c.name
		rule.Trigger = automation.TriggerSLABreach
		rule.Action = automation.ActionNotify
		rule.Targets = []string{"alice"}
		rule.Enabled = true
		created, err := s.CreateAutomationRule(ctx, rule, "admin", now)
		if err != nil {
			t.Fatalf("%s: create rule: %v", c.name, err)
		}
		_ = i
		if _, err := s.EvaluateAutomations(ctx, now.Add(time.Hour)); err != nil {
			t.Fatalf("%s: evaluate: %v", c.name, err)
		}
		runs, _ := s.ListAutomationRuns(ctx, 100)
		hit := map[int64]bool{}
		for _, run := range runs {
			if run.RuleID == created.ID {
				hit[run.OrderID] = true
			}
		}
		for _, id := range c.wantHit {
			if !hit[id] {
				t.Errorf("%s: order %d should have matched", c.name, id)
			}
		}
		for _, id := range c.wantNoHit {
			if hit[id] {
				t.Errorf("%s: order %d must not have matched", c.name, id)
			}
		}
	}
}

func TestAutomationRuleScopeRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	teamOf(t, s, "Warehouse", "wout")
	createAutomationUser(t, s, "alice")
	created, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Full rule", Trigger: automation.TriggerHoldAging, ThresholdMinutes: 240,
		Action: automation.ActionNotify, Targets: []string{"alice"}, Teams: []string{"Warehouse"},
		Customers: []string{"Kenter", "Dela"}, ExcludedCustomers: []string{"Internal"},
		Departments: []string{"Warehouse"}, Products: []string{"PRD-WH"},
		CustomerTargets: map[string][]string{"Kenter": {"alice"}},
		CustomerTeams:   map[string][]string{"Kenter": {"Warehouse"}},
		Message:         "look at it", Enabled: true,
	}, "admin", now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.AutomationRuleByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Teams) != 1 || got.Teams[0] != "Warehouse" {
		t.Errorf("teams = %v", got.Teams)
	}
	if len(got.Customers) != 2 || len(got.ExcludedCustomers) != 1 {
		t.Errorf("customer scope = %v / %v", got.Customers, got.ExcludedCustomers)
	}
	if len(got.Departments) != 1 || len(got.Products) != 1 {
		t.Errorf("dept/product scope = %v / %v", got.Departments, got.Products)
	}
	if len(got.CustomerTargets["Kenter"]) != 1 || len(got.CustomerTeams["Kenter"]) != 1 {
		t.Errorf("routing = %v / %v", got.CustomerTargets, got.CustomerTeams)
	}
}

func TestAutomationPreview(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "alice")
	teamOf(t, s, "Field", "fleur")
	orderForCustomer(t, s, "PRE-1", "Kenter", now.Add(-2*time.Hour))
	orderForCustomer(t, s, "PRE-2", "Dela", now.Add(-2*time.Hour))

	preview, err := s.PreviewAutomation(ctx, automation.Rule{
		Name: "Preview", Trigger: automation.TriggerSLABreach, Action: automation.ActionNotify,
		Targets: []string{"alice"}, Customers: []string{"Kenter"}, Teams: []string{"Field"}, Enabled: true,
	}, now, 10)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Matched != 1 || len(preview.Candidates) != 1 {
		t.Fatalf("preview = %+v, want exactly the Kenter order", preview)
	}
	row := preview.Candidates[0]
	if row.Customer != "Kenter" {
		t.Errorf("customer = %q", row.Customer)
	}
	if len(row.Recipients) != 2 || row.Recipients[0] != "alice" || row.Recipients[1] != "fleur" {
		t.Errorf("recipients = %v, want alice and fleur", row.Recipients)
	}
	// Nothing was written.
	if runs, _ := s.ListAutomationRuns(ctx, 10); len(runs) != 0 {
		t.Errorf("preview wrote %d runs", len(runs))
	}
}

func TestAutomationPreviewAssignTeam(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	teamOf(t, s, "Field", "finn", "fleur")
	o, err := s.CreateOrder(ctx, "PRE-A", now.Add(24*time.Hour), now, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAssignee(ctx, o.ID, "finn", "tester", now); err != nil {
		t.Fatal(err)
	}
	orderForCustomer(t, s, "PRE-B", "Kenter", now.Add(-2*time.Hour))

	preview, err := s.PreviewAutomation(ctx, automation.Rule{
		Name: "Preview assign", Trigger: automation.TriggerSLABreach, Action: automation.ActionAssign,
		Teams: []string{"Field"}, Enabled: true,
	}, now, 10)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Candidates) != 1 {
		t.Fatalf("candidates = %+v", preview.Candidates)
	}
	if preview.Candidates[0].Assignee != "fleur" {
		t.Errorf("assignee = %q, want fleur", preview.Candidates[0].Assignee)
	}
}

// An existing installation must upgrade in place: the rule-scoping columns are
// added to the existing table and old rules keep applying to everything.
func TestAutomationScopeMigrationOnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")

	raw, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Pretend this database was created before rule scoping existed.
	for _, col := range []string{
		"teams_json", "customers_json", "excluded_customers_json", "departments_json",
		"products_json", "customer_targets_json", "customer_teams_json",
	} {
		if _, err := raw.ExecContext(ctx, `ALTER TABLE automation_rules DROP COLUMN `+col); err != nil {
			t.Fatalf("drop %s: %v", col, err)
		}
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO automation_rules
		(name, trigger_type, threshold_minutes, action_type, targets_json, message,
		 escalation_level, enabled, created_by, created_at, updated_at)
		VALUES ('legacy', 'SLA_BREACHED', 0, 'NOTIFY', '["alice"]', '', 'L1', 1, 'admin', 1, 1)`); err != nil {
		t.Fatalf("insert legacy rule: %v", err)
	}
	raw.Close()

	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen (migration): %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	upgraded := New(conn)

	cols, err := tableColumns(ctx, conn, "automation_rules")
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"teams_json", "customers_json", "excluded_customers_json",
		"departments_json", "products_json", "customer_targets_json", "customer_teams_json"} {
		if !cols[col] {
			t.Errorf("migration did not add column %s", col)
		}
	}

	rules, err := upgraded.ListAutomationRules(ctx, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules = %+v, want the legacy rule", rules)
	}
	legacy := rules[0]
	if len(legacy.Targets) != 1 || legacy.Targets[0] != "alice" {
		t.Errorf("targets = %v", legacy.Targets)
	}
	if len(legacy.Teams) != 0 || len(legacy.Customers) != 0 || len(legacy.ExcludedCustomers) != 0 {
		t.Errorf("upgraded rule should have empty scope, got %+v", legacy)
	}
	if !legacy.MatchesScope(automation.Scope{Customer: "Anyone"}) {
		t.Error("an upgraded rule must keep applying to every customer")
	}
}
