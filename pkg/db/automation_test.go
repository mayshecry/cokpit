package db

import (
	"context"
	"strconv"
	"testing"
	"time"

	"cockpit/pkg/auth"
	"cockpit/pkg/automation"
	"cockpit/pkg/order"
	"cockpit/pkg/swi"
)

func createAutomationUser(t *testing.T, s *Store, username string) {
	t.Helper()
	if _, err := s.CreateUser(context.Background(), username, "test-hash", username, auth.RoleOperator, fixedNow()); err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
}

func TestAutomationQCFailureNotifyIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "quality-lead")
	o, err := s.CreateOrder(ctx, "AUTO-QC-1", now.Add(24*time.Hour), now, "tester")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Notify quality lead", Trigger: automation.TriggerQCFailed, Action: automation.ActionNotify,
		Targets: []string{"quality-lead"}, Message: "Please review", Enabled: true,
	}, "admin", now)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	check, err := s.SubmitQC(ctx, o.ID, order.QCFail, "inspector", "bad cable", "Cable seating out of spec", "inspector", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.EvaluateAutomations(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != automation.RunSuccess {
		t.Fatalf("runs = %+v, want one success", runs)
	}
	if runs[0].TriggerKey != "qc:"+fmtInt(check.ID) {
		t.Errorf("trigger key = %q", runs[0].TriggerKey)
	}
	runs, err = s.EvaluateAutomations(ctx, now.Add(2*time.Minute))
	if err != nil || len(runs) != 0 {
		t.Fatalf("repeat evaluation = %+v, %v; want no runs", runs, err)
	}
	notes, _, err := s.Notifications(ctx, "quality-lead", false, 10)
	if err != nil || len(notes) != 1 {
		t.Fatalf("notifications = %+v, %v", notes, err)
	}
	if notes[0].Kind != "automation" || notes[0].OrderID != o.ID {
		t.Errorf("notification = %+v", notes[0])
	}
	_ = rule
}

func TestAutomationSLAAssignmentAndAgingHold(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "owner")
	slaOrder, _ := s.CreateOrder(ctx, "AUTO-SLA-1", now.Add(time.Hour), now, "tester")
	_, err := s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Assign breached orders", Trigger: automation.TriggerSLABreach, Action: automation.ActionAssign,
		Targets: []string{"owner"}, Enabled: true,
	}, "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.EvaluateAutomations(ctx, now.Add(2*time.Hour))
	if err != nil || len(runs) != 1 {
		t.Fatalf("SLA runs = %+v, %v", runs, err)
	}
	updated, _ := s.GetOrder(ctx, slaOrder.ID)
	if updated.Assignee != "owner" {
		t.Errorf("assignee = %q, want owner", updated.Assignee)
	}

	heldOrder, _ := s.CreateOrder(ctx, "AUTO-HOLD-1", now.Add(48*time.Hour), now, "tester")
	hold, _, err := s.PlaceHold(ctx, heldOrder.ID, "waiting for parts", "tester", now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateAutomationRule(ctx, automation.Rule{
		Name: "Escalate old holds", Trigger: automation.TriggerHoldAging, ThresholdMinutes: 60,
		Action: automation.ActionEscalate, EscalationLevel: "L2", Targets: []string{"owner"}, Enabled: true,
	}, "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	runs, err = s.EvaluateAutomations(ctx, now.Add(59*time.Minute))
	if err != nil || len(runs) != 0 {
		t.Fatalf("early hold runs = %+v, %v", runs, err)
	}
	runs, err = s.EvaluateAutomations(ctx, now.Add(61*time.Minute))
	if err != nil || len(runs) != 1 {
		t.Fatalf("aging hold runs = %+v, %v", runs, err)
	}
	if runs[0].TriggerKey != "hold:"+fmtInt(hold.ID) {
		t.Errorf("hold trigger key = %q", runs[0].TriggerKey)
	}
	escalations, err := s.ListEscalations(ctx, true, 10)
	if err != nil || len(escalations) != 1 {
		t.Fatalf("escalations = %+v, %v", escalations, err)
	}
	if escalations[0].Level != swi.LevelL2 || escalations[0].Reason != swi.ReasonBlocked {
		t.Errorf("escalation = %+v", escalations[0])
	}
	runs, err = s.EvaluateAutomations(ctx, now.Add(62*time.Minute))
	if err != nil || len(runs) != 0 {
		t.Fatalf("repeat runs = %+v, %v", runs, err)
	}
}

func TestAutomationRejectsUnknownTarget(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateAutomationRule(context.Background(), automation.Rule{
		Name: "Bad target", Trigger: automation.TriggerQCFailed, Action: automation.ActionNotify,
		Targets: []string{"missing"}, Enabled: true,
	}, "admin", fixedNow())
	if err == nil {
		t.Fatal("expected unknown target error")
	}
}

func fmtInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
