package order

import (
	"errors"
	"fmt"
	"time"
)

type Status string

const (
	StatusReceived Status = "Received"

	StatusProcessing Status = "Processing"

	StatusQCReview Status = "QC_Review"

	StatusCompleted Status = "Completed"

	StatusOnHold Status = "On_Hold"

	StatusOnHoldPrefix = "Held_"
)

var AllStatuses = []Status{
	StatusReceived,
	StatusProcessing,
	StatusQCReview,
	StatusCompleted,
	StatusOnHold,
}

func IsValidStatus(s Status) bool {
	for _, ok := range AllStatuses {
		if s == ok {
			return true
		}
	}
	return false
}

var underlyingMask = []Status{StatusReceived, StatusProcessing, StatusQCReview}

func ExpandHeld(s Status) []Status {
	if s != StatusOnHold {
		return []Status{s}
	}
	held := make([]Status, 0, len(underlyingMask))
	for _, u := range underlyingMask {
		held = append(held, Status(StatusOnHoldPrefix+string(u)))
	}
	return held
}

func NormalizeHeld(s Status) Status {
	if len(s) > len(StatusOnHoldPrefix) && Status(s[:len(StatusOnHoldPrefix)]) == StatusOnHoldPrefix {
		return Status(s[len(StatusOnHoldPrefix):])
	}
	return s
}

var (
	ErrInvalidTransition = errors.New("invalid status transition for order")

	ErrActiveHold = errors.New("order has an active hold and cannot transition")

	ErrInvalidStatus = errors.New("invalid order status")

	ErrCompletedTransition = errors.New("completed orders are immutable")
)

var transitionTable = map[Status]map[Status]bool{
	StatusReceived: {
		StatusProcessing: true,
		StatusQCReview:   true,
	},
	StatusProcessing: {
		StatusQCReview:  true,
		StatusReceived:  true,
		StatusCompleted: false,
	},
	StatusQCReview: {
		StatusCompleted:  true,
		StatusProcessing: true,
		StatusReceived:   true,
	},
	StatusCompleted: {},
}

func CanTransition(src, dst Status) bool {
	if !IsValidStatus(src) || !IsValidStatus(dst) {
		return false
	}

	if src == StatusCompleted || dst == StatusCompleted {
		return src != StatusCompleted && dst == StatusCompleted && transitionTable[src][dst]
	}
	next, ok := transitionTable[src]
	if !ok {
		return false
	}
	return next[dst]
}

func absoluteStatus(s Status) Status {
	return NormalizeHeld(s)
}

type SLA struct {
	Status    SLAStatus     `json:"status"`
	Target    time.Time     `json:"targetCompletionAt"`
	Remaining time.Duration `json:"remaining"`
}

type SLAStatus string

const (
	SLAOnTime   SLAStatus = "ON_TIME"
	SLAWarning  SLAStatus = "WARNING"
	SLABreached SLAStatus = "BREACHED"
)

func ComputeSLA(target, now time.Time) SLA {
	remaining := target.Sub(now)
	threshold := remaining
	if threshold < 0 {
		threshold = -threshold
	}
	var st SLAStatus
	switch {
	case remaining < 0:
		st = SLABreached
	case threshold <= 4*time.Hour:
		st = SLAWarning
	default:
		st = SLAOnTime
	}
	return SLA{Status: st, Target: target, Remaining: remaining}
}

func DetermineTransition(current Status, activeHold bool, target Status) (Status, error) {
	base := absoluteStatus(current)

	if base == StatusCompleted {
		return "", ErrCompletedTransition
	}
	if activeHold {
		return "", fmt.Errorf("%w: %v", ErrActiveHold, current)
	}
	if target == StatusOnHold {
		return "", fmt.Errorf("%w: On_Hold is set by placing a hold", ErrInvalidTransition)
	}
	if !CanTransition(base, target) {
		return "", fmt.Errorf("%w: %v -> %v", ErrInvalidTransition, base, target)
	}
	return target, nil
}
