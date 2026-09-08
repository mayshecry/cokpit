
package si

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusConcept      Status = "Concept"
	StatusInValidation Status = "In Validation"
	StatusAccepted     Status = "Accepted"
	StatusOutphased    Status = "Outphased"
	StatusBlocked      Status = "Blocked"
)

var AllStatuses = []Status{
	StatusConcept,
	StatusInValidation,
	StatusAccepted,
	StatusOutphased,
	StatusBlocked,
}


func IsValidStatus(s Status) bool {
	for _, v := range AllStatuses {
		if s == v {
			return true
		}
	}
	return false
}



func StatusLabel(s Status) string {
	labels := map[Status]string{
		StatusConcept:      "Concept",
		StatusInValidation: "In Validatie",
		StatusAccepted:     "Geaccepteerd",
		StatusOutphased:    "Uitgefaseerd",
		StatusBlocked:      "Geblokkeerd",
	}
	if l, ok := labels[s]; ok {
		return l
	}
	return string(s)
}



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


var transitionTable = map[Status]map[Status]bool{
	StatusConcept: {
		StatusInValidation: true,
		StatusBlocked:      true,
	},
	StatusInValidation: {
		StatusAccepted: true,
		StatusConcept:  true,
		StatusBlocked:  true,
	},
	StatusAccepted: {
		StatusOutphased: true,
		StatusBlocked:   true,
	},
	StatusBlocked: {
		StatusConcept:      true,
		StatusInValidation: true,
		StatusAccepted:     true,
	},
	StatusOutphased: {},
}




func CanTransition(src, dst Status) bool {
	if !IsValidStatus(src) || !IsValidStatus(dst) {
		return false
	}
	return transitionTable[src][dst]
}




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

type Customer struct {
	ID        int64     `json:"id"`
	Number    string    `json:"number"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type SIConfig struct {
	DebitNumber     string   `json:"debitNumber,omitempty"`
	DeviceType      string   `json:"deviceType,omitempty"`
	ConfigID        string   `json:"configId,omitempty"`
	WindowsProfile  string   `json:"windowsProfile,omitempty"`
	Software        string   `json:"software,omitempty"`
	AssetSticker    bool     `json:"assetSticker"`
	Sleeve          bool     `json:"sleeve"`
	ScreenProtector bool     `json:"screenProtector"`
	OtherDemands    string   `json:"otherDemands,omitempty"`
	// NPI-determined automatic fields
	WorkInstructions string   `json:"workInstructions,omitempty"`
	SWI              string   `json:"swi,omitempty"`
	Workflow         string   `json:"workflow,omitempty"`
	QCProfile        string   `json:"qcProfile,omitempty"`
	Automations      string   `json:"automations,omitempty"`
	EscalationFlow   []string `json:"escalationFlow,omitempty"`
}

func ParseSICode(code string) (string, string, string, error) {
	parts := strings.Split(code, "-")
	if len(parts) != 4 || parts[0] != "SI" {
		return "", "", "", fmt.Errorf("invalid SI code format: %s, expected SI-{debit}-{device}-{config}", code)
	}
	return parts[1], parts[2], parts[3], nil
}


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


type SI struct {
	ID               int64       `json:"id"`
	Code             string      `json:"code"`
	Name             string      `json:"name"`
	Description      string      `json:"description,omitempty"`
	Config           *SIConfig   `json:"config,omitempty"`
	Status           Status      `json:"status"`
	StatusLabel      string      `json:"statusLabel"`
	Version          int         `json:"version"`
	VersionLabel     string      `json:"versionLabel"`
	Environment      Environment `json:"environment"`
	PrimaryProjectID int64       `json:"primaryProjectId"`
	ProjectIDs       []int64     `json:"projectIds"`
	CreatedBy        string      `json:"createdBy,omitempty"`
	CreatedAt        time.Time   `json:"createdAt"`
	UpdatedAt        time.Time   `json:"updatedAt"`
}

type Department struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}


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


type CreateSIRequest struct {
	Code              string      `json:"code"`
	Name              string      `json:"name"`
	Description       string      `json:"description"`
	Config            *SIConfig   `json:"config,omitempty"`
	PrimaryProjectID  int64       `json:"primaryProjectId"`
	Environment       Environment `json:"environment"`
}


type UpdateSIRequest struct {
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty"`
	Config      *SIConfig    `json:"config,omitempty"`
	Environment *Environment `json:"environment,omitempty"`
}


type TransitionRequest struct {
	Status Status `json:"status"`
	Note   string `json:"note"`
}


type SetProjectsRequest struct {
	ProjectIDs []int64 `json:"projectIds"`
}


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


type AddChecklistItemRequest struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}