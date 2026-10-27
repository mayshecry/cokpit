package automation

import (
	"errors"
	"strings"
	"testing"
)

func baseRule() Rule {
	return Rule{
		Name:    "Blow the whistle",
		Trigger: TriggerSLABreach,
		Action:  ActionNotify,
		Targets: []string{"alice"},
		Enabled: true,
	}
}

func TestValidateAssignRecipients(t *testing.T) {
	cases := []struct {
		name    string
		rule    Rule
		wantErr string
	}{
		{
			name: "one person is fine",
			rule: func() Rule { r := baseRule(); r.Action = ActionAssign; return r }(),
		},
		{
			name: "one team is fine",
			rule: func() Rule {
				r := baseRule()
				r.Action = ActionAssign
				r.Targets = nil
				r.Teams = []string{"Warehouse"}
				return r
			}(),
		},
		{
			name: "three people are rejected with an explanation",
			rule: func() Rule {
				r := baseRule()
				r.Action = ActionAssign
				r.Targets = []string{"alice", "bob", "carol"}
				return r
			}(),
			wantErr: "exactly one person or team",
		},
		{
			name: "a person plus a team is rejected",
			rule: func() Rule {
				r := baseRule()
				r.Action = ActionAssign
				r.Teams = []string{"Warehouse"}
				return r
			}(),
			wantErr: "exactly one person or team",
		},
		{
			name:    "notify needs somebody",
			rule:    func() Rule { r := baseRule(); r.Targets = nil; return r }(),
			wantErr: "NOTIFY needs at least one person or team",
		},
		{
			name: "escalate accepts a team",
			rule: func() Rule {
				r := baseRule()
				r.Action = ActionEscalate
				r.EscalationLevel = "L2"
				r.Targets = nil
				r.Teams = []string{"Operations"}
				return r
			}(),
		},
		{
			name: "escalate rejects two recipients",
			rule: func() Rule {
				r := baseRule()
				r.Action = ActionEscalate
				r.EscalationLevel = "L2"
				r.Targets = []string{"alice", "bob"}
				return r
			}(),
			wantErr: "at most one person or team",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.rule)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", c.wantErr)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("error does not wrap ErrValidation: %v", err)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.wantErr)
			}
		})
	}
}

// The historical message was a bare "ASSIGN needs exactly one target user",
// which did not say what to do about it. The replacement must explain itself.
func TestAssignErrorIsActionable(t *testing.T) {
	r := baseRule()
	r.Action = ActionAssign
	r.Targets = []string{"alice", "bob"}
	err := Validate(r)
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	for _, want := range []string{"ASSIGN", "2 people", "single team", "fewest open orders", "NOTIFY"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
}

func TestValidateDuplicatesIgnoreCase(t *testing.T) {
	r := baseRule()
	r.Targets = []string{"Alice", "alice"}
	if err := Validate(r); err == nil {
		t.Fatal("case-insensitive duplicate target must be rejected")
	}
	r = baseRule()
	r.Targets = []string{"alice"}
	r.Teams = []string{"Warehouse", "warehouse"}
	if err := Validate(r); err == nil {
		t.Fatal("case-insensitive duplicate team must be rejected")
	}
}

func TestValidateCustomerScope(t *testing.T) {
	r := baseRule()
	r.Customers = []string{"Kenter"}
	r.ExcludedCustomers = []string{"kenter"}
	err := Validate(r)
	if err == nil {
		t.Fatal("a customer that is both included and excluded must be rejected")
	}
	if !strings.Contains(err.Error(), "both included and excluded") {
		t.Errorf("error = %q", err.Error())
	}

	r = baseRule()
	r.Customers = []string{"Kenter"}
	r.CustomerTargets = map[string][]string{"Dela": {"bob"}}
	if err := Validate(r); err == nil || !strings.Contains(err.Error(), "pointless") {
		t.Fatalf("routing for a customer outside the include list must be rejected, got %v", err)
	}

	r = baseRule()
	r.Customers = []string{"Kenter"}
	r.ExcludedCustomers = []string{"Dela"}
	if err := Validate(r); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}

	r = baseRule()
	r.Customers = []string{"", "Kenter"}
	if err := Validate(r); err == nil {
		t.Fatal("empty scope entries must be rejected")
	}
}

func TestValidatePerCustomerRoutingRespectsAction(t *testing.T) {
	r := baseRule()
	r.Action = ActionAssign
	r.Targets = []string{"alice"}
	r.CustomerTargets = map[string][]string{"Kenter": {"bob", "carol"}}
	err := Validate(r)
	if err == nil || !strings.Contains(err.Error(), "for customer Kenter") {
		t.Fatalf("assign routing to two people must be rejected with context, got %v", err)
	}

	r.CustomerTargets = map[string][]string{"Kenter": {"bob"}}
	if err := Validate(r); err != nil {
		t.Fatalf("single-person routing rejected: %v", err)
	}

	r.CustomerTeams = map[string][]string{"Kenter": {"Warehouse"}}
	if err := Validate(r); err == nil {
		t.Fatal("person + team for one customer must be rejected")
	}
}

func TestMatchesScope(t *testing.T) {
	cases := []struct {
		name  string
		rule  Rule
		scope Scope
		want  bool
	}{
		{"no scope matches everything", baseRule(), Scope{Customer: "Kenter"}, true},
		{"include list matches", func() Rule { r := baseRule(); r.Customers = []string{"Kenter"}; return r }(),
			Scope{Customer: "kenter"}, true},
		{"include list miss", func() Rule { r := baseRule(); r.Customers = []string{"Kenter"}; return r }(),
			Scope{Customer: "Dela"}, false},
		{"exclusion wins", func() Rule {
			r := baseRule()
			r.Customers = []string{"Kenter", "Dela"}
			r.ExcludedCustomers = []string{"Dela"}
			return r
		}(), Scope{Customer: "Dela"}, false},
		{"exclusion only", func() Rule { r := baseRule(); r.ExcludedCustomers = []string{"Internal"}; return r }(),
			Scope{Customer: "Internal"}, false},
		{"exclusion other customer", func() Rule { r := baseRule(); r.ExcludedCustomers = []string{"Internal"}; return r }(),
			Scope{Customer: "Kenter"}, true},
		{"department match", func() Rule { r := baseRule(); r.Departments = []string{"Warehouse"}; return r }(),
			Scope{Customer: "Kenter", Departments: []string{"Field", "Warehouse"}}, true},
		{"department miss", func() Rule { r := baseRule(); r.Departments = []string{"Warehouse"}; return r }(),
			Scope{Customer: "Kenter", Departments: []string{"Field"}}, false},
		{"product match", func() Rule { r := baseRule(); r.Products = []string{"PRD-1"}; return r }(),
			Scope{Customer: "Kenter", Products: []string{"prd-1"}}, true},
		{"product miss", func() Rule { r := baseRule(); r.Products = []string{"PRD-1"}; return r }(),
			Scope{Customer: "Kenter"}, false},
		{"customer without name never matches an include list", func() Rule {
			r := baseRule()
			r.Customers = []string{"Kenter"}
			return r
		}(), Scope{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.rule.MatchesScope(c.scope); got != c.want {
				t.Fatalf("MatchesScope(%+v) = %v, want %v", c.scope, got, c.want)
			}
		})
	}
}

func TestTargetsFor(t *testing.T) {
	r := baseRule()
	r.Targets = []string{"alice"}
	r.Teams = []string{"Warehouse"}
	r.CustomerTargets = map[string][]string{"Kenter": {"bob"}}
	r.CustomerTeams = map[string][]string{"Dela": {"Field"}}

	users, teams := r.TargetsFor("KENTER")
	if len(users) != 1 || users[0] != "bob" || len(teams) != 0 {
		t.Errorf("Kenter -> %v / %v, want bob and no team", users, teams)
	}
	users, teams = r.TargetsFor("Dela")
	if len(users) != 1 || users[0] != "alice" || len(teams) != 1 || teams[0] != "Field" {
		t.Errorf("Dela -> %v / %v, want alice and team Field", users, teams)
	}
	users, teams = r.TargetsFor("Someone else")
	if len(users) != 1 || users[0] != "alice" || len(teams) != 1 || teams[0] != "Warehouse" {
		t.Errorf("default -> %v / %v, want the rule defaults", users, teams)
	}
	if got := r.CustomerRouting(); len(got) != 2 || got[0] != "Dela" || got[1] != "Kenter" {
		t.Errorf("CustomerRouting() = %v, want [Dela Kenter]", got)
	}
}
