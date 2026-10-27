package order

import (
	"errors"
	"time"
)

// TargetFloor is the earliest target completion the API accepts. Anything
// before it is a typo (a year in the past, a zero time, a truncated date),
// not a genuine "this order is already overdue" registration.
var TargetFloor = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

var (
	// ErrTargetInvalid means the target is zero or not a plausible date.
	ErrTargetInvalid = errors.New("target completion is not a valid date")

	// ErrTargetTooOld means the target predates TargetFloor.
	ErrTargetTooOld = errors.New("target completion is not a plausible date")

	// ErrTargetTooFar means the target is absurdly far in the future.
	ErrTargetTooFar = errors.New("target completion is too far in the future")
)

// TargetHorizon bounds how far ahead a target may be planned. It exists purely
// to catch typos such as 2072 instead of 2027; it is deliberately generous.
const TargetHorizon = 10 * 365 * 24 * time.Hour

// ValidateTargetCompletion checks an explicitly supplied target completion.
//
// A target in the past is intentionally allowed: orders are often registered
// after they arrived (or imported from Omnitracker), and such an order is
// simply already past its SLA — the app renders that as BREACHED. Only
// implausible values are rejected, so a typo cannot silently create an order
// that looks years overdue.
//
// now is the reference point for the upper bound. The return value is one of
// the sentinel errors above (wrapped), or nil.
func ValidateTargetCompletion(target, now time.Time) error {
	if target.IsZero() {
		return ErrTargetInvalid
	}
	if target.Before(TargetFloor) {
		return ErrTargetTooOld
	}
	if target.After(now.Add(TargetHorizon)) {
		return ErrTargetTooFar
	}
	return nil
}

// IsPastTarget reports whether target is already behind now, i.e. the order is
// created (or imported) overdue.
func IsPastTarget(target, now time.Time) bool {
	return !target.IsZero() && target.Before(now)
}
