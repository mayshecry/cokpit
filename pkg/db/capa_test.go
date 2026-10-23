package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"cockpit/pkg/capa"
	"cockpit/pkg/order"
)

func TestCAPALifecycleVerificationAndHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "creator")
	createAutomationUser(t, s, "owner")
	o, err := s.CreateOrder(ctx, "CAPA-1", now.Add(72*time.Hour), now, "creator")
	if err != nil {
		t.Fatal(err)
	}
	check, err := s.SubmitQC(ctx, o.ID, order.QCFail, "creator", "wrong firmware", "Firmware not at baseline", "creator", now)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateCAPA(ctx, capa.CreateRequest{
		OrderID: o.ID, QCCheckID: &check.ID, Title: "Prevent firmware mismatch", Owner: "owner",
		DueAt: now.Add(48 * time.Hour),
	}, "creator", now)
	if err != nil {
		t.Fatalf("create CAPA: %v", err)
	}
	if created.Status != capa.StatusOpen || created.FailReason != "Firmware not at baseline" {
		t.Errorf("created = %+v", created)
	}
	notes, _, err := s.Notifications(ctx, "owner", false, 10)
	if err != nil || len(notes) != 1 || notes[0].Kind != "capa" {
		t.Fatalf("owner notifications = %+v, %v", notes, err)
	}
	if _, err := s.VerifyCAPA(ctx, created.ID, "checked", "creator", now); !errors.Is(err, capa.ErrInvalidTransition) {
		t.Fatalf("early verification error = %v", err)
	}
	pending := capa.StatusPendingVerification
	if _, err := s.UpdateCAPA(ctx, created.ID, capa.UpdateRequest{Status: &pending}, "owner", now); !errors.Is(err, capa.ErrValidation) {
		t.Fatalf("incomplete verification request error = %v", err)
	}
	root, plan, resolution := "Unversioned image", "Pin the release image", "Reimaged and reran self-test"
	inProgress := capa.StatusInProgress
	if _, err := s.UpdateCAPA(ctx, created.ID, capa.UpdateRequest{
		Status: &inProgress, RootCause: &root, ActionPlan: &plan,
	}, "owner", now.Add(time.Hour)); err != nil {
		t.Fatalf("start CAPA: %v", err)
	}
	updated, err := s.UpdateCAPA(ctx, created.ID, capa.UpdateRequest{
		Status: &pending, Resolution: &resolution,
	}, "owner", now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("submit for verification: %v", err)
	}
	if updated.Status != capa.StatusPendingVerification {
		t.Errorf("status = %s", updated.Status)
	}
	closed, err := s.VerifyCAPA(ctx, created.ID, "Verified on a second device", "creator", now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if closed.Status != capa.StatusClosed || closed.VerifiedBy != "creator" || closed.VerifiedAt == nil {
		t.Errorf("closed = %+v", closed)
	}
	if _, err := s.UpdateCAPA(ctx, created.ID, capa.UpdateRequest{RootCause: &root}, "owner", now); !errors.Is(err, capa.ErrInvalidTransition) {
		t.Errorf("closed update error = %v", err)
	}
	events, err := s.CAPAEvents(ctx, created.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("events = %+v, %v; want four", events, err)
	}
	summary, err := s.CAPASummary(ctx, now.Add(4*time.Hour))
	if err != nil || summary.Closed != 1 || summary.Overdue != 0 {
		t.Errorf("summary = %+v, %v", summary, err)
	}
	audit, err := s.AuditLog(ctx, o.ID)
	if err != nil || len(audit) < 5 {
		t.Errorf("audit entries = %d, %v", len(audit), err)
	}
}

func TestCAPARejectsMismatchedQCCheck(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := fixedNow()
	createAutomationUser(t, s, "owner")
	first, _ := s.CreateOrder(ctx, "CAPA-A", now.Add(time.Hour), now, "tester")
	second, _ := s.CreateOrder(ctx, "CAPA-B", now.Add(time.Hour), now, "tester")
	check, _ := s.SubmitQC(ctx, first.ID, order.QCFail, "qc", "", "reason", "qc", now)
	_, err := s.CreateCAPA(ctx, capa.CreateRequest{
		OrderID: second.ID, QCCheckID: &check.ID, Title: "Wrong link", Owner: "owner", DueAt: now.Add(time.Hour),
	}, "creator", now)
	if !errors.Is(err, capa.ErrValidation) {
		t.Fatalf("mismatched check error = %v", err)
	}
}
