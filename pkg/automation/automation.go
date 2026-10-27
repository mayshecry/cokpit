package automation

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Trigger string

const (
	TriggerQCFailed  Trigger = "QC_FAILED"
	TriggerSLABreach Trigger = "SLA_BREACHED"
	TriggerHoldAging Trigger = "HOLD_AGING"
)

type Action string

const (
	ActionNotify   Action = "NOTIFY"
	ActionAssign   Action = "ASSIGN"
	ActionEscalate Action = "ESCALATE"
)

type RunStatus string

const (
	RunPending RunStatus = "PENDING"
	RunSuccess RunStatus = "SUCCESS"
	RunFailed  RunStatus = "FAILED"
)

var ErrValidation = errors.New("invalid automation rule")

// Rule is one automation rule.
//
// Recipients are expressed either as individual users (Targets) or as
// departments/teams (Teams). A team is expanded at execution time:
// NOTIFY notifies every member, ASSIGN hands the order to the member with the
// fewest open orders, ESCALATE records the team as the escalation owner.
//
// Scope narrows which orders a rule applies to. Every scope list is optional
// and an empty list means "no restriction on this dimension".
type Rule struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Trigger          Trigger   `json:"trigger"`
	ThresholdMinutes int       `json:"thresholdMinutes"`
	Action           Action    `json:"action"`
	Targets          []string  `json:"targets"`
	Teams            []string  `json:"teams,omitempty"`
	Message          string    `json:"message"`
	EscalationLevel  string    `json:"escalationLevel,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedBy        string    `json:"createdBy"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`

	// Customers limits the rule to these customer names. Empty = every
	// customer.
	Customers []string `json:"customers,omitempty"`
	// ExcludedCustomers removes customers from whatever the other scope
	// allows. Exclusions win over inclusions.
	ExcludedCustomers []string `json:"excludedCustomers,omitempty"`
	// Departments limits the rule to orders whose product belongs to one of
	// these departments.
	Departments []string `json:"departments,omitempty"`
	// Products limits the rule to orders that carry one of these product
	// codes.
	Products []string `json:"products,omitempty"`

	// CustomerTargets / CustomerTeams route a specific customer to different
	// people than the rule default. A key matches a customer name
	// case-insensitively and replaces the default recipients for that
	// customer.
	CustomerTargets map[string][]string `json:"customerTargets,omitempty"`
	CustomerTeams   map[string][]string `json:"customerTeams,omitempty"`
}

type Run struct {
	ID            int64     `json:"id"`
	RuleID        int64     `json:"ruleId"`
	RuleName      string    `json:"ruleName"`
	OrderID       int64     `json:"orderId"`
	OrderNumber   string    `json:"orderNumber"`
	TriggerKey    string    `json:"triggerKey"`
	Action        Action    `json:"action"`
	Status        RunStatus `json:"status"`
	Detail        string    `json:"detail"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	NotifiedUsers []string  `json:"-"`
}

// Scope is the part of an order that rule scoping looks at.
type Scope struct {
	Customer    string
	Departments []string
	Products    []string
}

func IsValidTrigger(v Trigger) bool {
	switch v {
	case TriggerQCFailed, TriggerSLABreach, TriggerHoldAging:
		return true
	default:
		return false
	}
}

func IsValidAction(v Action) bool {
	switch v {
	case ActionNotify, ActionAssign, ActionEscalate:
		return true
	default:
		return false
	}
}

func validationError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// has reports whether list contains value, compared case-insensitively.
func has(list []string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}

// MatchesScope reports whether the rule applies to an order with the given
// scope. Exclusions always win over inclusions.
func (r Rule) MatchesScope(s Scope) bool {
	customer := strings.TrimSpace(s.Customer)
	if len(r.Customers) > 0 && !has(r.Customers, customer) {
		return false
	}
	if has(r.ExcludedCustomers, customer) {
		return false
	}
	if len(r.Departments) > 0 {
		matched := false
		for _, dep := range s.Departments {
			if has(r.Departments, dep) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(r.Products) > 0 {
		matched := false
		for _, code := range s.Products {
			if has(r.Products, code) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// TargetsFor returns the recipients that apply to one customer: the
// per-customer override when the rule has one for that customer, otherwise the
// rule defaults.
func (r Rule) TargetsFor(customer string) (users []string, teams []string) {
	customer = strings.TrimSpace(customer)
	for name, override := range r.CustomerTargets {
		if customer != "" && strings.EqualFold(strings.TrimSpace(name), customer) && len(override) > 0 {
			if teamOverride, ok := lookupFold(r.CustomerTeams, name); ok && len(teamOverride) > 0 {
				return override, teamOverride
			}
			return override, nil
		}
	}
	if teamOverride, ok := lookupFold(r.CustomerTeams, customer); ok && len(teamOverride) > 0 {
		return r.Targets, teamOverride
	}
	return r.Targets, r.Teams
}

func lookupFold(m map[string][]string, key string) ([]string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}
	for name, value := range m {
		if strings.EqualFold(strings.TrimSpace(name), key) {
			return value, true
		}
	}
	return nil, false
}

// HasCustomerRouting reports whether at least one per-customer override exists.
func (r Rule) HasCustomerRouting() bool {
	return len(r.CustomerTargets) > 0 || len(r.CustomerTeams) > 0
}

// CustomerRouting returns the customer names that have an override, sorted.
func (r Rule) CustomerRouting() []string {
	seen := map[string]bool{}
	out := []string{}
	for name := range r.CustomerTargets {
		key := strings.TrimSpace(name)
		if key == "" || seen[strings.ToLower(key)] {
			continue
		}
		seen[strings.ToLower(key)] = true
		out = append(out, key)
	}
	for name := range r.CustomerTeams {
		key := strings.TrimSpace(name)
		if key == "" || seen[strings.ToLower(key)] {
			continue
		}
		seen[strings.ToLower(key)] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// describeRecipients renders "2 people" / "the team Warehouse" for error
// messages so a rejected rule explains itself.
func describeRecipients(users, teams []string) string {
	parts := []string{}
	if len(users) == 1 {
		parts = append(parts, "1 person (@"+users[0]+")")
	} else if len(users) > 1 {
		parts = append(parts, fmt.Sprintf("%d people", len(users)))
	}
	if len(teams) == 1 {
		parts = append(parts, "1 team ("+teams[0]+")")
	} else if len(teams) > 1 {
		parts = append(parts, fmt.Sprintf("%d teams", len(teams)))
	}
	if len(parts) == 0 {
		return "nobody"
	}
	return strings.Join(parts, " and ")
}

// ValidateRecipients checks a recipient set against the rule's action. where
// is appended to the message, e.g. " for customer Kenter".
func ValidateRecipients(action Action, users, teams []string, where string) error {
	switch action {
	case ActionNotify:
		if len(users) == 0 && len(teams) == 0 {
			return validationError("NOTIFY needs at least one person or team%s", where)
		}
	case ActionAssign:
		if len(users)+len(teams) != 1 {
			return validationError(
				"ASSIGN hands the order to exactly one person or team%s, but your selection reaches %s. "+
					"Pick a single person, or a single team — with a team, the member with the fewest open orders gets the order. "+
					"Use NOTIFY if several people should be informed",
				where, describeRecipients(users, teams))
		}
	case ActionEscalate:
		if len(users)+len(teams) > 1 {
			return validationError("ESCALATE accepts at most one person or team%s, but your selection reaches %s",
				where, describeRecipients(users, teams))
		}
	}
	return nil
}

func Validate(r Rule) error {
	if strings.TrimSpace(r.Name) == "" || len(strings.TrimSpace(r.Name)) > 120 {
		return validationError("name is required and must be at most 120 characters")
	}
	if !IsValidTrigger(r.Trigger) {
		return validationError("trigger must be QC_FAILED, SLA_BREACHED or HOLD_AGING")
	}
	if !IsValidAction(r.Action) {
		return validationError("action must be NOTIFY, ASSIGN or ESCALATE")
	}
	if r.Trigger == TriggerHoldAging {
		if r.ThresholdMinutes < 1 || r.ThresholdMinutes > 525600 {
			return validationError("hold threshold must be between 1 and 525600 minutes")
		}
	} else if r.ThresholdMinutes != 0 {
		return validationError("thresholdMinutes is only valid for HOLD_AGING")
	}
	if len(r.Message) > 500 {
		return validationError("message must be at most 500 characters")
	}
	if err := ValidateRecipients(r.Action, r.Targets, r.Teams, ""); err != nil {
		return err
	}
	if r.Action == ActionEscalate {
		switch r.EscalationLevel {
		case "L1", "L2", "L3":
		default:
			return validationError("escalationLevel must be L1, L2 or L3")
		}
	}

	// Recipient lists.
	seen := map[string]bool{}
	for _, target := range r.Targets {
		trimmed := strings.TrimSpace(target)
		if trimmed == "" {
			return validationError("target users must not be empty")
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			return validationError("duplicate target user %q", trimmed)
		}
		seen[key] = true
	}
	for _, team := range r.Teams {
		trimmed := strings.TrimSpace(team)
		if trimmed == "" {
			return validationError("target teams must not be empty")
		}
		key := "team:" + strings.ToLower(trimmed)
		if seen[key] {
			return validationError("duplicate target team %q", trimmed)
		}
		seen[key] = true
	}

	// Scope lists.
	for _, list := range []struct {
		field  string
		values []string
	}{
		{"customers", r.Customers},
		{"excludedCustomers", r.ExcludedCustomers},
		{"departments", r.Departments},
		{"products", r.Products},
	} {
		seen := map[string]bool{}
		for _, value := range list.values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				return validationError("%s must not contain empty values", list.field)
			}
			key := strings.ToLower(trimmed)
			if seen[key] {
				return validationError("duplicate %s entry %q", list.field, trimmed)
			}
			seen[key] = true
		}
	}
	for _, excluded := range r.ExcludedCustomers {
		if has(r.Customers, excluded) {
			return validationError(
				"%q is both included and excluded — an exclusion always wins, so the rule would never fire for it; remove it from one of the two lists",
				strings.TrimSpace(excluded))
		}
	}

	// Per-customer routing must itself be valid for the action.
	for _, name := range r.CustomerRouting() {
		if len(r.Customers) > 0 && !has(r.Customers, name) {
			return validationError(
				"per-customer routing for %q is pointless: the rule only applies to %s; "+
					"add the customer to the include list or remove the routing",
				strings.TrimSpace(name), strings.Join(r.Customers, ", "))
		}
		if has(r.ExcludedCustomers, name) {
			return validationError("per-customer routing for %q is pointless: that customer is excluded", strings.TrimSpace(name))
		}
		users, _ := lookupFold(r.CustomerTargets, name)
		teams, _ := lookupFold(r.CustomerTeams, name)
		if len(users) == 0 && len(teams) == 0 {
			continue
		}
		if err := ValidateRecipients(r.Action, users, teams, fmt.Sprintf(" for customer %s", strings.TrimSpace(name))); err != nil {
			return err
		}
		for _, user := range users {
			if strings.TrimSpace(user) == "" {
				return validationError("per-customer targets must not be empty")
			}
		}
		for _, team := range teams {
			if strings.TrimSpace(team) == "" {
				return validationError("per-customer teams must not be empty")
			}
		}
	}
	return nil
}
