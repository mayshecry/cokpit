package capa

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusOpen                Status = "OPEN"
	StatusInProgress          Status = "IN_PROGRESS"
	StatusPendingVerification Status = "PENDING_VERIFICATION"
	StatusClosed              Status = "CLOSED"
)

var (
	ErrValidation        = errors.New("invalid corrective action")
	ErrInvalidTransition = errors.New("invalid corrective action transition")
)

type CorrectiveAction struct {
	ID               int64      `json:"id"`
	OrderID          int64      `json:"orderId"`
	OrderNumber      string     `json:"orderNumber"`
	QCCheckID        *int64     `json:"qcCheckId,omitempty"`
	Title            string     `json:"title"`
	FailReason       string     `json:"failReason,omitempty"`
	Owner            string     `json:"owner"`
	DueAt            time.Time  `json:"dueAt"`
	RootCause        string     `json:"rootCause,omitempty"`
	ActionPlan       string     `json:"actionPlan,omitempty"`
	Resolution       string     `json:"resolution,omitempty"`
	Status           Status     `json:"status"`
	CreatedBy        string     `json:"createdBy"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	VerifiedBy       string     `json:"verifiedBy,omitempty"`
	VerifiedAt       *time.Time `json:"verifiedAt,omitempty"`
	VerificationNote string     `json:"verificationNote,omitempty"`
	Overdue          bool       `json:"overdue"`
}

type CreateRequest struct {
	OrderID    int64     `json:"orderId"`
	QCCheckID  *int64    `json:"qcCheckId,omitempty"`
	Title      string    `json:"title"`
	FailReason string    `json:"failReason"`
	Owner      string    `json:"owner"`
	DueAt      time.Time `json:"dueAt"`
	RootCause  string    `json:"rootCause"`
	ActionPlan string    `json:"actionPlan"`
	Resolution string    `json:"resolution"`
}

type UpdateRequest struct {
	Title      *string    `json:"title,omitempty"`
	FailReason *string    `json:"failReason,omitempty"`
	Owner      *string    `json:"owner,omitempty"`
	DueAt      *time.Time `json:"dueAt,omitempty"`
	RootCause  *string    `json:"rootCause,omitempty"`
	ActionPlan *string    `json:"actionPlan,omitempty"`
	Resolution *string    `json:"resolution,omitempty"`
	Status     *Status    `json:"status,omitempty"`
}

type Event struct {
	ID          int64     `json:"id"`
	CAPAID      int64     `json:"capaId"`
	Action      string    `json:"action"`
	Note        string    `json:"note,omitempty"`
	PerformedBy string    `json:"performedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Summary struct {
	Open                int `json:"open"`
	InProgress          int `json:"inProgress"`
	PendingVerification int `json:"pendingVerification"`
	Closed              int `json:"closed"`
	Overdue             int `json:"overdue"`
}

func IsValidStatus(v Status) bool {
	switch v {
	case StatusOpen, StatusInProgress, StatusPendingVerification, StatusClosed:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusOpen:
		return to == StatusInProgress || to == StatusPendingVerification
	case StatusInProgress:
		return to == StatusOpen || to == StatusPendingVerification
	case StatusPendingVerification:
		return to == StatusInProgress || to == StatusClosed
	default:
		return false
	}
}

func validationError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

func ValidateCreate(req CreateRequest, now time.Time) error {
	if req.OrderID <= 0 {
		return validationError("orderId must be a positive integer")
	}
	if strings.TrimSpace(req.Title) == "" || len(strings.TrimSpace(req.Title)) > 160 {
		return validationError("title is required and must be at most 160 characters")
	}
	if strings.TrimSpace(req.Owner) == "" {
		return validationError("owner is required")
	}
	if req.DueAt.IsZero() || !req.DueAt.After(now) {
		return validationError("dueAt must be in the future")
	}
	if len(req.RootCause) > 4000 || len(req.ActionPlan) > 4000 || len(req.Resolution) > 4000 {
		return validationError("root cause, action plan and resolution must be at most 4000 characters")
	}
	return nil
}

func ValidateReadyForVerification(a CorrectiveAction) error {
	if strings.TrimSpace(a.RootCause) == "" {
		return validationError("rootCause is required before verification")
	}
	if strings.TrimSpace(a.ActionPlan) == "" {
		return validationError("actionPlan is required before verification")
	}
	if strings.TrimSpace(a.Resolution) == "" {
		return validationError("resolution is required before verification")
	}
	return nil
}
