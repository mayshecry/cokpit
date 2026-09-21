package swi

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// System is an external system the SWI process keeps in sync. These are the
// systems listed in the process scope: AFAS (ERP / order import), Omnitracker
// (ticketing), Intune (MDM), Knox (Samsung MDM) and Apple Business Manager.
type System string

const (
	SystemAFAS                 System = "AFAS"
	SystemOmnitracker          System = "Omnitracker"
	SystemIntune               System = "Intune"
	SystemKnox                 System = "Knox"
	SystemAppleBusinessManager System = "AppleBusinessManager"
)

// AllSystems lists every integrated system.
var AllSystems = []System{
	SystemAFAS,
	SystemOmnitracker,
	SystemIntune,
	SystemKnox,
	SystemAppleBusinessManager,
}

// SystemLabel returns the display label of a system.
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

// Operation is the kind of change pushed to (or pulled from) a system.
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

// Direction distinguishes inbound (import) from outbound (update) traffic.
type Direction string

const (
	DirectionInbound  Direction = "INBOUND"
	DirectionOutbound Direction = "OUTBOUND"
)

// SyncStatus is the lifecycle of a queued external update.
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

// SyncStatusLabel returns the display label of a sync status.
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

// PlannedSync is a single external update the process requires.
type PlannedSync struct {
	System      System    `json:"system"`
	SystemLabel string    `json:"systemLabel"`
	Operation   Operation `json:"operation"`
	Direction   Direction `json:"direction"`
	Entity      string    `json:"entity"`
	Reason      string    `json:"reason"`
}

// SyncJob is a persisted, queueable external update (outbox item). The
// integration worker claims pending jobs, performs the call against the
// external system and reports the result back.
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

// SyncJobFilter selects sync jobs for the integration dashboard.
type SyncJobFilter struct {
	System      System
	Status      SyncStatus
	OrderID     int64
	Limit       int
	PendingOnly bool
}

// JobKey builds the idempotency key of a sync job.
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

// SortJobs orders jobs by system so the worker processes them predictably.
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

// syncPlan maps a stage transition onto the external updates that must be
// queued. Straight-forward transitions mirror the process steps; rework and
// escalation movements only need Omnitracker to be updated.
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
	// escalationEnterSync is queued when a process enters the escalation lane.
	escalationEnterSync = PlannedSync{
		System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound,
		Entity: "ticket", Reason: "Escalatie vastleggen op het Omnitracker-ticket",
	}
	// escalationLeaveSync is queued when an escalation is released.
	escalationLeaveSync = PlannedSync{
		System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound,
		Entity: "ticket", Reason: "Escalatie afsluiten op het Omnitracker-ticket",
	}
	// reworkSync is queued when work moves a step back.
	reworkSync = PlannedSync{
		System: SystemOmnitracker, Operation: OpUpdate, Direction: DirectionOutbound,
		Entity: "ticket", Reason: "Herwerk/terugzetten van de processtap vastleggen",
	}
)

// PlanSync returns the external updates required for a stage transition. The
// result is deterministic (stable order) so re-queueing the same transition is
// idempotent when combined with the job idempotency key.
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
	// Rework transitions (a step back) only keep Omnitracker in step.
	return []PlannedSync{reworkSync}
}

