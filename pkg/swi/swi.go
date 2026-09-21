package swi

import (
	"errors"
	"fmt"

	"cockpit/pkg/order"
)

type Stage string

const (
	StageOrderImport Stage = "OrderImport"

	StageWorkPreparation Stage = "WorkPreparation"

	StagePointingInWork Stage = "PointingInWork"

	StageInControlWork Stage = "InControlWork"

	StageEscalation Stage = "Escalation"

	StageProcessManagement Stage = "ProcessManagement"

	StageCompleted Stage = "Completed"
)

var AllStages = []Stage{
	StageOrderImport,
	StageWorkPreparation,
	StagePointingInWork,
	StageInControlWork,
	StageEscalation,
	StageProcessManagement,
	StageCompleted,
}

var OperationalStages = []Stage{
	StageOrderImport,
	StageWorkPreparation,
	StagePointingInWork,
	StageInControlWork,
	StageProcessManagement,
}

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

func IsTerminalStage(s Stage) bool { return s == StageCompleted }

func IsOperationalStage(s Stage) bool {
	for _, v := range OperationalStages {
		if s == v {
			return true
		}
	}
	return false
}

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

func AllStageInfo() []StageInfo {
	out := make([]StageInfo, 0, len(AllStages))
	for _, s := range AllStages {
		out = append(out, StageInfo{Stage: s, Label: StageLabel(s), Description: StageDescription(s)})
	}
	return out
}

func StageIndex(s Stage) int {
	for i, v := range AllStages {
		if v == s {
			return i
		}
	}
	return -1
}

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
	ErrInvalidStage = errors.New("invalid SWI stage")

	ErrInvalidTransition = errors.New("invalid SWI stage transition")

	ErrEscalationActive = errors.New("order is escalated and cannot advance")

	ErrNoEscalation = errors.New("order has no open escalation")

	ErrTasksOpen = errors.New("required process tasks are still open")

	ErrValidation = errors.New("validation error")
)

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

func CanAdvance(src, dst Stage) bool {
	if !IsValidStage(src) || !IsValidStage(dst) || src == dst {
		return false
	}
	if IsTerminalStage(src) {
		return false
	}

	if src == StageEscalation {
		return IsOperationalStage(dst) || dst == StageProcessManagement
	}
	if advanceTable[src][dst] {
		return true
	}

	return dst == StageEscalation && IsOperationalStage(src)
}

func NextStages(src Stage) []Stage {
	var out []Stage
	for _, s := range AllStages {
		if CanAdvance(src, s) {
			out = append(out, s)
		}
	}
	return out
}

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
