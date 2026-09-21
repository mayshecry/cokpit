package swi

import (
	"errors"
	"testing"
	"time"

	"cockpit/pkg/order"
)

func now() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) }

func TestStageValidityAndLabels(t *testing.T) {
	for _, s := range AllStages {
		if !IsValidStage(s) {
			t.Errorf("IsValidStage(%q) = false, want true", s)
		}
		if StageLabel(s) == "" || StageDescription(s) == "" {
			t.Errorf("stage %q misses a label or description", s)
		}
	}
	if IsValidStage("Nonsense") {
		t.Error("IsValidStage(Nonsense) = true, want false")
	}
	if !IsTerminalStage(StageCompleted) {
		t.Error("Completed should be terminal")
	}
	if IsTerminalStage(StageOrderImport) || IsTerminalStage(StageEscalation) {
		t.Error("only Completed is terminal")
	}
	if len(AllStageInfo()) != len(AllStages) {
		t.Errorf("AllStageInfo length = %d, want %d", len(AllStageInfo()), len(AllStages))
	}
}

func TestForwardFlow(t *testing.T) {
	chain := []Stage{
		StageOrderImport,
		StageWorkPreparation,
		StagePointingInWork,
		StageInControlWork,
		StageProcessManagement,
		StageCompleted,
	}
	for i := 0; i+1 < len(chain); i++ {
		from, to := chain[i], chain[i+1]
		if !CanAdvance(from, to) {
			t.Errorf("CanAdvance(%q, %q) = false, want true", from, to)
		}
		if _, err := DetermineAdvance(from, to); err != nil {
			t.Errorf("DetermineAdvance(%q, %q) = %v, want nil", from, to, err)
		}
	}
	if CanAdvance(StageOrderImport, StageCompleted) {
		t.Error("the process must not skip stages")
	}
	if CanAdvance(StageCompleted, StageProcessManagement) {
		t.Error("Completed is immutable")
	}
	if CanAdvance(StageOrderImport, StageOrderImport) {
		t.Error("a stage is not its own successor")
	}
}

func TestEscalationLane(t *testing.T) {
	for _, s := range OperationalStages {
		if !CanAdvance(s, StageEscalation) {
			t.Errorf("CanAdvance(%q, Escalation) = false, want true", s)
		}
	}
	if !CanAdvance(StageEscalation, StagePointingInWork) {
		t.Error("an escalation must be able to release back into the flow")
	}
	if !CanAdvance(StageEscalation, StageProcessManagement) {
		t.Error("an escalation may go straight to process management")
	}
	if CanAdvance(StageEscalation, StageCompleted) {
		t.Error("an escalation must not jump to Completed")
	}
	if !CanAdvance(StagePointingInWork, StageWorkPreparation) {
		t.Error("rework must be possible")
	}
}

func TestDetermineAdvanceErrors(t *testing.T) {
	if _, err := DetermineAdvance(Stage("Bogus"), StageWorkPreparation); !errors.Is(err, ErrInvalidStage) {
		t.Errorf("err = %v, want ErrInvalidStage", err)
	}
	if _, err := DetermineAdvance(StageOrderImport, StageCompleted); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestNextStages(t *testing.T) {
	got := NextStages(StageOrderImport)
	want := map[Stage]bool{StageWorkPreparation: true, StageEscalation: true}
	if len(got) != len(want) {
		t.Fatalf("NextStages(OrderImport) = %v, want 2 entries", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected next stage %q", s)
		}
	}
	if len(NextStages(StageCompleted)) != 0 {
		t.Error("Completed has no next stages")
	}
}

func TestSuggestOrderStatus(t *testing.T) {
	cases := map[Stage]order.Status{
		StageOrderImport:       order.StatusReceived,
		StageWorkPreparation:   order.StatusProcessing,
		StagePointingInWork:    order.StatusProcessing,
		StageInControlWork:     order.StatusQCReview,
		StageEscalation:        order.StatusQCReview,
		StageProcessManagement: order.StatusQCReview,
		StageCompleted:         order.StatusCompleted,
	}
	for stage, want := range cases {
		if got := SuggestOrderStatus(stage); got != want {
			t.Errorf("SuggestOrderStatus(%q) = %q, want %q", stage, got, want)
		}
	}
}
