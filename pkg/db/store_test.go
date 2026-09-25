package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cockpit/pkg/order"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	conn, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return New(conn)
}

func fixedNow() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }

func TestCreateAndGetOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()

	o, err := s.CreateOrder(ctx, "ORD-1", now.Add(24*time.Hour), now, "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if o.Status != order.StatusReceived {
		t.Errorf("status = %q, want Received", o.Status)
	}
	if o.ID == 0 || o.OrderNumber != "ORD-1" {
		t.Errorf("order = %+v", o)
	}

	got, err := s.GetOrder(ctx, o.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrderNumber != "ORD-1" || got.Status != order.StatusReceived {
		t.Errorf("got = %+v", got)
	}

	_, err = s.CreateOrder(ctx, "ORD-1", now.Add(24*time.Hour), now, "alice")
	if !errors.Is(err, ErrDuplicateOrderNumber) {
		t.Errorf("duplicate create = %v, want ErrDuplicateOrderNumber", err)
	}
}

func TestMissingOrder(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetOrder(context.Background(), 42)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("get = %v, want ErrNotFound", err)
	}
}

func TestTransitionValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	o, _ := s.CreateOrder(ctx, "ORD-2", now.Add(24*time.Hour), now, "alice")

	if _, err := s.TransitionOrder(ctx, o.ID, string(order.StatusCompleted), "alice", now); err == nil {
		t.Error("Received->Completed must fail")
	}

	upd, err := s.TransitionOrder(ctx, o.ID, string(order.StatusProcessing), "alice", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if upd.Status != order.StatusProcessing {
		t.Errorf("status = %q, want Processing", upd.Status)
	}

	s2 := newTestStore(t)
	o2, _ := s2.CreateOrder(ctx, "ORD-3", now.Add(24*time.Hour), now, "alice")
	_, _ = s2.TransitionOrder(ctx, o2.ID, string(order.StatusQCReview), "alice", now)
	if _, err := s2.SubmitQC(ctx, o2.ID, order.QCPass, "insp", "", "", "insp", now); err != nil {
		t.Fatalf("qc: %v", err)
	}
	if _, err := s2.TransitionOrder(ctx, o2.ID, string(order.StatusCompleted), "alice", now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := s2.TransitionOrder(ctx, o2.ID, string(order.StatusProcessing), "alice", now); err == nil {
		t.Error("transition out of Completed must fail")
	}
	if _, err := s2.TransitionOrder(ctx, o2.ID, string(order.StatusProcessing), "alice", now); err == nil {
		t.Error("transition out of Completed must fail")
	}
}

func TestSeedDemoSI(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()

	count, err := s.SeedDemoSI(ctx, now)
	if err != nil {
		t.Fatalf("SeedDemoSI failed: %v", err)
	}
	if count != 5 {
		t.Errorf("SeedDemoSI created %d items, want 5", count)
	}

	count2, err := s.SeedDemoSI(ctx, now)
	if err != nil {
		t.Fatalf("SeedDemoSI second call failed: %v", err)
	}
	if count2 != 0 {
		t.Errorf("SeedDemoSI second call created %d items, want 0 (idempotent)", count2)
	}
}
