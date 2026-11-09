package order

import (
	"errors"
	"time"
)

var TargetFloor = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

var (
	ErrTargetInvalid = errors.New("target completion is not a valid date")

	ErrTargetTooOld = errors.New("target completion is not a plausible date")

	ErrTargetTooFar = errors.New("target completion is too far in the future")
)

const TargetHorizon = 10 * 365 * 24 * time.Hour

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

func IsPastTarget(target, now time.Time) bool {
	return !target.IsZero() && target.Before(now)
}
