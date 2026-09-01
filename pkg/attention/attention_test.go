package attention

import (
	"reflect"
	"testing"
	"time"

	"cockpit/pkg/order"
)

func at(hours float64) time.Time {
	return fixedNow().Add(time.Duration(hours * float64(time.Hour)))
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
}

func mkOrder(id int64, number string, status order.Status, target, created time.Time) order.Order {
	return order.Order{
		ID:               id,
		OrderNumber:      number,
		Status:           status,
		TargetCompletion: target,
		CreatedAt:        created,
		UpdatedAt:        created,
	}
}

func TestProjectEmpty(t *testing.T) {
	p := Project(fixedNow(), nil, nil)
	if len(p.Cards) != 0 || p.SLABreach != 0 || p.SLAWarning != 0 || p.OnHold != 0 || p.AwaitingQC != 0 {
		t.Fatalf("empty projection = %+v", p)
	}
}

func TestProjectSeveritiesAndReasons(t *testing.T) {
	now := fixedNow()
	orders := []order.Order{
		mkOrder(1, "ORD-BREACH", order.StatusProcessing, at(-3), at(-27)),
		mkOrder(2, "ORD-WARN", order.StatusProcessing, at(2), at(-22)),
		mkOrder(3, "ORD-HOLD", order.StatusQCReview, at(20), at(-4)),
		mkOrder(4, "ORD-QC", order.StatusQCReview, at(30), at(-2)),
		mkOrder(5, "ORD-FRESH", order.StatusReceived, at(30), at(-1)),
		mkOrder(6, "ORD-DONE", order.StatusCompleted, at(-1), at(-25)),
		mkOrder(7, "ORD-CALM", order.StatusProcessing, at(30), at(-2)),
	}
	holds := []order.Hold{
		{ID: 10, OrderID: 3, ResolvedAt: nil, CreatedAt: at(-1)},
		{ID: 11, OrderID: 3, ResolvedAt: nil, CreatedAt: at(-2)},
		{ID: 12, OrderID: 6, ResolvedAt: atPtr(at(-1)), CreatedAt: at(-2)},
	}

	p := Project(now, orders, holds)

	if p.SLABreach != 1 || p.SLAWarning != 1 || p.OnHold != 1 || p.AwaitingQC != 1 {
		t.Fatalf("counters = breach:%d warn:%d hold:%d qc:%d",
			p.SLABreach, p.SLAWarning, p.OnHold, p.AwaitingQC)
	}

	var gotOrder []string
	for _, c := range p.Cards {
		gotOrder = append(gotOrder, c.OrderNumber)
		if c.HoldCount > 0 && c.Reason != ReasonOnHold {
			t.Errorf("%s: reason = %s, want ON_HOLD when holds active", c.OrderNumber, c.Reason)
		}
	}
	wantOrder := []string{"ORD-BREACH", "ORD-HOLD", "ORD-WARN", "ORD-QC", "ORD-FRESH"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("card order = %v, want %v", gotOrder, wantOrder)
	}

	if p.Cards[0].Severity != SeverityHigh || p.Cards[0].Reason != ReasonSLABreached {
		t.Errorf("top card = %+v", p.Cards[0])
	}

	byNum := map[string]Card{}
	for _, c := range p.Cards {
		byNum[c.OrderNumber] = c
	}
	if c := byNum["ORD-HOLD"]; c.HoldCount != 2 || c.Reason != ReasonOnHold || c.Severity != SeverityHigh {
		t.Errorf("hold card = %+v", c)
	}
	if c := byNum["ORD-DONE"]; c.OrderID != 0 {
		t.Errorf("completed order must not appear: %+v", c)
	}
	if c := byNum["ORD-CALM"]; c.OrderID != 0 {
		t.Errorf("healthy order must not appear: %+v", c)
	}
	if c := byNum["ORD-FRESH"]; c.Reason != ReasonFresh || c.Severity != SeverityLow {
		t.Errorf("fresh card = %+v", c)
	}
	if c := byNum["ORD-BREACH"]; c.Severity != SeverityHigh || c.Reason != ReasonSLABreached {
		t.Errorf("breach card = %+v", c)
	}
	if c := byNum["ORD-WARN"]; c.Severity != SeverityMedium || c.Reason != ReasonSLAWarning {
		t.Errorf("warn card = %+v", c)
	}
	if c := byNum["ORD-QC"]; c.Reason != ReasonAwaitingQC || c.Severity != SeverityLow {
		t.Errorf("qc card = %+v", c)
	}
}

func TestProjectBreachBeatsHoldWhenNoHolds(t *testing.T) {
	now := fixedNow()
	orders := []order.Order{
		mkOrder(9, "ORD-X", order.StatusReceived, at(-10), at(-34)),
	}
	p := Project(now, orders, nil)
	if len(p.Cards) != 1 || p.Cards[0].Reason != ReasonSLABreached {
		t.Fatalf("cards = %+v", p.Cards)
	}
	if p.Cards[0].Severity != SeverityHigh {
		t.Errorf("breached severity = %v, want high", p.Cards[0].Severity)
	}
}

func TestFreshWindowBoundary(t *testing.T) {
	now := fixedNow()
	inWindow := []order.Order{mkOrder(1, "ORD-IN", order.StatusReceived, at(24), now.Add(-freshWindow+time.Minute))}
	if p := Project(now, inWindow, nil); len(p.Cards) != 1 {
		t.Errorf("order inside fresh window should appear, got %+v", p)
	}
	outside := []order.Order{mkOrder(2, "ORD-OUT", order.StatusReceived, at(24), now.Add(-freshWindow-time.Minute))}
	if p := Project(now, outside, nil); len(p.Cards) != 0 {
		t.Errorf("order outside fresh window should not appear, got %+v", p)
	}
}

func atPtr(t time.Time) *time.Time { return &t }
