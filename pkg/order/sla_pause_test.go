package order

import (
	"testing"
	"time"
)

func TestComputeSLAPaused(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	target := now.Add(6 * time.Hour)

	if got := ComputeSLAPaused(target, now, 0, nil); got.Status != SLAOnTime {
		t.Errorf("no pause = %s, want ON_TIME", got.Status)
	}

	if got := ComputeSLAPaused(target, now, 3*3600, nil); got.Status != SLAOnTime {
		t.Errorf("3h pause = %s, want ON_TIME", got.Status)
	}

	hs := now.Add(-2 * time.Hour)
	if got := ComputeSLAPaused(target, now, 0, &hs); got.Status != SLAOnTime {
		t.Errorf("running 2h hold = %s, want ON_TIME", got.Status)
	}

	targetOld := now.Add(-24 * time.Hour)
	if got := ComputeSLAPaused(targetOld, now, 26*3600, nil); got.Status != SLAWarning {
		t.Errorf("26h pause on old target = %s, want WARNING (2h effectively left)", got.Status)
	}
	if got := ComputeSLAPaused(targetOld, now, 30*3600, nil); got.Status != SLAOnTime {
		t.Errorf("30h pause on old target = %s, want ON_TIME", got.Status)
	}
	if got := ComputeSLAPaused(targetOld, now, 10*3600, nil); got.Status != SLABreached {
		t.Errorf("10h pause on old target = %s, want BREACHED", got.Status)
	}

	if got := ComputeSLA(targetOld, now); got.Status != SLABreached {
		t.Errorf("plain SLA = %s, want BREACHED", got.Status)
	}
}
