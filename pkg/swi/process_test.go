package swi

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cockpit/pkg/order"
)

func TestPlanSyncCoversEverySystem(t *testing.T) {
	seen := map[System]bool{}
	for _, s := range AllSystems {
		seen[s] = false
	}
	for _, from := range OperationalStages {
		for _, to := range AllStages {
			for _, p := range PlanSync(from, to) {
				seen[p.System] = true
				if !IsValidOperation(p.Operation) {
					t.Errorf("plan %s -> %s has invalid operation %q", from, to, p.Operation)
				}
				if p.Reason == "" || p.Entity == "" {
					t.Errorf("plan %s -> %s misses a reason or entity", from, to)
				}
			}
		}
	}
	for system, ok := range seen {
		if !ok {
			t.Errorf("system %q is never planned by any transition", system)
		}
	}
}

func TestPlanSyncOrderImportToPreparation(t *testing.T) {
	plans := PlanSync(StageOrderImport, StageWorkPreparation)
	if len(plans) != 2 {
		t.Fatalf("len(plans) = %d, want 2", len(plans))
	}
	if plans[0].System != SystemAFAS || plans[0].Direction != DirectionInbound {
		t.Errorf("first plan = %+v, want the inbound AFAS import", plans[0])
	}
	if plans[1].System != SystemOmnitracker || plans[1].Operation != OpCreate {
		t.Errorf("second plan = %+v, want the Omnitracker ticket creation", plans[1])
	}
}

func TestPlanSyncPointingInWorkEnrollsDevices(t *testing.T) {
	plans := PlanSync(StagePointingInWork, StageInControlWork)
	got := map[System]bool{}
	for _, p := range plans {
		got[p.System] = true
		if p.System != SystemOmnitracker && p.Operation != OpCreate {
			t.Errorf("device enrolment for %s should be CREATE, got %s", p.System, p.Operation)
		}
	}
	for _, want := range []System{SystemIntune, SystemKnox, SystemAppleBusinessManager, SystemOmnitracker} {
		if !got[want] {
			t.Errorf("plan misses system %q", want)
		}
	}
}

func TestPlanSyncEscalationAndRework(t *testing.T) {
	enter := PlanSync(StageInControlWork, StageEscalation)
	if len(enter) != 1 || enter[0].System != SystemOmnitracker || enter[0].Operation != OpUpdate {
		t.Errorf("escalation plan = %+v, want a single Omnitracker update", enter)
	}
	leave := PlanSync(StageEscalation, StageWorkPreparation)
	if len(leave) != 1 || leave[0].System != SystemOmnitracker {
		t.Errorf("release plan = %+v, want a single Omnitracker update", leave)
	}
	rework := PlanSync(StagePointingInWork, StageWorkPreparation)
	if len(rework) != 1 || rework[0].System != SystemOmnitracker {
		t.Errorf("rework plan = %+v, want a single Omnitracker update", rework)
	}
	if PlanSync(StageCompleted, StageProcessManagement) != nil {
		t.Error("an invalid transition must not plan any sync")
	}
}

func TestJobKeyIsStableAndUnique(t *testing.T) {
	p := PlannedSync{System: SystemAFAS, Operation: OpUpdate}
	a := JobKey(42, StageOrderImport, StageWorkPreparation, 0, p)
	b := JobKey(42, StageOrderImport, StageWorkPreparation, 0, p)
	if a != b {
		t.Errorf("JobKey is not stable: %q vs %q", a, b)
	}
	if c := JobKey(42, StageOrderImport, StageWorkPreparation, 1, p); c == a {
		t.Errorf("JobKey does not differ per index: %q", c)
	}
	if d := JobKey(43, StageOrderImport, StageWorkPreparation, 0, p); d == a {
		t.Errorf("JobKey does not differ per order: %q", d)
	}
}

func TestSyncJobLabelsAndJSON(t *testing.T) {
	for _, s := range []SyncStatus{SyncPending, SyncInProgress, SyncDone, SyncFailed, SyncSkipped} {
		if !IsValidSyncStatus(s) || SyncStatusLabel(s) == "" {
			t.Errorf("sync status %q is not fully defined", s)
		}
	}
	if IsValidSyncStatus("WAT") {
		t.Error("IsValidSyncStatus(WAT) = true, want false")
	}
	job := SyncJob{ID: 1, System: SystemKnox, Payload: json.RawMessage(`{"device":"A1"}`)}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(raw) {
		t.Errorf("job json invalid: %s", raw)
	}
	if SystemLabel(SystemAppleBusinessManager) != "Apple Business Manager" {
		t.Error("ABM label should be readable")
	}
	for _, s := range AllSystems {
		if !IsValidSystem(s) {
			t.Errorf("IsValidSystem(%q) = false, want true", s)
		}
	}
	if IsValidSystem("SAP") {
		t.Error("IsValidSystem(SAP) = true, want false")
	}
}

func TestEscalationLevelsAndReasons(t *testing.T) {
	if SuggestedLevel(order.SLAOnTime, 0) != LevelL1 {
		t.Error("an on-time order escalates at L1")
	}
	if SuggestedLevel(order.SLABreached, 30*time.Minute) != LevelL2 {
		t.Error("a fresh breach escalates at L2")
	}
	if SuggestedLevel(order.SLABreached, 5*time.Hour) != LevelL3 {
		t.Error("a long breach escalates at L3")
	}
	for _, r := range AllReasons {
		if !IsValidReason(r) || ReasonLabel(r) == "" {
			t.Errorf("reason %q is not fully defined", r)
		}
	}
	for _, l := range AllLevels {
		if !IsValidLevel(l) || LevelLabel(l) == "" {
			t.Errorf("level %q is not fully defined", l)
		}
	}
}

func TestNeedsEscalation(t *testing.T) {
	base := now()
	o := order.Order{ID: 1, OrderNumber: "ORD-1", TargetCompletion: base.Add(time.Hour)}
	proc := Process{OrderID: 1, Stage: StageWorkPreparation, StageSince: base}

	if _, _, ok := NeedsEscalation(o, proc, order.ComputeSLA(o.TargetCompletion, base), base); ok {
		t.Error("a healthy order must not escalate")
	}

	breached := order.ComputeSLA(o.TargetCompletion, base.Add(30*time.Hour))
	level, reason, ok := NeedsEscalation(o, proc, breached, base.Add(30*time.Hour))
	if !ok {
		t.Fatal("a breached SLA must escalate")
	}
	if level != LevelL3 || reason != ReasonSLABreach {
		t.Errorf("level/reason = %s/%s, want L3/SLA_BREACH", level, reason)
	}

	stalled := Process{OrderID: 1, Stage: StageWorkPreparation, StageSince: base.Add(-9 * time.Hour)}
	level, reason, ok = NeedsEscalation(o, stalled, order.ComputeSLA(o.TargetCompletion, base), base)
	if !ok || level != LevelL1 || reason != ReasonStageStalled {
		t.Errorf("stalled = %s/%s/%v, want L1/STAGE_STALLED/true", level, reason, ok)
	}

	if _, _, ok := NeedsEscalation(o, Process{Stage: StageCompleted}, breached, base); ok {
		t.Error("a completed process must not escalate")
	}
	if _, _, ok := NeedsEscalation(o, Process{Stage: StageWorkPreparation, Escalated: true}, breached, base); ok {
		t.Error("an escalated process must not escalate twice")
	}
}

func TestEscalationValidation(t *testing.T) {
	good := EscalateRequest{Level: LevelL2, Reason: ReasonQuality}
	if err := ValidateEscalation(good); err != nil {
		t.Errorf("valid escalation rejected: %v", err)
	}
	if err := ValidateEscalation(EscalateRequest{Level: "L9", Reason: ReasonQuality}); !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
	if err := ValidateEscalation(EscalateRequest{Level: LevelL1, Reason: "NOPE"}); !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestStageTasksAndProgress(t *testing.T) {
	for _, s := range OperationalStages {
		tasks := StageTasks(s)
		if len(tasks) == 0 {
			t.Errorf("stage %q has no work instructions", s)
		}
		if len(StageTasks(s)) != len(tasks) {
			t.Errorf("StageTasks(%q) should return a copy", s)
		}
		for _, tpl := range tasks {
			if tpl.Seq == 0 || tpl.Title == "" || tpl.Role == "" {
				t.Errorf("stage %q has an incomplete template: %+v", s, tpl)
			}
		}
	}
	if len(StageTasks(StageCompleted)) != 0 {
		t.Error("Completed has no work instructions")
	}
	tasks := []Task{{Required: true, Done: true}, {Required: true}, {Done: true}}
	total, done, open := TaskProgress(tasks)
	if total != 3 || done != 2 || open != 1 {
		t.Errorf("progress = %d/%d open=%d, want 3/2 open=1", total, done, open)
	}
}

func TestBuildBoardPlacesEscalatedWorkInTheEscalationLane(t *testing.T) {
	procs := []Process{
		{OrderID: 1, Stage: StageOrderImport, StageSince: now()},
		{OrderID: 2, Stage: StageWorkPreparation, StageSince: now(), Escalated: true, EscalationLevel: LevelL2},
		{OrderID: 3, Stage: StageEscalation, StageSince: now(), Escalated: true},
		{OrderID: 4, Stage: StageCompleted, StageSince: now()},
	}
	board := BuildBoard(now(), procs, Metrics{Total: len(procs)})
	if len(board.Lanes) != len(AllStages) {
		t.Fatalf("lanes = %d, want %d", len(board.Lanes), len(AllStages))
	}
	find := func(s Stage) Lane {
		for _, l := range board.Lanes {
			if l.Stage == s {
				return l
			}
		}
		t.Fatalf("lane %q missing", s)
		return Lane{}
	}
	if got := find(StageEscalation).Count; got != 2 {
		t.Errorf("escalation lane count = %d, want 2", got)
	}
	if got := find(StageWorkPreparation).Count; got != 0 {
		t.Errorf("work preparation lane count = %d, want 0 (escalated work moves lanes)", got)
	}
	if got := find(StageCompleted).Count; got != 1 {
		t.Errorf("completed lane count = %d, want 1", got)
	}
	if board.GeneratedAt.IsZero() {
		t.Error("board must carry a generation timestamp")
	}
}

func TestRequestValidation(t *testing.T) {
	if err := ValidateAdvance(AdvanceRequest{Stage: StagePointingInWork}); err != nil {
		t.Errorf("valid advance rejected: %v", err)
	}
	if err := ValidateAdvance(AdvanceRequest{Stage: StageCompleted}); !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
	if err := ValidateAdvance(AdvanceRequest{Stage: "Bogus"}); !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}

	if err := ValidateImport(ImportRequest{OrderNumber: "ORD-9", DebitNumber: "94828"}); err != nil {
		t.Errorf("valid import rejected: %v", err)
	}
	if err := ValidateImport(ImportRequest{DebitNumber: "94828"}); !errors.Is(err, ErrValidation) {
		t.Errorf("import without order number: err = %v, want ErrValidation", err)
	}
	if err := ValidateImport(ImportRequest{OrderNumber: "ORD-9"}); !errors.Is(err, ErrValidation) {
		t.Errorf("import without AFAS data: err = %v, want ErrValidation", err)
	}

	if err := ValidateComplete(CompleteSyncRequest{Status: SyncDone}); err != nil {
		t.Errorf("valid completion rejected: %v", err)
	}
	if err := ValidateComplete(CompleteSyncRequest{Status: SyncPending}); !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}
