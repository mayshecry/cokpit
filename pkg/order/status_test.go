package order

import (
	"testing"
	"time"
)

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from, to Status
		want     bool
	}{

		{StatusReceived, StatusProcessing, true},
		{StatusReceived, StatusQCReview, true},
		{StatusProcessing, StatusQCReview, true},
		{StatusQCReview, StatusCompleted, true},

		{StatusProcessing, StatusReceived, true},
		{StatusQCReview, StatusProcessing, true},
		{StatusQCReview, StatusReceived, true},

		{StatusReceived, StatusCompleted, false},
		{StatusProcessing, StatusCompleted, false},
		{StatusReceived, StatusReceived, false},
		{StatusCompleted, StatusReceived, false},
		{StatusCompleted, StatusProcessing, false},

		{Status("Bogus"), StatusProcessing, false},
		{"", StatusProcessing, false},
	}
	for _, tc := range tests {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("CanTransition(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestDetermineTransition(t *testing.T) {

	if _, err := DetermineTransition(StatusReceived, false, StatusProcessing); err != nil {
		t.Errorf("legal transition errored: %v", err)
	}
	if _, err := DetermineTransition(StatusReceived, false, StatusCompleted); err == nil {
		t.Error("Received->Completed must be rejected")
	}

	if _, err := DetermineTransition(StatusProcessing, true, StatusQCReview); err == nil {
		t.Error("transition while held must be rejected")
	}

	if got, err := DetermineTransition(Status("Held_Processing"), false, StatusQCReview); err != nil || got != StatusQCReview {
		t.Errorf("held transition = %v, %v; want QC_Review, nil", got, err)
	}

	if _, err := DetermineTransition(StatusCompleted, false, StatusProcessing); err == nil {
		t.Error("transition out of Completed must be rejected")
	}

	if _, err := DetermineTransition(StatusReceived, false, StatusOnHold); err == nil {
		t.Error("On_Hold must not be accepted as a transition target")
	}
}

func TestHoldNormalization(t *testing.T) {
	variants := ExpandHeld(StatusOnHold)
	if len(variants) != 3 {
		t.Fatalf("ExpandHeld(On_Hold) = %v, want 3 variants", variants)
	}
	for _, v := range variants {
		base := NormalizeHeld(v)
		if base == StatusOnHold {
			t.Errorf("NormalizeHeld(%q) must not return On_Hold", v)
		}
		if IsValidStatus(v) {
			t.Errorf("variant %q must not be a bare base status", v)
		}
		if !IsValidStatus(base) {
			t.Errorf("base %q of %q must be a valid progress status", base, v)
		}
	}
	if got := NormalizeHeld(Status("Held_Processing")); got != StatusProcessing {
		t.Errorf("NormalizeHeld(Held_Processing) = %q, want Processing", got)
	}
	if got := NormalizeHeld(StatusProcessing); got != StatusProcessing {
		t.Errorf("NormalizeHeld(Processing) = %q, want itself", got)
	}
	if got := ExpandHeld(StatusProcessing); len(got) != 1 || got[0] != StatusProcessing {
		t.Errorf("ExpandHeld(Processing) = %v, want [Processing]", got)
	}
}

func TestComputeSLA(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	if s := ComputeSLA(now.Add(-1*time.Minute), now); s.Status != SLABreached {
		t.Errorf("past target = %s, want BREACHED", s.Status)
	}
	if s := ComputeSLA(now.Add(2*time.Hour), now); s.Status != SLAWarning {
		t.Errorf("2h target = %s, want WARNING", s.Status)
	}
	if s := ComputeSLA(now.Add(24*time.Hour), now); s.Status != SLAOnTime {
		t.Errorf("24h target = %s, want ON_TIME", s.Status)
	}
	if s := ComputeSLA(now.Add(-2*time.Hour), now); s.Remaining >= 0 {
		t.Errorf("remaining must be negative when breached, got %v", s.Remaining)
	}
}

func TestStatusValidation(t *testing.T) {
	for _, s := range AllStatuses {
		if !IsValidStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	if IsValidStatus("Bogus") || IsValidStatus("Held_Processing") {
		t.Error("Bogus / Held_* must not be accepted as base statuses")
	}
}
