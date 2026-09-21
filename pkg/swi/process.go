package swi

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cockpit/pkg/order"
)

// Process is the SWI process state of one order.
type Process struct {
	OrderID           int64     `json:"orderId"`
	OrderNumber       string    `json:"orderNumber,omitempty"`
	OrderStatus       string    `json:"orderStatus,omitempty"`
	Stage             Stage     `json:"stage"`
	StageLabel        string    `json:"stageLabel,omitempty"`
	StageSince        time.Time `json:"stageSince"`
	NextStages        []Stage   `json:"nextStages"`
	Escalated         bool      `json:"escalated"`
	EscalationLevel   Level     `json:"escalationLevel,omitempty"`
	OpenEscalations   int       `json:"openEscalations"`
	TaskTotal         int       `json:"taskTotal"`
	TaskDone          int       `json:"taskDone"`
	TasksRequiredOpen int       `json:"tasksRequiredOpen"`
	SyncPending       int       `json:"syncPending"`
	SyncFailed        int       `json:"syncFailed"`
	SuggestedStatus   string    `json:"suggestedOrderStatus,omitempty"`
	Assignee          string    `json:"assignee,omitempty"`
	Version           int       `json:"version"`
	UpdatedBy         string    `json:"updatedBy,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
	Tasks             []Task    `json:"tasks,omitempty"`
}

// Event is an append-only process event (audit trail of the SWI process).
type Event struct {
	ID          int64     `json:"id"`
	OrderID     int64     `json:"orderId"`
	Action      string    `json:"action"`
	FromStage   Stage     `json:"fromStage,omitempty"`
	ToStage     Stage     `json:"toStage,omitempty"`
	Level       Level     `json:"level,omitempty"`
	Note        string    `json:"note,omitempty"`
	PerformedBy string    `json:"performedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Metrics summarises the process for the process-management view.
type Metrics struct {
	Total                  int            `json:"total"`
	ByStage                map[Stage]int  `json:"byStage"`
	Escalated              int            `json:"escalated"`
	TasksOpen              int            `json:"tasksOpen"`
	SyncPending            int            `json:"syncPending"`
	SyncFailed             int            `json:"syncFailed"`
	SyncFailedBySystem     map[System]int `json:"syncFailedBySystem,omitempty"`
	AvgActiveAgeSeconds    int64          `json:"avgActiveAgeSeconds"`
	OldestActiveAgeSeconds int64          `json:"oldestActiveAgeSeconds"`
	CompletedLast24h       int            `json:"completedLast24h"`
	GeneratedAt            time.Time      `json:"generatedAt"`
}

// Lane is one column of the process board.
type Lane struct {
	Stage       Stage     `json:"stage"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	Count       int       `json:"count"`
	Processes   []Process `json:"processes"`
}

// Board is the process-management overview: every order in its stage lane plus
// the aggregate metrics.
type Board struct {
	Lanes       []Lane    `json:"lanes"`
	Metrics     Metrics   `json:"metrics"`
	GeneratedAt time.Time `json:"generatedAt"`
}

// BuildBoard groups processes into stage lanes in flow order. Escalated
// processes are shown in the escalation lane, so that lane contains exactly
// the work that needs attention.
func BuildBoard(now time.Time, procs []Process, m Metrics) Board {
	board := Board{Lanes: make([]Lane, 0, len(AllStages)), Metrics: m, GeneratedAt: now}
	byStage := make(map[Stage][]Process, len(AllStages))
	for _, p := range procs {
		if p.Escalated && p.Stage != StageEscalation {
			byStage[StageEscalation] = append(byStage[StageEscalation], p)
			continue
		}
		byStage[p.Stage] = append(byStage[p.Stage], p)
	}
	for _, s := range AllStages {
		items := byStage[s]
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Escalated != items[j].Escalated {
				return items[i].Escalated
			}
			return items[i].StageSince.Before(items[j].StageSince)
		})
		board.Lanes = append(board.Lanes, Lane{
			Stage:       s,
			Label:       StageLabel(s),
			Description: StageDescription(s),
			Count:       len(items),
			Processes:   items,
		})
	}
	return board
}
// AdvanceRequest moves a process to another stage. Force bypasses the required
// task gate and is only allowed for users with the process:manage permission.
type AdvanceRequest struct {
	Stage Stage  `json:"stage"`
	Note  string `json:"note,omitempty"`
	Force bool   `json:"force,omitempty"`
}

// EscalateRequest raises an escalation for an order.
type EscalateRequest struct {
	Level       Level  `json:"level"`
	Reason      Reason `json:"reason"`
	Note        string `json:"note,omitempty"`
	EscalatedTo string `json:"escalatedTo,omitempty"`
}

// ResolveEscalationRequest closes an escalation.
type ResolveEscalationRequest struct {
	Resolution string `json:"resolution"`
	Stage      Stage  `json:"stage,omitempty"`
}

// TaskRequest ticks or unticks a process task.
type TaskRequest struct {
	Note string `json:"note,omitempty"`
}

// ImportRequest is the AFAS order import payload: the first step of the SWI
// process. It creates the order and starts the process in OrderImport.
type ImportRequest struct {
	OrderNumber       string     `json:"orderNumber"`
	DebitNumber       string     `json:"debitNumber,omitempty"`
	CustomerName      string     `json:"customerName,omitempty"`
	Device            string     `json:"device,omitempty"`
	AssetNumber       string     `json:"assetNumber,omitempty"`
	Configuration     string     `json:"configuration,omitempty"`
	OmnitrackerTicket string     `json:"omnitrackerTicket,omitempty"`
	SIID              *int64     `json:"siId,omitempty"`
	Source            string     `json:"source,omitempty"`
	TargetCompletion  *time.Time `json:"targetCompletionAt,omitempty"`
}

// ImportResult is returned after an order import.
type ImportResult struct {
	Order   order.Order `json:"order"`
	Process Process     `json:"process"`
	Jobs    []SyncJob   `json:"syncJobs"`
}

// CompleteSyncRequest is the callback of the integration worker.
type CompleteSyncRequest struct {
	Status   SyncStatus `json:"status"`
	Response string     `json:"response,omitempty"`
	Error    string     `json:"error,omitempty"`
}

// ErrSyncJobDecided is returned when a sync job that already reached a final
// state is completed again.
var ErrSyncJobDecided = errors.New("sync job already has a final status")

func validationErr(msg string) error { return fmt.Errorf("%w: %s", ErrValidation, msg) }

// ValidateAdvance checks an advance request.
func ValidateAdvance(req AdvanceRequest) error {
	if !IsValidStage(req.Stage) {
		return validationErr("stage must be one of " + strings.Join(stageNames(), ", "))
	}
	if IsTerminalStage(req.Stage) {
		return validationErr("Completed is only reachable from ProcessManagement")
	}
	return nil
}

// ValidateImport checks an AFAS import payload.
func ValidateImport(req ImportRequest) error {
	if strings.TrimSpace(req.OrderNumber) == "" {
		return validationErr("orderNumber is required")
	}
	if strings.TrimSpace(req.DebitNumber) == "" && strings.TrimSpace(req.CustomerName) == "" {
		return validationErr("either debitNumber or customerName is required")
	}
	return nil
}

// ValidateComplete checks an integration worker callback.
func ValidateComplete(req CompleteSyncRequest) error {
	switch req.Status {
	case SyncDone, SyncFailed, SyncSkipped:
		return nil
	}
	return validationErr("status must be one of DONE, FAILED, SKIPPED")
}

func stageNames() []string {
	out := make([]string, 0, len(AllStages))
	for _, s := range AllStages {
		out = append(out, string(s))
	}
	return out
}