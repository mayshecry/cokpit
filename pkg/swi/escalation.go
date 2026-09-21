package swi

import (
	"time"

	"cockpit/pkg/order"
)

type Level string

const (
	LevelL1 Level = "L1"
	LevelL2 Level = "L2"
	LevelL3 Level = "L3"
)

var AllLevels = []Level{LevelL1, LevelL2, LevelL3}

func IsValidLevel(l Level) bool {
	for _, v := range AllLevels {
		if l == v {
			return true
		}
	}
	return false
}

func LevelLabel(l Level) string {
	switch l {
	case LevelL1:
		return "L1 — teamleider"
	case LevelL2:
		return "L2 — operations"
	case LevelL3:
		return "L3 — proceseigenaar"
	}
	return string(l)
}

type Reason string

const (
	ReasonSLABreach    Reason = "SLA_BREACH"
	ReasonStageStalled Reason = "STAGE_STALLED"
	ReasonBlocked      Reason = "BLOCKED"
	ReasonCustomer     Reason = "CUSTOMER"
	ReasonQuality      Reason = "QUALITY"
	ReasonManual       Reason = "MANUAL"
)

var AllReasons = []Reason{
	ReasonSLABreach,
	ReasonStageStalled,
	ReasonBlocked,
	ReasonCustomer,
	ReasonQuality,
	ReasonManual,
}

func IsValidReason(r Reason) bool {
	for _, v := range AllReasons {
		if r == v {
			return true
		}
	}
	return false
}

func ReasonLabel(r Reason) string {
	switch r {
	case ReasonSLABreach:
		return "SLA overschreden"
	case ReasonStageStalled:
		return "Processtap blijft te lang liggen"
	case ReasonBlocked:
		return "Geblokkeerd (materiaal/toegang/leverancier)"
	case ReasonCustomer:
		return "Klantvraag of klacht"
	case ReasonQuality:
		return "Kwaliteit of herwerk"
	case ReasonManual:
		return "Handmatig gemeld"
	}
	return string(r)
}

type Escalation struct {
	ID             int64      `json:"id"`
	OrderID        int64      `json:"orderId"`
	OrderNumber    string     `json:"orderNumber,omitempty"`
	Stage          Stage      `json:"stage"`
	ReturnStage    Stage      `json:"returnStage,omitempty"`
	Level          Level      `json:"level"`
	LevelLabel     string     `json:"levelLabel,omitempty"`
	Reason         Reason     `json:"reason"`
	ReasonLabel    string     `json:"reasonLabel,omitempty"`
	Note           string     `json:"note,omitempty"`
	RaisedBy       string     `json:"raisedBy"`
	RaisedAt       time.Time  `json:"raisedAt"`
	EscalatedTo    string     `json:"escalatedTo,omitempty"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
	ResolvedBy     string     `json:"resolvedBy,omitempty"`
	ResolutionNote string     `json:"resolutionNote,omitempty"`
	Open           bool       `json:"open"`
}

func StageBudget(s Stage) time.Duration {
	switch s {
	case StageOrderImport:
		return 4 * time.Hour
	case StageWorkPreparation:
		return 8 * time.Hour
	case StagePointingInWork:
		return 4 * time.Hour
	case StageInControlWork:
		return 16 * time.Hour
	case StageEscalation:
		return 4 * time.Hour
	case StageProcessManagement:
		return 8 * time.Hour
	}
	return 0
}

func SuggestedLevel(sla order.SLAStatus, overdue time.Duration) Level {
	if sla != order.SLABreached {
		return LevelL1
	}
	if overdue >= 4*time.Hour {
		return LevelL3
	}
	return LevelL2
}

func NeedsEscalation(o order.Order, p Process, sla order.SLA, now time.Time) (Level, Reason, bool) {
	if p.Escalated || IsTerminalStage(p.Stage) || !IsValidStage(p.Stage) {
		return "", "", false
	}
	if sla.Status == order.SLABreached {
		return SuggestedLevel(sla.Status, -sla.Remaining), ReasonSLABreach, true
	}
	budget := StageBudget(p.Stage)
	if budget > 0 && !p.StageSince.IsZero() && now.Sub(p.StageSince) > budget {
		return LevelL1, ReasonStageStalled, true
	}
	return "", "", false
}

func ValidateEscalation(req EscalateRequest) error {
	if !IsValidLevel(req.Level) {
		return validationErr("level must be one of L1, L2, L3")
	}
	if !IsValidReason(req.Reason) {
		return validationErr("reason must be one of SLA_BREACH, STAGE_STALLED, BLOCKED, CUSTOMER, QUALITY, MANUAL")
	}
	return nil
}
