// Package swi implements the SWI (Service Work Instruction) tool process: the
// single end-to-end operating process for service work orders, from the AFAS
// order import up to process management, including escalations and the
// automatic updates to the external systems (AFAS, Omnitracker, Intune, Knox
// and Apple Business Manager).
//
// The package is pure domain logic — no storage, no HTTP — and mirrors the
// pattern of pkg/order and pkg/si (status/transition tables, typed errors,
// labels, request types).
//
// Process flow:
//
//	OrderImport ──► WorkPreparation ──► PointingInWork ──► InControlWork
//	                     │                     │                 │
//	                     └─────────► Escalation ◄────────────────┘
//	                                     │
//	                                     ▼
//	                            ProcessManagement ──► Completed
//
// Every stage transition plans the external system updates that must be pushed
// to AFAS, Omnitracker, Intune, Knox and Apple Business Manager; those plans
// are persisted as sync jobs (outbox) and executed by an integration worker.
package swi

import (
	"errors"
	"fmt"

	"cockpit/pkg/order"
)

// Stage is one step of the SWI tool process.
type Stage string

const (
	// StageOrderImport is the intake of the order from AFAS (debit number,
	// customer, device, asset) and the registration of the Omnitracker ticket.
	StageOrderImport Stage = "OrderImport"

	// StageWorkPreparation is the preparation of the work: pick list, product
	// manual, required parts and planning.
	StageWorkPreparation Stage = "WorkPreparation"

	// StagePointingInWork is where the work is pointed at (assigned to) an
	// engineer/team and the device is registered in Intune, Knox and ABM.
	StagePointingInWork Stage = "PointingInWork"

	// StageInControlWork is the execution plus the in-control check (4-eyes /
	// QC) before the order leaves the floor.
	StageInControlWork Stage = "InControlWork"

	// StageEscalation is the escalation lane. It can be entered from any
	// operational stage and returns to the stage it came from once resolved.
	StageEscalation Stage = "Escalation"

	// StageProcessManagement is the process owner's closure and control step:
	// throughput/TAT review, root cause and updating the source systems.
	StageProcessManagement Stage = "ProcessManagement"

	// StageCompleted is the terminal stage.
	StageCompleted Stage = "Completed"
)

// AllStages lists every stage in board order.
var AllStages = []Stage{
	StageOrderImport,
	StageWorkPreparation,
	StagePointingInWork,
	StageInControlWork,
	StageEscalation,
	StageProcessManagement,
	StageCompleted,
}

// OperationalStages are the stages work flows through. StageEscalation is a
// side lane that can be entered from (and released back to) any of these.
var OperationalStages = []Stage{
	StageOrderImport,
	StageWorkPreparation,
	StagePointingInWork,
	StageInControlWork,
	StageProcessManagement,
}

// StageInfo is the display metadata of a stage.
type StageInfo struct {
	Stage       Stage  `json:"stage"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func IsValidStage(s Stage) bool {
	for _, v := range AllStages {
		if s == v {
			return true
		}
	}
	return false
}

// IsTerminalStage reports whether no further stage transition is possible.
func IsTerminalStage(s Stage) bool { return s == StageCompleted }

// IsOperationalStage reports whether s is part of the operational flow (i.e.
// not the escalation lane and not the terminal stage).
func IsOperationalStage(s Stage) bool {
	for _, v := range OperationalStages {
		if s == v {
			return true
		}
	}
	return false
}

// StageLabel returns the Dutch label shown in the dashboard.
func StageLabel(s Stage) string {
	switch s {
	case StageOrderImport:
		return "Orderinvoer (AFAS)"
	case StageWorkPreparation:
		return "Werkvoorbereiding"
	case StagePointingInWork:
		return "Werk aanwijzen"
	case StageInControlWork:
		return "Werk in control"
	case StageEscalation:
		return "Escalaties"
	case StageProcessManagement:
		return "Procesmanagement"
	case StageCompleted:
		return "Afgerond"
	}
	return string(s)
}

// StageDescription returns the one-line explanation of a stage.
func StageDescription(s Stage) string {
	switch s {
	case StageOrderImport:
		return "Order uit AFAS inlezen: debiteur, klant, device, asset en Omnitracker-ticket."
	case StageWorkPreparation:
		return "Picklijst, productmanual, onderdelen en planning gereedmaken."
	case StagePointingInWork:
		return "Werk en apparaat toewijzen aan engineer; device aanmelden in Intune, Knox en ABM."
	case StageInControlWork:
		return "Uitvoering plus inwerkcontrole (4-ogen) en QC."
	case StageEscalation:
		return "Afwijking of overschrijding: eigenaar bepalen, besluit nemen en terugkoppelen."
	case StageProcessManagement:
		return "Proceseigenaar controleert doorlooptijd, oorzaak en werkt bronsystemen bij."
	case StageCompleted:
		return "Order is afgerond en alle koppelingen zijn bijgewerkt."
	}
	return ""
}

// AllStageInfo lists the stage metadata in board order.
func AllStageInfo() []StageInfo {
	out := make([]StageInfo, 0, len(AllStages))
	for _, s := range AllStages {
		out = append(out, StageInfo{Stage: s, Label: StageLabel(s), Description: StageDescription(s)})
	}
	return out
}

// StageIndex returns the position of a stage in the flow (-1 when unknown).
func StageIndex(s Stage) int {
	for i, v := range AllStages {
		if v == s {
			return i
		}
	}
	return -1
}

// SuggestOrderStatus maps a SWI stage onto the order lifecycle status so the
// dashboard can show how the two layers line up. The order state machine
// (pkg/order) stays authoritative; the SWI stage never changes it silently.
func SuggestOrderStatus(s Stage) order.Status {
	switch s {
	case StageOrderImport:
		return order.StatusReceived
	case StageWorkPreparation, StagePointingInWork:
		return order.StatusProcessing
	case StageInControlWork, StageEscalation, StageProcessManagement:
		return order.StatusQCReview
	case StageCompleted:
		return order.StatusCompleted
	}
	return order.StatusReceived
}

var (
	// ErrInvalidStage marks an unknown stage value.
	ErrInvalidStage = errors.New("invalid SWI stage")

	// ErrInvalidTransition marks a stage transition the process does not allow.
	ErrInvalidTransition = errors.New("invalid SWI stage transition")

	// ErrEscalationActive marks an action blocked because the order is escalated.
	ErrEscalationActive = errors.New("order is escalated and cannot advance")

	// ErrNoEscalation marks a resolve call without an open escalation.
	ErrNoEscalation = errors.New("order has no open escalation")

	// ErrTasksOpen marks an advance blocked by unfinished required tasks.
	ErrTasksOpen = errors.New("required process tasks are still open")

	// ErrValidation marks invalid input.
	ErrValidation = errors.New("validation error")
)

// advanceTable holds the forward and rework transitions per stage. The
// escalation lane is additionally reachable from every operational stage and
// releases back to any operational stage (handled in CanAdvance).
var advanceTable = map[Stage]map[Stage]bool{
	StageOrderImport: {
		StageWorkPreparation: true,
		StageEscalation:      true,
	},
	StageWorkPreparation: {
		StagePointingInWork: true,
		StageOrderImport:    true,
		StageEscalation:     true,
	},
	StagePointingInWork: {
		StageInControlWork:   true,
		StageWorkPreparation: true,
		StageEscalation:      true,
	},
	StageInControlWork: {
		StageProcessManagement: true,
		StagePointingInWork:    true,
		StageEscalation:        true,
	},
	StageEscalation: {
		StageProcessManagement: true,
	},
	StageProcessManagement: {
		StageCompleted:     true,
		StageInControlWork: true,
	},
	StageCompleted: {},
}

// CanAdvance reports whether the process may move from src to dst.
func CanAdvance(src, dst Stage) bool {
	if !IsValidStage(src) || !IsValidStage(dst) || src == dst {
		return false
	}
	if IsTerminalStage(src) {
		return false
	}
	// Releasing an escalation: back to any operational stage.
	if src == StageEscalation {
		return IsOperationalStage(dst) || dst == StageProcessManagement
	}
	if advanceTable[src][dst] {
		return true
	}
	// Escalation may be raised from any operational stage.
	return dst == StageEscalation && IsOperationalStage(src)
}

// NextStages lists the stages reachable from src in flow order.
func NextStages(src Stage) []Stage {
	var out []Stage
	for _, s := range AllStages {
		if CanAdvance(src, s) {
			out = append(out, s)
		}
	}
	return out
}

// DetermineAdvance validates a stage transition.
func DetermineAdvance(src, dst Stage) (Stage, error) {
	if !IsValidStage(dst) {
		return "", fmt.Errorf("%w: %v", ErrInvalidStage, dst)
	}
	if !IsValidStage(src) {
		return "", fmt.Errorf("%w: %v", ErrInvalidStage, src)
	}
	if !CanAdvance(src, dst) {
		return "", fmt.Errorf("%w: %v -> %v", ErrInvalidTransition, src, dst)
	}
	return dst, nil
}


