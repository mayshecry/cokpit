package order

import (
	"errors"
	"testing"
	"time"
)

func TestValidateTargetCompletion(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		target time.Time
		want   error
	}{
		{"five minutes ago", now.Add(-5 * time.Minute), nil},
		{"three days ago", now.Add(-72 * time.Hour), nil},
		{"two years ago", now.AddDate(-2, 0, 0), nil},
		{"exactly now", now, nil},
		{"in an hour", now.Add(time.Hour), nil},
		{"in nine years", now.AddDate(9, 0, 0), nil},
		{"zero value", time.Time{}, ErrTargetInvalid},
		{"1999-12-31", time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC), ErrTargetTooOld},
		{"year 1 (zero-ish parse)", time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), ErrTargetInvalid},
		{"year 2999", time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC), ErrTargetTooFar},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTargetCompletion(c.target, now)
			if c.want == nil {
				if err != nil {
					t.Fatalf("ValidateTargetCompletion(%v) = %v, want nil", c.target, err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("ValidateTargetCompletion(%v) = %v, want %v", c.target, err, c.want)
			}
		})
	}
}

func TestValidateTargetCompletionIsNotFutureOnly(t *testing.T) {
	now := time.Now().UTC()
	if err := ValidateTargetCompletion(now.Add(-time.Hour), now); err != nil {
		t.Fatalf("a past target must be accepted, got %v", err)
	}
}

func TestIsPastTarget(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if !IsPastTarget(now.Add(-time.Second), now) {
		t.Error("target one second ago should be past")
	}
	if IsPastTarget(now, now) {
		t.Error("target exactly now is not past")
	}
	if IsPastTarget(now.Add(time.Second), now) {
		t.Error("target in the future is not past")
	}
	if IsPastTarget(time.Time{}, now) {
		t.Error("zero target must not be reported as past")
	}
}
