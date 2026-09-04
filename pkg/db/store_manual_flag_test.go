package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"cockpit/pkg/order"
)

func seedFlagManual(t *testing.T, s *Store, ctx context.Context, now time.Time) (int64, int64) {
	t.Helper()
	o, err := s.CreateOrder(ctx, "ORD-FLAG", now.Add(24*time.Hour), now, "wkr")
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	p, err := s.CreateProduct(ctx, "PROD-FLAG", "Prod", "", "admin", now)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := s.AddManualBlock(ctx, p.ID, "Check seal", "", "", now); err != nil {
		t.Fatalf("add block: %v", err)
	}
	m, err := s.InstantiateManual(ctx, o.ID, p.ID, "wkr", now)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if _, err := s.AnswerManualBlock(ctx, m.ID, m.Blocks[0].ID, "YES", "wkr", now); err != nil {
		t.Fatalf("answer: %v", err)
	}
	return m.ID, m.Blocks[0].ID
}

func TestFlagManualBlockStore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	mid, bid := seedFlagManual(t, s, ctx, now)

	if _, _, err := s.FlagManualBlock(ctx, mid, bid, true, "", "admin", now); !errors.Is(err, ErrBadFlagReason) {
		t.Fatalf("flag without reason = %v, want ErrBadFlagReason", err)
	}

	if _, _, err := s.FlagManualBlock(ctx, mid, 9999, true, "x", "admin", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("flag unknown block = %v, want ErrNotFound", err)
	}

	b, notified, err := s.FlagManualBlock(ctx, mid, bid, true, "seal missing", "admin", now)
	if err != nil {
		t.Fatalf("flag: %v", err)
	}
	if !b.Flagged || b.FlaggedBy != "admin" || b.FlagReason != "seal missing" || b.FlaggedAt == nil {
		t.Fatalf("flag state wrong: %+v", b)
	}
	if notified != "wkr" {
		t.Fatalf("notified = %q, want wkr", notified)
	}

	m, err := s.OrderManual(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	if m.Flagged != 1 {
		t.Fatalf("manual flagged = %d, want 1", m.Flagged)
	}
	last := m.Log[len(m.Log)-1]
	if last.Action != order.FlagSet || last.Username != "admin" || last.Note != "seal missing" {
		t.Fatalf("flag log entry wrong: %+v", last)
	}

	if _, _, err := s.FlagManualBlock(ctx, mid, bid, true, "still missing", "admin", now); err != nil {
		t.Fatalf("re-flag: %v", err)
	}
	notes, unread, err := s.Notifications(ctx, "wkr", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || unread != 1 {
		t.Fatalf("notifications = %d unread = %d, want 1/1", len(notes), unread)
	}
	if notes[0].Kind != "flag" || notes[0].Body != "admin flagged your step “Check seal”: still missing" {
		t.Fatalf("notification wrong: %+v", notes[0])
	}

	if _, notified, err := s.FlagManualBlock(ctx, mid, bid, false, "", "admin", now); err != nil || notified != "" {
		t.Fatalf("unflag = %v notified %q, want nil/empty", err, notified)
	}
	m, _ = s.OrderManual(ctx, mid)
	if m.Flagged != 0 {
		t.Fatalf("flagged after unflag = %d, want 0", m.Flagged)
	}
	last = m.Log[len(m.Log)-1]
	if last.Action != order.FlagClear {
		t.Fatalf("unflag log entry wrong: %+v", last)
	}

	set, clear := 0, 0
	for _, e := range m.Log {
		switch e.Action {
		case order.FlagSet:
			set++
		case order.FlagClear:
			clear++
		}
	}
	if set != 2 || clear != 1 {
		t.Fatalf("flag history = %d sets / %d clears, want 2/1", set, clear)
	}

	if _, _, err := s.FlagManualBlock(ctx, mid, bid, true, "again", "admin", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AnswerManualBlock(ctx, mid, bid, "YES", "wkr", now); err != nil {
		t.Fatal(err)
	}
	m, _ = s.OrderManual(ctx, mid)
	if m.Flagged != 0 {
		t.Fatalf("flagged after re-answer = %d, want 0", m.Flagged)
	}
}

func TestFlagUnansweredBlockStore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	o, err := s.CreateOrder(ctx, "ORD-F2", now.Add(24*time.Hour), now, "a")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.CreateProduct(ctx, "PROD-F2", "P2", "", "admin", now)
	_, err = s.AddManualBlock(ctx, p.ID, "Step", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.InstantiateManual(ctx, o.ID, p.ID, "a", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.FlagManualBlock(ctx, m.ID, m.Blocks[0].ID, true, "x", "admin", now); !errors.Is(err, ErrFlagUnanswered) {
		t.Fatalf("flag unanswered = %v, want ErrFlagUnanswered", err)
	}
}
