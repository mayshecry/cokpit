package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cockpit/pkg/automation"
	"cockpit/pkg/order"
	"cockpit/pkg/swi"
)

var ErrDuplicateAutomationName = errors.New("automation rule name already exists")

const automationRuleCols = `id, name, trigger_type, threshold_minutes, action_type, targets_json, teams_json,
	customers_json, excluded_customers_json, departments_json, products_json,
	customer_targets_json, customer_teams_json,
	message, escalation_level, enabled, created_by, created_at, updated_at`

// cleanList trims every entry, drops empties and removes case-insensitive
// duplicates while keeping the original order.
func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, trimmed)
	}
	return out
}

// cleanMap cleans the per-customer routing map, dropping empty entries.
func cleanMap(values map[string][]string) map[string][]string {
	out := map[string][]string{}
	for name, list := range values {
		key := strings.TrimSpace(name)
		if key == "" {
			continue
		}
		cleaned := cleanList(list)
		if len(cleaned) == 0 {
			continue
		}
		out[key] = cleaned
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeAutomationRule(r automation.Rule) automation.Rule {
	r.Name = strings.TrimSpace(r.Name)
	r.Message = strings.TrimSpace(r.Message)
	r.EscalationLevel = strings.TrimSpace(r.EscalationLevel)
	if r.EscalationLevel == "" {
		r.EscalationLevel = "L1"
	}
	r.Targets = cleanList(r.Targets)
	r.Teams = cleanList(r.Teams)
	r.Customers = cleanList(r.Customers)
	r.ExcludedCustomers = cleanList(r.ExcludedCustomers)
	r.Departments = cleanList(r.Departments)
	r.Products = cleanList(r.Products)
	r.CustomerTargets = cleanMap(r.CustomerTargets)
	r.CustomerTeams = cleanMap(r.CustomerTeams)
	if r.Targets == nil {
		r.Targets = []string{}
	}
	return r
}

func scanAutomationRule(row interface{ Scan(...any) error }) (automation.Rule, error) {
	var r automation.Rule
	var targets, teams, customers, excluded, departments, products, custTargets, custTeams string
	var enabled int
	var createdMS, updatedMS int64
	if err := row.Scan(&r.ID, &r.Name, &r.Trigger, &r.ThresholdMinutes, &r.Action, &targets, &teams,
		&customers, &excluded, &departments, &products, &custTargets, &custTeams,
		&r.Message, &r.EscalationLevel, &enabled, &r.CreatedBy, &createdMS, &updatedMS); err != nil {
		return automation.Rule{}, err
	}
	decodeList := func(raw, field string) ([]string, error) {
		if strings.TrimSpace(raw) == "" {
			return []string{}, nil
		}
		var out []string
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("decode automation %s: %w", field, err)
		}
		if out == nil {
			out = []string{}
		}
		return out, nil
	}
	decodeMap := func(raw, field string) (map[string][]string, error) {
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
		var out map[string][]string
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("decode automation %s: %w", field, err)
		}
		return out, nil
	}
	var err error
	if r.Targets, err = decodeList(targets, "targets"); err != nil {
		return automation.Rule{}, err
	}
	if r.Teams, err = decodeList(teams, "teams"); err != nil {
		return automation.Rule{}, err
	}
	if r.Customers, err = decodeList(customers, "customers"); err != nil {
		return automation.Rule{}, err
	}
	if r.ExcludedCustomers, err = decodeList(excluded, "excluded customers"); err != nil {
		return automation.Rule{}, err
	}
	if r.Departments, err = decodeList(departments, "departments"); err != nil {
		return automation.Rule{}, err
	}
	if r.Products, err = decodeList(products, "products"); err != nil {
		return automation.Rule{}, err
	}
	if r.CustomerTargets, err = decodeMap(custTargets, "customer targets"); err != nil {
		return automation.Rule{}, err
	}
	if r.CustomerTeams, err = decodeMap(custTeams, "customer teams"); err != nil {
		return automation.Rule{}, err
	}
	r.Enabled = enabled != 0
	r.CreatedAt = fromMillis(createdMS)
	r.UpdatedAt = fromMillis(updatedMS)
	return r, nil
}

// encodeAutomationRule renders the list/map columns for storage.
func encodeAutomationRule(r automation.Rule) (targets, teams, customers, excluded, departments, products, custTargets, custTeams string) {
	marshal := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			return "null"
		}
		return string(raw)
	}
	emptyIfNil := func(list []string) []string {
		if list == nil {
			return []string{}
		}
		return list
	}
	emptyMapIfNil := func(m map[string][]string) map[string][]string {
		if m == nil {
			return map[string][]string{}
		}
		return m
	}
	return marshal(emptyIfNil(r.Targets)), marshal(emptyIfNil(r.Teams)),
		marshal(emptyIfNil(r.Customers)), marshal(emptyIfNil(r.ExcludedCustomers)),
		marshal(emptyIfNil(r.Departments)), marshal(emptyIfNil(r.Products)),
		marshal(emptyMapIfNil(r.CustomerTargets)), marshal(emptyMapIfNil(r.CustomerTeams))
}

func (s *Store) userExists(ctx context.Context, username string) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&exists); err != nil {
		return false, fmt.Errorf("validate automation target: %w", err)
	}
	return exists > 0, nil
}

func (s *Store) departmentExists(ctx context.Context, name string) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM departments WHERE name = ? COLLATE NOCASE`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("validate automation team: %w", err)
	}
	return exists > 0, nil
}

func (s *Store) validateAutomationTargets(ctx context.Context, rule automation.Rule) error {
	checkUsers := func(users []string) error {
		for _, target := range users {
			ok, err := s.userExists(ctx, target)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: %s", ErrUnknownAssignee, target)
			}
		}
		return nil
	}
	checkTeams := func(teams []string) error {
		for _, team := range teams {
			ok, err := s.departmentExists(ctx, team)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: %s", ErrUnknownDepartment, team)
			}
		}
		return nil
	}
	if err := checkUsers(rule.Targets); err != nil {
		return err
	}
	if err := checkTeams(rule.Teams); err != nil {
		return err
	}
	for _, users := range rule.CustomerTargets {
		if err := checkUsers(users); err != nil {
			return err
		}
	}
	for _, teams := range rule.CustomerTeams {
		if err := checkTeams(teams); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateAutomationRule(ctx context.Context, rule automation.Rule, createdBy string, now time.Time) (automation.Rule, error) {
	rule = normalizeAutomationRule(rule)
	if err := automation.Validate(rule); err != nil {
		return automation.Rule{}, err
	}
	if err := s.validateAutomationTargets(ctx, rule); err != nil {
		return automation.Rule{}, err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rules WHERE name = ?`, rule.Name).Scan(&exists); err != nil {
		return automation.Rule{}, err
	}
	if exists > 0 {
		return automation.Rule{}, ErrDuplicateAutomationName
	}
	targets, teams, customers, excluded, departments, products, custTargets, custTeams := encodeAutomationRule(rule)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO automation_rules (name, trigger_type, threshold_minutes, action_type, targets_json, teams_json,
			customers_json, excluded_customers_json, departments_json, products_json,
			customer_targets_json, customer_teams_json,
			message, escalation_level, enabled, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Name, rule.Trigger, rule.ThresholdMinutes, rule.Action, targets, teams,
		customers, excluded, departments, products, custTargets, custTeams,
		rule.Message, rule.EscalationLevel, boolToInt(rule.Enabled), createdBy, millis(now), millis(now))
	if err != nil {
		return automation.Rule{}, fmt.Errorf("insert automation rule: %w", err)
	}
	rule.ID, err = res.LastInsertId()
	if err != nil {
		return automation.Rule{}, err
	}
	return s.AutomationRuleByID(ctx, rule.ID)
}

func (s *Store) UpdateAutomationRule(ctx context.Context, rule automation.Rule, now time.Time) (automation.Rule, error) {
	rule = normalizeAutomationRule(rule)
	if err := automation.Validate(rule); err != nil {
		return automation.Rule{}, err
	}
	if err := s.validateAutomationTargets(ctx, rule); err != nil {
		return automation.Rule{}, err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rules WHERE name = ? AND id != ?`, rule.Name, rule.ID).Scan(&exists); err != nil {
		return automation.Rule{}, err
	}
	if exists > 0 {
		return automation.Rule{}, ErrDuplicateAutomationName
	}
	targets, teams, customers, excluded, departments, products, custTargets, custTeams := encodeAutomationRule(rule)
	res, err := s.db.ExecContext(ctx, `
		UPDATE automation_rules SET name = ?, trigger_type = ?, threshold_minutes = ?, action_type = ?,
		targets_json = ?, teams_json = ?, customers_json = ?, excluded_customers_json = ?,
		departments_json = ?, products_json = ?, customer_targets_json = ?, customer_teams_json = ?,
		message = ?, escalation_level = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		rule.Name, rule.Trigger, rule.ThresholdMinutes, rule.Action, targets, teams,
		customers, excluded, departments, products, custTargets, custTeams,
		rule.Message, rule.EscalationLevel, boolToInt(rule.Enabled), millis(now), rule.ID)
	if err != nil {
		return automation.Rule{}, fmt.Errorf("update automation rule: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return automation.Rule{}, ErrNotFound
	}
	return s.AutomationRuleByID(ctx, rule.ID)
}

func (s *Store) DeleteAutomationRule(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM automation_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AutomationRuleByID(ctx context.Context, id int64) (automation.Rule, error) {
	r, err := scanAutomationRule(s.db.QueryRowContext(ctx,
		`SELECT `+automationRuleCols+` FROM automation_rules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return automation.Rule{}, ErrNotFound
	}
	return r, err
}

func (s *Store) ListAutomationRules(ctx context.Context, enabledOnly bool) ([]automation.Rule, error) {
	query := `SELECT ` + automationRuleCols + ` FROM automation_rules`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []automation.Rule{}
	for rows.Next() {
		r, err := scanAutomationRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const automationRunCols = `r.id, r.rule_id, COALESCE(ar.name, 'deleted rule'), r.order_id, o.order_number,
	r.trigger_key, r.action_type, r.status, r.detail, r.error, r.created_at`

func scanAutomationRun(row interface{ Scan(...any) error }) (automation.Run, error) {
	var r automation.Run
	var created int64
	if err := row.Scan(&r.ID, &r.RuleID, &r.RuleName, &r.OrderID, &r.OrderNumber, &r.TriggerKey,
		&r.Action, &r.Status, &r.Detail, &r.Error, &created); err != nil {
		return automation.Run{}, err
	}
	r.CreatedAt = fromMillis(created)
	r.NotifiedUsers = []string{}
	return r, nil
}

func (s *Store) automationRunByID(ctx context.Context, id int64) (automation.Run, error) {
	return scanAutomationRun(s.db.QueryRowContext(ctx, `
		SELECT `+automationRunCols+` FROM automation_runs r
		JOIN orders o ON o.id = r.order_id
		LEFT JOIN automation_rules ar ON ar.id = r.rule_id WHERE r.id = ?`, id))
}

func (s *Store) ListAutomationRuns(ctx context.Context, limit int) ([]automation.Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+automationRunCols+` FROM automation_runs r
		JOIN orders o ON o.id = r.order_id
		LEFT JOIN automation_rules ar ON ar.id = r.rule_id
		ORDER BY r.created_at DESC, r.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []automation.Run{}
	for rows.Next() {
		r, err := scanAutomationRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type automationCandidate struct {
	orderID     int64
	orderNumber string
	key         string
	detail      string
	customer    string
	scope       automation.Scope
}

// automationScopes loads the customer, product codes and product departments
// for a set of orders, so rule scoping can be evaluated in memory.
func (s *Store) automationScopes(ctx context.Context, orderIDs []int64) (map[int64]automation.Scope, error) {
	out := map[int64]automation.Scope{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(orderIDs))
	args := make([]any, len(orderIDs))
	for i, id := range orderIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.customer_name,
		       COALESCE(GROUP_CONCAT(DISTINCT om.product_code), ''),
		       COALESCE(GROUP_CONCAT(DISTINCT p.department), '')
		FROM orders o
		LEFT JOIN order_manuals om ON om.order_id = o.id
		LEFT JOIN products p ON p.id = om.product_id
		WHERE o.id IN (`+strings.Join(placeholders, ",")+`)
		GROUP BY o.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("load automation scopes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var customer, products, departments string
		if err := rows.Scan(&id, &customer, &products, &departments); err != nil {
			return nil, err
		}
		out[id] = automation.Scope{
			Customer:    customer,
			Products:    splitCSV(products),
			Departments: splitCSV(departments),
		}
	}
	return out, rows.Err()
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// applyScope attaches the order scope to every candidate and drops the ones the
// rule does not apply to.
func (s *Store) applyScope(ctx context.Context, rule automation.Rule, candidates []automationCandidate) ([]automationCandidate, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}
	ids := make([]int64, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.orderID)
	}
	scopes, err := s.automationScopes(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]automationCandidate, 0, len(candidates))
	for _, c := range candidates {
		scope := scopes[c.orderID]
		c.customer = scope.Customer
		c.scope = scope
		if !rule.MatchesScope(scope) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) automationRunExists(ctx context.Context, ruleID int64, key string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND trigger_key = ?`, ruleID, key).Scan(&exists)
	return exists > 0, err
}

func (s *Store) automationCandidates(ctx context.Context, rule automation.Rule, now time.Time) ([]automationCandidate, error) {
	switch rule.Trigger {
	case automation.TriggerQCFailed:
		rows, err := s.db.QueryContext(ctx, `
			SELECT q.id, o.id, o.order_number, q.fail_reason, q.notes
			FROM qc_checks q JOIN orders o ON o.id = q.order_id
			WHERE q.status = 'FAIL' AND q.created_at >= ?
			  AND NOT EXISTS (SELECT 1 FROM automation_runs r
			                  WHERE r.rule_id = ? AND r.trigger_key = 'qc:' || q.id)
			ORDER BY q.id ASC`, millis(rule.CreatedAt), rule.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []automationCandidate{}
		for rows.Next() {
			var checkID int64
			var c automationCandidate
			var reason, notes string
			if err := rows.Scan(&checkID, &c.orderID, &c.orderNumber, &reason, &notes); err != nil {
				return nil, err
			}
			c.key = fmt.Sprintf("qc:%d", checkID)
			c.detail = "QC failed"
			if reason != "" {
				c.detail += ": " + reason
			} else if notes != "" {
				c.detail += ": " + notes
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return s.applyScope(ctx, rule, out)

	case automation.TriggerHoldAging:
		cutoff := millis(now.Add(-time.Duration(rule.ThresholdMinutes) * time.Minute))
		rows, err := s.db.QueryContext(ctx, `
			SELECT h.id, o.id, o.order_number, h.reason, h.created_at
			FROM holds h JOIN orders o ON o.id = h.order_id
			WHERE h.resolved_at IS NULL AND h.created_at <= ?
			  AND NOT EXISTS (SELECT 1 FROM automation_runs r
			                  WHERE r.rule_id = ? AND r.trigger_key = 'hold:' || h.id)
			ORDER BY h.id ASC`, cutoff, rule.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []automationCandidate{}
		for rows.Next() {
			var holdID, created int64
			var c automationCandidate
			var reason string
			if err := rows.Scan(&holdID, &c.orderID, &c.orderNumber, &reason, &created); err != nil {
				return nil, err
			}
			c.key = fmt.Sprintf("hold:%d", holdID)
			age := now.Sub(fromMillis(created)).Round(time.Minute)
			c.detail = fmt.Sprintf("hold active for %s: %s", age, reason)
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return s.applyScope(ctx, rule, out)

	case automation.TriggerSLABreach:
		orders, err := s.ListOrders(ctx, nil)
		if err != nil {
			return nil, err
		}
		holds, err := s.ActiveHoldStarts(ctx)
		if err != nil {
			return nil, err
		}
		out := []automationCandidate{}
		for _, o := range orders {
			if order.NormalizeHeld(o.Status) == order.StatusCompleted {
				continue
			}
			var holdSince *time.Time
			if start, ok := holds[o.ID]; ok {
				startCopy := start
				holdSince = &startCopy
			}
			sla := order.ComputeSLAPaused(o.TargetCompletion, now, o.PausedSeconds, holdSince)
			if sla.Status != order.SLABreached {
				continue
			}
			key := fmt.Sprintf("sla:%d", o.ID)
			exists, err := s.automationRunExists(ctx, rule.ID, key)
			if err != nil {
				return nil, err
			}
			if exists {
				continue
			}
			out = append(out, automationCandidate{
				orderID: o.ID, orderNumber: o.OrderNumber, key: key,
				detail: fmt.Sprintf("SLA breached by %s", -sla.Remaining.Round(time.Minute)),
			})
		}
		return s.applyScope(ctx, rule, out)
	default:
		return nil, fmt.Errorf("%w: unsupported trigger %s", automation.ErrValidation, rule.Trigger)
	}
}

func automationNotificationBody(rule automation.Rule, c automationCandidate) string {
	body := fmt.Sprintf("%s — %s", c.orderNumber, c.detail)
	if rule.Message != "" {
		body += " — " + rule.Message
	}
	return body
}

// teamMembers returns the usernames in a department, ordered by name.
func (s *Store) teamMembers(ctx context.Context, department string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.username FROM users u
		JOIN user_departments ud ON ud.user_id = u.id
		JOIN departments d ON d.id = ud.department_id
		WHERE d.name = ? COLLATE NOCASE
		ORDER BY u.username ASC`, department)
	if err != nil {
		return nil, fmt.Errorf("team members: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, err
		}
		out = append(out, username)
	}
	return out, rows.Err()
}

// openOrderCount returns how many unfinished orders a user currently owns.
func (s *Store) openOrderCount(ctx context.Context, username string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE assigned_to = ? AND status <> 'Completed'`, username).Scan(&count)
	return count, err
}

// leastBusyMember picks the team member with the fewest unfinished orders, so a
// team target load-balances instead of always landing on the same person.
func (s *Store) leastBusyMember(ctx context.Context, department string) (string, []string, error) {
	members, err := s.teamMembers(ctx, department)
	if err != nil {
		return "", nil, err
	}
	if len(members) == 0 {
		return "", members, nil
	}
	best := members[0]
	bestCount := -1
	for _, member := range members {
		count, err := s.openOrderCount(ctx, member)
		if err != nil {
			return "", members, err
		}
		if bestCount == -1 || count < bestCount {
			best, bestCount = member, count
		}
	}
	return best, members, nil
}

// expandTeams turns team names into their member usernames, preserving order and
// dropping duplicates.
func (s *Store) expandTeams(ctx context.Context, teams []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, team := range teams {
		members, err := s.teamMembers(ctx, team)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if seen[member] {
				continue
			}
			seen[member] = true
			out = append(out, member)
		}
	}
	return out, nil
}

// resolveRecipients applies per-customer routing and expands teams.
func (s *Store) resolveRecipients(ctx context.Context, rule automation.Rule, customer string) (users []string, teams []string, err error) {
	users, teams = rule.TargetsFor(customer)
	if len(users)+len(teams) == 0 {
		return users, teams, fmt.Errorf("%w: rule %q has no recipients for customer %q", automation.ErrValidation, rule.Name, customer)
	}
	return users, teams, nil
}

func (s *Store) executeAutomationAction(ctx context.Context, rule automation.Rule, c automationCandidate, runID int64, now time.Time) ([]string, error) {
	body := automationNotificationBody(rule, c)
	actor := "automation: " + rule.Name
	users, teams, err := s.resolveRecipients(ctx, rule, c.customer)
	if err != nil {
		return nil, err
	}
	switch rule.Action {
	case automation.ActionNotify:
		recipients := append([]string{}, users...)
		members, err := s.expandTeams(ctx, teams)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, r := range recipients {
			seen[r] = true
		}
		for _, member := range members {
			if !seen[member] {
				seen[member] = true
				recipients = append(recipients, member)
			}
		}
		if len(recipients) == 0 {
			return nil, fmt.Errorf("%w: rule %q has no recipients for customer %q", automation.ErrValidation, rule.Name, c.customer)
		}
		for _, target := range recipients {
			if err := s.Notify(ctx, target, "automation", c.orderID, c.orderNumber, runID, body, now); err != nil {
				return nil, err
			}
		}
		return recipients, nil

	case automation.ActionAssign:
		target := ""
		if len(users) > 0 {
			target = users[0]
		} else {
			member, members, err := s.leastBusyMember(ctx, teams[0])
			if err != nil {
				return nil, err
			}
			if member == "" {
				return nil, fmt.Errorf("%w: team %q has no members, so there is nobody to assign %s to",
					automation.ErrValidation, teams[0], c.orderNumber)
			}
			target = member
			_ = members
		}
		if _, err := s.SetAssignee(ctx, c.orderID, target, actor, now); err != nil {
			return nil, err
		}
		if err := s.Notify(ctx, target, "automation", c.orderID, c.orderNumber, runID,
			body+" — assigned to you", now); err != nil {
			return nil, err
		}
		return []string{target}, nil

	case automation.ActionEscalate:
		target := ""
		if len(users) > 0 {
			target = users[0]
		} else if len(teams) > 0 {
			member, _, err := s.leastBusyMember(ctx, teams[0])
			if err != nil {
				return nil, err
			}
			target = member
		}
		reason := swi.ReasonManual
		switch rule.Trigger {
		case automation.TriggerQCFailed:
			reason = swi.ReasonQuality
		case automation.TriggerSLABreach:
			reason = swi.ReasonSLABreach
		case automation.TriggerHoldAging:
			reason = swi.ReasonBlocked
		}
		_, _, _, err := s.EscalateProcess(ctx, c.orderID, swi.Level(rule.EscalationLevel), reason,
			body, target, actor, now)
		if err != nil {
			return nil, err
		}
		if target != "" {
			return []string{target}, nil
		}
		return []string{}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported action %s", automation.ErrValidation, rule.Action)
	}
}

func (s *Store) EvaluateAutomations(ctx context.Context, now time.Time) ([]automation.Run, error) {
	rules, err := s.ListAutomationRules(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list enabled automations: %w", err)
	}
	executed := []automation.Run{}
	for _, rule := range rules {
		candidates, err := s.automationCandidates(ctx, rule, now)
		if err != nil {
			return executed, fmt.Errorf("automation %s candidates: %w", rule.Name, err)
		}
		for _, c := range candidates {
			res, err := s.db.ExecContext(ctx, `
				INSERT OR IGNORE INTO automation_runs
				(rule_id, order_id, trigger_key, action_type, status, detail, created_at)
				VALUES (?, ?, ?, ?, 'PENDING', ?, ?)`,
				rule.ID, c.orderID, c.key, rule.Action, c.detail, millis(now))
			if err != nil {
				return executed, fmt.Errorf("claim automation run: %w", err)
			}
			if affected, _ := res.RowsAffected(); affected == 0 {
				continue
			}
			runID, err := res.LastInsertId()
			if err != nil {
				return executed, err
			}
			notified, actionErr := s.executeAutomationAction(ctx, rule, c, runID, now)
			status, errText := automation.RunSuccess, ""
			if actionErr != nil {
				status, errText = automation.RunFailed, actionErr.Error()
			}
			if _, err := s.db.ExecContext(ctx,
				`UPDATE automation_runs SET status = ?, error = ? WHERE id = ?`, status, errText, runID); err != nil {
				return executed, fmt.Errorf("finish automation run: %w", err)
			}
			run, err := s.automationRunByID(ctx, runID)
			if err != nil {
				return executed, err
			}
			run.NotifiedUsers = notified
			executed = append(executed, run)
		}
	}
	return executed, nil
}
