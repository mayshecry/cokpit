package swi

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type System string

const (
	SystemAFAS                 System = "AFAS"
	SystemOmnitracker          System = "Omnitracker"
	SystemIntune               System = "Intune"
	SystemKnox                 System = "Knox"
	SystemAppleBusinessManager System = "AppleBusinessManager"
)

var AllSystems = []System{
	SystemAFAS,
	SystemOmnitracker,
	SystemIntune,
	SystemKnox,
	SystemAppleBusinessManager,
}

func SystemLabel(s System) string {
	if s == SystemAppleBusinessManager {
		return "Apple Business Manager"
	}
	return string(s)
}

func IsValidSystem(s System) bool {
	for _, v := range AllSystems {
		if s == v {
			return true
		}
	}
	return false
}

type Operation string

const (
	OpRead   Operation = "READ"
	OpCreate Operation = "CREATE"
	OpUpdate Operation = "UPDATE"
	OpDelete Operation = "DELETE"
)

func IsValidOperation(o Operation) bool {
	switch o {
	case OpRead, OpCreate, OpUpdate, OpDelete:
		return true
	}
	return false
}

type Direction string

const (
	DirectionInbound  Direction = "INBOUND"
	DirectionOutbound Direction = "OUTBOUND"
)

type SyncStatus string

const (
	SyncPending    SyncStatus = "PENDING"
	SyncInProgress SyncStatus = "IN_PROGRESS"
	SyncDone       SyncStatus = "DONE"
	SyncFailed     SyncStatus = "FAILED"
	SyncSkipped    SyncStatus = "SKIPPED"
)

func IsValidSyncStatus(s SyncStatus) bool {
	switch s {
	case SyncPending, SyncInProgress, SyncDone, SyncFailed, SyncSkipped:
		return true
	}
	return false
}

func SyncStatusLabel(s SyncStatus) string {
	switch s {
	case SyncPending:
		return "Wacht op verzending"
	case SyncInProgress:
		return "In behandeling"
	case SyncDone:
		return "Bijgewerkt"
	case SyncFailed:
		return "Mislukt"
	case SyncSkipped:
		return "Overgeslagen"
	}
	return string(s)
}

type PlannedSync struct {
	System      System    `json:"system"`
	SystemLabel string    `json:"systemLabel"`
	Operation   Operation `json:"operation"`
	Direction   Direction `json:"direction"`
	Entity      string    `json:"entity"`
	Reason      string    `json:"reason"`
}

type SyncJob struct {
	ID             int64           `json:"id"`
	OrderID        int64           `json:"orderId"`
	OrderNumber    string          `json:"orderNumber,omitempty"`
	System         System          `json:"system"`
	SystemLabel    string          `json:"systemLabel,omitempty"`
	Operation      Operation       `json:"operation"`
	Direction      Direction       `json:"direction"`
	Entity         string          `json:"entity"`
	Reason         string          `json:"reason,omitempty"`
	Status         SyncStatus      `json:"status"`
	StatusLabel    string          `json:"statusLabel,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Response       string          `json:"response,omitempty"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"lastError,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
}

type SyncJobFilter struct {
	System      System
	Status      SyncStatus
	OrderID     int64
	Limit       int
	PendingOnly bool
}

func JobKey(orderID int64, from, to Stage, index int, p PlannedSync) string {
	parts := []string{
		"ord", itoa64(orderID),
		"from", string(from),
		"to", string(to),
		"idx", itoa64(int64(index)),
		"sys", string(p.System),
		"op", string(p.Operation),
	}
	return strings.ToLower(strings.Join(parts, ":"))
}

func SortJobs(jobs []SyncJob) {
	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].System != jobs[j].System {
			return jobs[i].System < jobs[j].System
		}
		return jobs[i].ID < jobs[j].ID
	})
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var syncPlan = map[Stage]map[Stage][]PlannedSync{
	StageOrderImport: {
		StageWorkPreparation: {
			{System: SystemAFAS, Operation: OpRead, Direction: DirectionInbound, Entity: "order-lines", Reason: "Orderregels en debiteurnummer uit AFAS ophalen"},
			{System: SystemOmnitracker, Operation: OpCreate, Direction: DirectionOutbound, Entity: "ticket", Reason: "Omnitracker-ticket aanmaken voor de order"},
		},
	},
	StageWorkPreparation: {
		StagePointingInWork: {
			{System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "ticket", Reason: "Werkvoorbereiding geboekt op het Omnitracker-ticket"},
			{System: SystemAFAS, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "workorder", Reason: "Werkorder bijwerken in AFAS"},
		},
	},
	StagePointingInWork: {
		StageInControlWork: {
			{System: SystemIntune, Operation: OpCreate, Direction: DirectionOutbound, Entity: "device-enrollment", Reason: "Device aanmelden in Intune"},
			{System: SystemKnox, Operation: OpCreate, Direction: DirectionOutbound, Entity: "device-enrollment", Reason: "Device aanmelden in Knox"},
			{System: SystemAppleBusinessManager, Operation: OpCreate, Direction: DirectionOutbound, Entity: "device-enrollment", Reason: "Device aanmelden in Apple Business Manager"},
			{System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "ticket", Reason: "Werk toegewezen aan engineer"},
		},
	},
	StageInControlWork: {
		StageProcessManagement: {
			{System: SystemIntune, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "device", Reason: "Deviceconfiguratie en compliance bijwerken in Intune"},
			{System: SystemKnox, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "device", Reason: "Knox-profiel bijwerken"},
			{System: SystemAppleBusinessManager, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "device", Reason: "ABM-toewijzing bijwerken"},
			{System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "ticket", Reason: "Uitvoering en inwerkcontrole vastleggen"},
			{System: SystemAFAS, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "workorder", Reason: "Uitgevoerd werk doorgeven aan AFAS"},
		},
	},
	StageProcessManagement: {
		StageCompleted: {
			{System: SystemAFAS, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "workorder", Reason: "Order financieel afronden in AFAS"},
			{System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "ticket", Reason: "Omnitracker-ticket sluiten"},
			{System: SystemIntune, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "device", Reason: "Device afmelden/uitfaseren in Intune"},
			{System: SystemKnox, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "device", Reason: "Knox-inschrijving afronden"},
			{System: SystemAppleBusinessManager, Operation: OpUpdate, Direction: DirectionOutbound, Entity: "device", Reason: "ABM-inschrijving afronden"},
		},
	},
}

var (
	escalationEnterSync = PlannedSync{
		System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound,
		Entity: "ticket", Reason: "Escalatie vastleggen op het Omnitracker-ticket",
	}

	escalationLeaveSync = PlannedSync{
		System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound,
		Entity: "ticket", Reason: "Escalatie afsluiten op het Omnitracker-ticket",
	}

	reworkSync = PlannedSync{
		System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound,
		Entity: "ticket", Reason: "Herwerk/terugzetten van de processtap vastleggen",
	}
)

func PlanSync(from, to Stage) []PlannedSync {
	if !CanAdvance(from, to) {
		return nil
	}
	if to == StageEscalation {
		return []PlannedSync{escalationEnterSync}
	}
	if from == StageEscalation {
		return []PlannedSync{escalationLeaveSync}
	}
	if plans, ok := syncPlan[from][to]; ok {
		out := make([]PlannedSync, len(plans))
		copy(out, plans)
		return out
	}

	return []PlannedSync{reworkSync}
}
