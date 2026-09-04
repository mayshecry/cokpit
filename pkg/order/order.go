package order

import "time"

type Order struct {
	ID               int64     `json:"id"`
	OrderNumber      string    `json:"orderNumber"`
	Status           Status    `json:"status"`
	TargetCompletion time.Time `json:"targetCompletionAt"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`

	Assignee string `json:"assignee,omitempty"`

	PausedSeconds int64      `json:"pausedSeconds,omitempty"`
	HoldSince     *time.Time `json:"holdSince,omitempty"`

	SLA *SLA `json:"sla,omitempty"`
}

type Hold struct {
	ID         int64      `json:"id"`
	OrderID    int64      `json:"orderId"`
	Reason     string     `json:"reason"`
	CreatedBy  string     `json:"createdBy"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type QCStatus string

const (
	QCPass QCStatus = "PASS"
	QCFail QCStatus = "FAIL"
)

func IsValidQC(s QCStatus) bool {
	return s == QCPass || s == QCFail
}

type QCCheck struct {
	ID          int64     `json:"id"`
	OrderID     int64     `json:"orderId"`
	Status      QCStatus  `json:"status"`
	InspectorID string    `json:"inspectorId"`
	Notes       string    `json:"notes"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AuditLog struct {
	ID          int64     `json:"id"`
	OrderID     int64     `json:"orderId"`
	Action      string    `json:"action"`
	PerformedBy string    `json:"performedBy"`
	Timestamp   time.Time `json:"timestamp"`
}

type Comment struct {
	ID         int64     `json:"id"`
	OrderID    int64     `json:"orderId"`
	AuthorID   string    `json:"authorId"`
	AuthorName string    `json:"authorName"`
	Body       string    `json:"body"`
	Mentions   []string  `json:"mentions"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ScanEvent struct {
	ID        int64     `json:"id"`
	OrderID   int64     `json:"orderId"`
	Code      string    `json:"code"`
	ScannedBy string    `json:"scannedBy"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"createdAt"`
}

type ScanResult struct {
	Order   Order        `json:"order"`
	Detail  *OrderDetail `json:"detail,omitempty"`
	Event   ScanEvent    `json:"event"`
	Action  string       `json:"action"`
	Matched bool         `json:"matched"`
}

type Notification struct {
	ID          int64      `json:"id"`
	UserName    string     `json:"userName"`
	Kind        string     `json:"kind"`
	OrderID     int64      `json:"orderId"`
	OrderNumber string     `json:"orderNumber"`
	RefID       int64      `json:"refId"`
	Body        string     `json:"body"`
	ReadAt      *time.Time `json:"readAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type CommentRequest struct {
	Body string `json:"body"`
}

type ScanRequest struct {
	Code   string `json:"code"`
	Action string `json:"action"`
}

type NotificationsResponse struct {
	Notifications []Notification `json:"notifications"`
	Unread        int64          `json:"unread"`
}

type OrderDetail struct {
	Order

	ActiveHolds []Hold `json:"activeHolds"`

	QCChecks []QCCheck `json:"qcChecks"`

	SLA SLA `json:"sla"`

	OnHold bool `json:"onHold"`
}

type CreateOrderRequest struct {
	OrderNumber string `json:"orderNumber"`

	TargetCompletion *time.Time `json:"targetCompletionAt"`
}

type TransitionRequest struct {
	Status Status `json:"status"`

	PerformedBy string `json:"performedBy"`

	ExpectedUpdatedAt *time.Time `json:"expectedUpdatedAt,omitempty"`
}

type HoldRequest struct {
	Reason    string `json:"reason"`
	CreatedBy string `json:"createdBy"`

	ExpectedUpdatedAt *time.Time `json:"expectedUpdatedAt,omitempty"`
}

type AssignRequest struct {
	Assignee string `json:"assignee"`
}

type BulkTransitionRequest struct {
	IDs    []int64 `json:"ids"`
	Status Status  `json:"status"`
}

type BulkResolveRequest struct {
	OrderIDs []int64 `json:"orderIds"`
}

type BulkItemResult struct {
	ID      int64  `json:"id"`
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type QCRequest struct {
	Status      QCStatus `json:"status"`
	InspectorID string   `json:"inspectorId"`
	Notes       string   `json:"notes"`
}

type ChecklistItem struct {
	ID           int64      `json:"id"`
	OrderID      int64      `json:"orderId"`
	Seq          int        `json:"seq"`
	Artikel      string     `json:"artikel"`
	Omschrijving string     `json:"omschrijving"`
	Locatie      string     `json:"locatie"`
	Aantal       int        `json:"aantal"`
	CheckedBy    string     `json:"checkedBy,omitempty"`
	CheckedAt    *time.Time `json:"checkedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type ChecklistOverview struct {
	OrderID     int64           `json:"orderId"`
	OrderNumber string          `json:"orderNumber"`
	Status      string          `json:"status"`
	Total       int             `json:"total"`
	Done        int             `json:"done"`
	Items       []ChecklistItem `json:"items"`
}

type ChecklistItemInput struct {
	Artikel      string `json:"artikel"`
	Omschrijving string `json:"omschrijving"`
	Locatie      string `json:"locatie"`
	Aantal       int    `json:"aantal"`
}
