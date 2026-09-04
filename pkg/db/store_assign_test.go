package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"cockpit/pkg/order"
)

func TestStaleWriteGuard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()

	o, _ := s.CreateOrder(ctx, "ORD-SG", now.Add(24*time.Hour), now, "alice")
	first, err := s.TransitionOrder(ctx, o.ID, string(order.StatusProcessing), "alice", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("transition: %v", err)
	}

	stale := now
	if _, err := s.TransitionOrderGuarded(ctx, o.ID, string(order.StatusQCReview), "alice", &stale, now.Add(2*time.Minute)); !errors.Is(err, ErrStaleWrite) {
		t.Errorf("stale transition = %v, want ErrStaleWrite", err)
	}
	if _, _, err := s.PlaceHoldGuarded(ctx, o.ID, "reason", "bob", &stale, now.Add(2*time.Minute)); !errors.Is(err, ErrStaleWrite) {
		t.Errorf("stale hold = %v, want ErrStaleWrite", err)
	}

	fresh := first.UpdatedAt
	if _, err := s.TransitionOrderGuarded(ctx, o.ID, string(order.StatusQCReview), "alice", &fresh, now.Add(3*time.Minute)); err != nil {
		t.Errorf("fresh guarded transition = %v, want nil", err)
	}
}

func TestSetAssignee(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()

	o, _ := s.CreateOrder(ctx, "ORD-AS", now.Add(24*time.Hour), now, "alice")
	if o.Assignee != "" {
		t.Fatalf("new order should be unassigned, got %q", o.Assignee)
	}

	got, err := s.SetAssignee(ctx, o.ID, "bob", "alice", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if got.Assignee != "bob" {
		t.Errorf("assignee = %q, want bob", got.Assignee)
	}
	reloaded, _ := s.GetOrder(ctx, o.ID)
	if reloaded.Assignee != "bob" {
		t.Errorf("persisted assignee = %q, want bob", reloaded.Assignee)
	}

	if _, err := s.SetAssignee(ctx, o.ID, "", "alice", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	reloaded, _ = s.GetOrder(ctx, o.ID)
	if reloaded.Assignee != "" {
		t.Errorf("assignee after unassign = %q, want empty", reloaded.Assignee)
	}
}

func TestListOrdersFiltered(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()

	a, _ := s.CreateOrder(ctx, "ORD-F1", now.Add(24*time.Hour), now, "alice")
	b, _ := s.CreateOrder(ctx, "ORD-F2", now.Add(48*time.Hour), now, "alice")
	_, _ = s.SetAssignee(ctx, a.ID, "bob", "alice", now)

	got, total, err := s.ListOrdersFiltered(ctx, ListQuery{Assignee: "bob"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != a.ID {
		t.Errorf("bob filter = %v (total %d), want only ORD-F1", got, total)
	}

	got, total, err = s.ListOrdersFiltered(ctx, ListQuery{OnlyUnassigned: true})
	if err != nil || total != 1 || len(got) != 1 || got[0].ID != b.ID {
		t.Errorf("unassigned filter = %v (total %d), want only ORD-F2 (err %v)", got, total, err)
	}

	got, total, err = s.ListOrdersFiltered(ctx, ListQuery{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("paged list: %v", err)
	}
	if total != 2 || len(got) != 1 {
		t.Errorf("paged = %v (total %d), want 1 of 2", got, total)
	}
}

func TestResolveHoldAccumulatesPausedSeconds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()

	o, _ := s.CreateOrder(ctx, "ORD-P", now.Add(24*time.Hour), now, "alice")
	_, _ = s.TransitionOrder(ctx, o.ID, string(order.StatusProcessing), "alice", now)
	h, _, err := s.PlaceHold(ctx, o.ID, "waiting", "bob", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("hold: %v", err)
	}

	if _, _, err := s.ResolveHold(ctx, h.ID, "bob", now.Add(time.Hour+90*time.Minute)); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, _ := s.GetOrder(ctx, o.ID)
	want := int64(90 * 60)
	if got.PausedSeconds != want {
		t.Errorf("pausedSeconds = %d, want %d", got.PausedSeconds, want)
	}
}
