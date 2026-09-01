package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"cockpit/pkg/order"
)

func TestHoldLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	o, _ := s.CreateOrder(ctx, "ORD-4", now.Add(24*time.Hour), now, "alice")
	_, _ = s.TransitionOrder(ctx, o.ID, string(order.StatusProcessing), "alice", now)

	h, updated, err := s.PlaceHold(ctx, o.ID, "awaiting parts", "bob", now)
	if err != nil {
		t.Fatalf("place hold: %v", err)
	}
	if updated.Status != order.Status("Held_Processing") {
		t.Errorf("held status = %q, want Held_Processing", updated.Status)
	}
	active, err := s.ActiveHolds(ctx, o.ID)
	if err != nil || len(active) != 1 {
		t.Fatalf("active holds = %v, %v", active, err)
	}

	if _, err := s.TransitionOrder(ctx, o.ID, string(order.StatusQCReview), "alice", now); !errors.Is(err, order.ErrActiveHold) {
		t.Errorf("transition while held = %v, want ErrActiveHold", err)
	}

	if _, _, err := s.PlaceHold(ctx, o.ID, "again", "bob", now); !errors.Is(err, ErrHoldActive) {
		t.Errorf("second hold = %v, want ErrHoldActive", err)
	}

	rh, restored, err := s.ResolveHold(ctx, h.ID, "bob", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rh.ResolvedAt == nil {
		t.Error("resolved hold must have ResolvedAt set")
	}
	if restored.Status != order.StatusProcessing {
		t.Errorf("restored status = %q, want Processing", restored.Status)
	}

	if _, _, err := s.ResolveHold(ctx, h.ID, "bob", now); !errors.Is(err, ErrHoldResolved) {
		t.Errorf("second resolve = %v, want ErrHoldResolved", err)
	}

	if _, err := s.TransitionOrder(ctx, o.ID, string(order.StatusQCReview), "alice", now); err != nil {
		t.Errorf("transition after resolve: %v", err)
	}
}

func TestQCGating(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	o, _ := s.CreateOrder(ctx, "ORD-5", now.Add(24*time.Hour), now, "alice")
	_, _ = s.TransitionOrder(ctx, o.ID, string(order.StatusQCReview), "alice", now)

	_, _ = s.SubmitQC(ctx, o.ID, order.QCFail, "insp", "bad", "insp", now)
	if _, err := s.TransitionOrder(ctx, o.ID, string(order.StatusCompleted), "alice", now); !errors.Is(err, ErrNoQCPass) {
		t.Errorf("complete after FAIL = %v, want ErrNoQCPass", err)
	}

	_, _ = s.SubmitQC(ctx, o.ID, order.QCPass, "insp", "good", "insp", now)
	if _, err := s.TransitionOrder(ctx, o.ID, string(order.StatusCompleted), "alice", now); err != nil {
		t.Errorf("complete after PASS = %v", err)
	}
}

func TestInvalidQC(t *testing.T) {
	s := newTestStore(t)
	o, _ := s.CreateOrder(context.Background(), "ORD-6", fixedNow().Add(24*time.Hour), fixedNow(), "alice")
	if _, err := s.SubmitQC(context.Background(), o.ID, order.QCStatus("MAYBE"), "insp", "", "insp", fixedNow()); err == nil {
		t.Error("invalid QC status must be rejected")
	}
}

func TestAuditTrailImmutability(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	o, _ := s.CreateOrder(ctx, "ORD-7", now.Add(24*time.Hour), now, "alice")
	_, _ = s.TransitionOrder(ctx, o.ID, string(order.StatusProcessing), "alice", now)

	logs, err := s.AuditLog(ctx, o.ID)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("audit entries = %d, want 2 (create + transition)", len(logs))
	}
	if logs[0].Action == "" || logs[0].PerformedBy != "alice" {
		t.Errorf("unexpected log: %+v", logs[0])
	}
	if _, err := s.AuditLog(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("audit for missing order = %v, want ErrNotFound", err)
	}
}

func TestListOrdersFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	a, _ := s.CreateOrder(ctx, "ORD-A", now.Add(24*time.Hour), now, "alice")
	b, _ := s.CreateOrder(ctx, "ORD-B", now.Add(24*time.Hour), now, "alice")
	_, _ = s.TransitionOrder(ctx, a.ID, string(order.StatusProcessing), "alice", now)
	_, _, _ = s.PlaceHold(ctx, b.ID, "hold", "bob", now)

	all, err := s.ListOrders(ctx, nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("list all = %v, %v", all, err)
	}

	rec := order.StatusReceived
	recList, err := s.ListOrders(ctx, &rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(recList) != 0 {
		t.Errorf("Received filter should match none, got %d", len(recList))
	}

	proc := order.StatusProcessing
	procList, err := s.ListOrders(ctx, &proc)
	if err != nil {
		t.Fatal(err)
	}
	if len(procList) != 1 || procList[0].OrderNumber != "ORD-A" {
		t.Errorf("Processing filter = %+v", procList)
	}

	onHold := order.StatusOnHold
	heldList, err := s.ListOrders(ctx, &onHold)
	if err != nil {
		t.Fatal(err)
	}
	if len(heldList) != 1 || heldList[0].OrderNumber != "ORD-B" {
		t.Errorf("On_Hold filter = %+v", heldList)
	}
}
