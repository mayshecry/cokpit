package attention

import (
	"sort"
	"time"

	"cockpit/pkg/order"
)

type Reason string

const (
	ReasonSLABreached Reason = "SLA_BREACHED"
	ReasonSLAWarning  Reason = "SLA_WARNING"
	ReasonOnHold      Reason = "ON_HOLD"
	ReasonAwaitingQC  Reason = "AWAITING_QC"
	ReasonFresh       Reason = "FRESH"
)

type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

func (s Severity) rank() int {
	switch s {
	case SeverityHigh:
		return 0
	case SeverityMedium:
		return 1
	default:
		return 2
	}
}

type Card struct {
	OrderID      int64     `json:"orderId"`
	OrderNumber  string    `json:"orderNumber"`
	Status       string    `json:"status"`
	Reason       Reason    `json:"reason"`
	Severity     Severity  `json:"severity"`
	ReasonDetail string    `json:"reasonDetail"`
	TargetAt     time.Time `json:"targetAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	HoldCount    int       `json:"holdCount"`
}

type Projection struct {
	Cards      []Card `json:"cards"`
	SLABreach  int    `json:"slaBreached"`
	SLAWarning int    `json:"slaWarning"`
	OnHold     int    `json:"onHold"`
	AwaitingQC int    `json:"awaitingQc"`
}

const freshWindow = 2 * time.Hour

func Project(now time.Time, orders []order.Order, holds []order.Hold) Projection {
	holdCount := make(map[int64]int)
	for _, h := range holds {
		if h.ResolvedAt == nil {
			holdCount[h.OrderID]++
		}
	}

	p := Projection{Cards: []Card{}}
	for _, o := range orders {
		card, ok := evaluate(now, o, holdCount[o.ID])
		if !ok {
			continue
		}
		switch card.Reason {
		case ReasonSLABreached:
			p.SLABreach++
		case ReasonSLAWarning:
			p.SLAWarning++
		case ReasonOnHold:
			p.OnHold++
		case ReasonAwaitingQC:
			p.AwaitingQC++
		}
		p.Cards = append(p.Cards, card)
	}

	sort.SliceStable(p.Cards, func(i, j int) bool {
		if ri, rj := p.Cards[i].Severity.rank(), p.Cards[j].Severity.rank(); ri != rj {
			return ri < rj
		}
		return p.Cards[i].TargetAt.Before(p.Cards[j].TargetAt)
	})
	return p
}

func evaluate(now time.Time, o order.Order, holds int) (Card, bool) {
	sla := order.ComputeSLA(o.TargetCompletion, now)

	if o.Status == order.StatusCompleted {
		return Card{}, false
	}

	card := Card{
		OrderID:     o.ID,
		OrderNumber: o.OrderNumber,
		Status:      string(o.Status),
		TargetAt:    o.TargetCompletion,
		UpdatedAt:   o.UpdatedAt,
		HoldCount:   holds,
	}

	if holds > 0 {
		card.Reason = ReasonOnHold
		card.Severity = SeverityHigh
		card.ReasonDetail = plural(holds, "active hold")
		return card, true
	}

	switch sla.Status {
	case order.SLABreached:
		card.Reason = ReasonSLABreached
		card.Severity = SeverityHigh
		card.ReasonDetail = "target passed " + rel(now.Sub(o.TargetCompletion))
		return card, true
	case order.SLAWarning:
		card.Reason = ReasonSLAWarning
		card.Severity = SeverityMedium
		card.ReasonDetail = rel(-sla.Remaining) + " to target"
		return card, true
	}

	if o.Status == order.StatusQCReview {
		card.Reason = ReasonAwaitingQC
		card.Severity = SeverityLow
		card.ReasonDetail = "waiting for QC decision"
		return card, true
	}

	if now.Sub(o.CreatedAt) < freshWindow && o.Status == order.StatusReceived {
		card.Reason = ReasonFresh
		card.Severity = SeverityLow
		card.ReasonDetail = "new order, not yet started"
		return card, true
	}

	return Card{}, false
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func rel(d time.Duration) string {
	neg := d < 0
	if neg {
		d = -d
	}
	var out string
	switch {
	case d >= 24*time.Hour:
		out = plural(int(d/(24*time.Hour)), "day")
	case d >= time.Hour:
		out = plural(int(d/time.Hour), "hour")
	case d >= time.Minute:
		out = plural(int(d/time.Minute), "minute")
	default:
		out = plural(int(d/time.Second), "second")
	}
	if neg {
		return out + " ago"
	}
	return out
}
