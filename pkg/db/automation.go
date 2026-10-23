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

const automationRuleCols = `id, name, trigger_type, threshold_minutes, action_type, targets_json,
	message, escalation_level, enabled, created_by, created_at, updated_at`

func normalizeAutomationRule(r automation.Rule) automation.Rule {
	r.Name = strings.TrimSpace(r.Name)
	r.Message = strings.TrimSpace(r.Message)
	r.EscalationLevel = strings.TrimSpace(r.EscalationLevel)
	if r.EscalationLevel == "" {
		r.EscalationLevel = "L1"
	}
	targets := make([]string, 0, len(r.Targets))
	for _, target := range r.Targets {
		targets = append(targets, strings.TrimSpace(target))
	}
	r.Targets = targets
	return r
}

func scanAutomationRule(row interface{ Scan(...any) error }) (automation.Rule, error) {
	var r automation.Rule
	var targets string
	var enabled int
	var createdMS, updatedMS int64
	if err := row.Scan(&r.ID, &r.Name, &r.Trigger, &r.ThresholdMinutes, &r.Action, &targets,
		&r.Message, &r.EscalationLevel, &enabled, &r.CreatedBy, &createdMS, &updatedMS); err != nil {
		return automation.Rule{}, err
	}
	if err := json.Unmarshal([]byte(targets), &r.Targets); err != nil {
		return automation.Rule{}, fmt.Errorf("decode automation targets: %w", err)
	}
	if r.Targets == nil {
		r.Targets = []string{}
	}
	r.Enabled = enabled != 0
	r.CreatedAt = fromMillis(createdMS)
	r.UpdatedAt = fromMillis(updatedMS)
	return r, nil
}

func (s *Store) validateAutomationTargets(ctx context.Context, targets []string) error {
	for _, target := range targets {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, target).Scan(&exists); err != nil {
			return fmt.Errorf("validate automation target: %w", err)
		}
		if exists == 0 {
			return fmt.Errorf("%w: %s", ErrUnknownAssignee, target)
		}
	}
	return nil
}

func (s *Store) CreateAutomationRule(ctx context.Context, rule automation.Rule, createdBy string, now time.Time) (automation.Rule, error) {
	rule = normalizeAutomationRule(rule)
	if err := automation.Validate(rule); err != nil {
		return automation.Rule{}, err
	}
	if err := s.validateAutomationTargets(ctx, rule.Targets); err != nil {
		return automation.Rule{}, err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rules WHERE name = ?`, rule.Name).Scan(&exists); err != nil {
		return automation.Rule{}, err
	}
	if exists > 0 {
		return automation.Rule{}, ErrDuplicateAutomationName
	}
	targets, _ := json.Marshal(rule.Targets)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO automation_rules (name, trigger_type, threshold_minutes, action_type, targets_json,
			message, escalation_level, enabled, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Name, rule.Trigger, rule.ThresholdMinutes, rule.Action, string(targets), rule.Message,
		rule.EscalationLevel, boolToInt(rule.Enabled), createdBy, millis(now), millis(now))
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
	if err := s.validateAutomationTargets(ctx, rule.Targets); err != nil {
		return automation.Rule{}, err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rules WHERE name = ? AND id != ?`, rule.Name, rule.ID).Scan(&exists); err != nil {
		return automation.Rule{}, err
	}
	if exists > 0 {
		return automation.Rule{}, ErrDuplicateAutomationName
	}
	targets, _ := json.Marshal(rule.Targets)
	res, err := s.db.ExecContext(ctx, `
		UPDATE automation_rules SET name = ?, trigger_type = ?, threshold_minutes = ?, action_type = ?,
		targets_json = ?, message = ?, escalation_level = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		rule.Name, rule.Trigger, rule.ThresholdMinutes, rule.Action, string(targets), rule.Message,
		rule.EscalationLevel, boolToInt(rule.Enabled), millis(now), rule.ID)
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
		return out, rows.Err()

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
		return out, rows.Err()

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
		return out, nil
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

func (s *Store) executeAutomationAction(ctx context.Context, rule automation.Rule, c automationCandidate, runID int64, now time.Time) ([]string, error) {
	body := automationNotificationBody(rule, c)
	actor := "automation: " + rule.Name
	switch rule.Action {
	case automation.ActionNotify:
		for _, target := range rule.Targets {
			if err := s.Notify(ctx, target, "automation", c.orderID, c.orderNumber, runID, body, now); err != nil {
				return nil, err
			}
		}
		return rule.Targets, nil

	case automation.ActionAssign:
		target := rule.Targets[0]
		if _, err := s.SetAssignee(ctx, c.orderID, target, actor, now); err != nil {
			return nil, err
		}
		if err := s.Notify(ctx, target, "automation", c.orderID, c.orderNumber, runID,
			body+" — assigned to you", now); err != nil {
			return nil, err
		}
		return rule.Targets, nil

	case automation.ActionEscalate:
		target := ""
		if len(rule.Targets) == 1 {
			target = rule.Targets[0]
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
