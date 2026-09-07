// Package si implements the System Integration (SI) domain: statuses,
// the SI lifecycle transition graph, environments and the data types used
// by the API. It mirrors the structure of cockpit/pkg/order.

package si

import (
	"errors"
	"fmt"
	"time"
)

// Status represents a lifecycle state of a System Integration.
type Status string

const (
	StatusRequested   Status = "Requested"   // Aangevraagd
	StatusDesign      Status = "Design"      // In ontwerp
	StatusDevelopment Status = "Development" // In ontwikkeling
	StatusTesting     Status = "Testing"     // In test
	StatusLive        Status = "Live"        // Live / actief
	StatusChange      Status = "Change"      // Gewijzigd (wijzigingscyclus)
	StatusRetired     Status = "Retired"     // Uitgefaseerd
	StatusArchived    Status = "Archived"    // Gearchiveerd
	StatusCancelled   Status = "Cancelled"   // Geannuleerd
)

// AllStatuses lists every valid lifecycle status.

var AllStatuses = []Status{
	StatusRequested,
	StatusDesign,
	StatusDevelopment,
	StatusTesting,
	StatusLive,
	StatusChange,
	StatusRetired,
	StatusArchived,
	StatusCancelled,
}

// IsValidStatus reports whether s is a known lifecycle status.

func IsValidStatus(s Status) bool {
	for _, v := range AllStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// StatusLabel returns the Dutch display label for a status.


func StatusLabel(s Status) string {
	labels := map[Status]string{
		StatusRequested:   "Aangevraagd",
		StatusDesign:      "In ontwerp",
		StatusDevelopment: "In ontwikkeling",
		StatusTesting:     "In test",
		StatusLive:        "Live / actief",
		StatusChange:      "In wijziging",
		StatusRetired:     "Uitgefaseerd",
		StatusArchived:    "Gearchiveerd",
		StatusCancelled:  "Geannuleerd",
	}
	if l, ok := labels[s]; ok {
		return l
	}
	return string(s)
}

// Environment is a property of an SI (not a lifecycle state). TEST‖Acceptance‖Production.


type Environment string

const (
	EnvironmentTest      Environment = "TEST"
	EnvironmentAcceptatie Environment = "ACCEPTATIE"
	EnvironmentProductie Environment = "PRODUCTIE"
)
var AllEnvironments = []Environment{
	EnvironmentTest,
	EnvironmentAcceptatie,
	EnvironmentProductie,
}

// IsValidEnvironment reports whether e is a known environment.



func IsValidEnvironment(e Environment) bool {
	for _, v := range AllEnvironments {
		if e == v {
			return true
		}
	}
	return false
}
var (
	ErrInvalidStatus     = errors.New("invalid SI status")
	ErrInvalidTransition = errors.New("invalid SI status transition")
	ErrValidation        = errors.New("validation error")
)

// transitionTable defines the allowed lifecycle transitions per status.

var transitionTable = map[Status]map[Status]bool{
	StatusRequested: {
		StatusDesign:  true,
		StatusCancelled: true,
	},
	StatusDesign: {
		StatusDevelopment: true,
		StatusCancelled:     true,
	},
	StatusDevelopment: {
		StatusTesting: true,
		StatusDesign:   true,
	},
	StatusTesting: {
		StatusLive:       true,
		StatusDevelopment: true,
	},
	StatusLive: {
		StatusChange: true,
		StatusRetired: true,
	},
	StatusChange: {
		StatusDevelopment: true,
		StatusRetired:     true,
	},
	StatusRetired: {
		StatusArchived: true,
	},
	StatusArchived: {},
	StatusCancelled: {},
}

// CanTransition reports whether moving from src to dst is allowed.



func CanTransition(src, dst Status) bool {
	if !IsValidStatus(src) || !IsValidStatus(dst) {
		return false
	}
	return transitionTable[src][dst]
}

// NextStatuses returns the list of allowed targets for a given status (used by the UI)).



func NextStatuses(src Status) []Status {
	if !IsValidStatus(src) {
		return nil
	}
	var out []Status
	for s := range transitionTable[src] {
		if transitionTable[src][s] {
			out = append(out, s)
		}
	}
	return out
}

// DetermineTransition validates and returns the target status for a transition.

func DetermineTransition(src, dst Status) (Status, error) {
	if !IsValidStatus(src) {
		return "", fmt.Errorf("%w: %v", ErrInvalidStatus, src)
	}
	if !IsValidStatus(dst) {
		return "", fmt.Errorf("%w: %v", ErrInvalidStatus, dst)
	}
	if !CanTransition(src, dst) {
		return "", fmt.Errorf("%w: %v -> %v", ErrInvalidTransition, src, dst)
	}
	return dst, nil
}
// Customer represents and organisation (by number, e.g. 94828; that owns projects and SIs.

type Customer struct {
	ID        int64     `json:"id"`
	Number    string    `json:"number"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// Project represents a project underneath a customer.

type Project struct {
	ID             int64     `json:"id"`
	CustomerID     int64     `json:"customerId"`
	CustomerNumber string    `json:"customerNumber"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// SI represents a System Integration; the primary_project_id marks the
// owning project; ProjectIDs lists every linked project (primary included).

type SI struct {
	ID               int64       `json:"id"`
	Code             string      `json:"code"`
	Name             string      `json:"name"`
	Description      string      `json:"description,omitempty"`
	Status           Status      `json:"status"`
	StatusLabel     string      `json:"statusLabel"`
	Version          int         `json:"version"`
	Environment      Environment  `json:"environment"`
	PrimaryProjectID int64       `json:"primaryProjectId"`
	ProjectIDs       []int64     `json:"projectIds"`
	CreatedBy        string      `json:"createdBy,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

// SIEvent is an immutable audit record for an SI action.

type SIEvent struct {
	ID          int64     `json:"id"`
	SIID        int64     `json:"siId"`
	Action      string    `json:"action"`
	FromStatus  string    `json:"fromStatus,omitempty"`
	ToStatus    string    `json:"toStatus,omitempty"`
	Version     int       `json:"version"`
	Note        string    `json:"note,omitempty"`
	PerformedBy string    `json:"performedBy"`
	Timestamp   time.Time `json:"timestamp"`
}

// CreateSIRequest is the payload for creating a new SI.

type CreateSIRequest struct {
	Code              string      `json:"code"`
	Name              string      `json:"name"`
	Description       string      `json:"description"`
	PrimaryProjectID  int64      `json:"primaryProjectId"`
	Environment       Environment `json:"environment"`
}

// UpdateSIRequest is the payload for editing SI metadata (only non-nil fields change).

type UpdateSIRequest struct {
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty"`
	Environment *Environment `json:"environment,omitempty"`
}

// TransitionRequest is the payload for a lifecycle transition.

type TransitionRequest struct {
	Status Status `json:"status"`
	Note   string `json:"note"`
}

// SetProjectsRequest is the payload for linking/unlinking projects (all from the same customer).

type SetProjectsRequest struct {
	ProjectIDs []int64 `json:"projectIds"`
}

// ProjectChecklistItem represents a single checklist item for a project.
// Scanning its QR code opens a check-in page where it can be marked done.

type ProjectChecklistItem struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"projectId"`
	Seq         int        `json:"seq"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	CheckedBy   string     `json:"checkedBy,omitempty"`
	CheckedAt   *time.Time `json:"checkedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// AddChecklistItemRequest is the payload for adding a checklist item.

type AddChecklistItemRequest struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}