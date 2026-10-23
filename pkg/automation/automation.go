package automation

import (
	"errors"
	"fmt"
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

type Rule struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Trigger          Trigger   `json:"trigger"`
	ThresholdMinutes int       `json:"thresholdMinutes"`
	Action           Action    `json:"action"`
	Targets          []string  `json:"targets"`
	Message          string    `json:"message"`
	EscalationLevel  string    `json:"escalationLevel,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedBy        string    `json:"createdBy"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
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
	switch r.Action {
	case ActionNotify:
		if len(r.Targets) == 0 {
			return validationError("NOTIFY needs at least one target user")
		}
	case ActionAssign:
		if len(r.Targets) != 1 {
			return validationError("ASSIGN needs exactly one target user")
		}
	case ActionEscalate:
		if len(r.Targets) > 1 {
			return validationError("ESCALATE accepts at most one target user")
		}
		switch r.EscalationLevel {
		case "L1", "L2", "L3":
		default:
			return validationError("escalationLevel must be L1, L2 or L3")
		}
	}
	seen := map[string]bool{}
	for _, target := range r.Targets {
		if strings.TrimSpace(target) == "" {
			return validationError("target users must not be empty")
		}
		if seen[target] {
			return validationError("duplicate target user %q", target)
		}
		seen[target] = true
	}
	return nil
}
