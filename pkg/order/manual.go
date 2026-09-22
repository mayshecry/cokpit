package order

import "time"

type Product struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	BlockCount  int       `json:"blockCount"`
	Department  string    `json:"department,omitempty"`
	ApprovedBy  string    `json:"approvedBy,omitempty"`
	ApprovedAt  *time.Time `json:"approvedAt,omitempty"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ManualBlock struct {
	ID        int64     `json:"id"`
	ProductID int64     `json:"productId"`
	Seq       int       `json:"seq"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Assignee  string    `json:"assignee,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type OrderManual struct {
	ID          int64              `json:"id"`
	OrderID     int64              `json:"orderId"`
	ProductID   int64              `json:"productId"`
	ProductCode string             `json:"productCode"`
	ProductName string             `json:"productName"`
	CreatedBy   string             `json:"createdBy"`
	CreatedAt   time.Time          `json:"createdAt"`
	Blocks      []OrderManualBlock `json:"blocks"`
	Log         []ManualTickEvent  `json:"log"`
	Total       int                `json:"total"`
	Answered    int                `json:"answered"`
	Failed      int                `json:"failed"`
	Flagged     int                `json:"flagged"`
}

type OrderManualBlock struct {
	ID         int64      `json:"id"`
	ManualID   int64      `json:"manualId"`
	Seq        int        `json:"seq"`
	Title      string     `json:"title"`
	Body       string     `json:"body,omitempty"`
	Assignee   string     `json:"assignee,omitempty"`
	Answer     string     `json:"answer,omitempty"`
	AnsweredBy string     `json:"answeredBy,omitempty"`
	AnsweredAt *time.Time `json:"answeredAt,omitempty"`
	Flagged    bool       `json:"flagged"`
	FlaggedBy  string     `json:"flaggedBy,omitempty"`
	FlaggedAt  *time.Time `json:"flaggedAt,omitempty"`
	FlagReason string     `json:"flagReason,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type ManualTickEvent struct {
	ID        int64     `json:"id"`
	ManualID  int64     `json:"manualId"`
	BlockID   int64     `json:"blockId"`
	Action    string    `json:"action"`
	Username  string    `json:"username"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type ManualOverview struct {
	ManualID    int64              `json:"manualId"`
	OrderID     int64              `json:"orderId"`
	OrderNumber string             `json:"orderNumber"`
	Status      string             `json:"status"`
	ProductID   int64              `json:"productId"`
	ProductCode string             `json:"productCode"`
	ProductName string             `json:"productName"`
	CreatedBy   string             `json:"createdBy"`
	CreatedAt   time.Time          `json:"createdAt"`
	Total       int                `json:"total"`
	Answered    int                `json:"answered"`
	Failed      int                `json:"failed"`
	Flagged     int                `json:"flagged"`
	Blocks      []OrderManualBlock `json:"blocks"`
}

const (
	AnswerYes   = "YES"
	AnswerNo    = "NO"
	AnswerClear = "CLEAR"
)

func IsValidManualAnswer(a string) bool {
	return a == AnswerYes || a == AnswerNo || a == AnswerClear
}

type CreateProductRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Department  string `json:"department"`
}

type UpdateProductRequest struct {
	Code        *string `json:"code,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type QuickManualRequest struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	Department string   `json:"department"`
	Steps      []string `json:"steps"`
}

type ProductDepartmentRequest struct {
	Department string `json:"department"`
}

type ManualBlockRequest struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Assignee string `json:"assignee"`
}

type ManualBlockPatchRequest struct {
	Title    *string `json:"title,omitempty"`
	Body     *string `json:"body,omitempty"`
	Assignee *string `json:"assignee,omitempty"`
}

type ManualBlockMoveRequest struct {
	Direction string `json:"direction"`
}

type InstantiateManualRequest struct {
	ProductID int64 `json:"productId"`
}

type ManualAnswerRequest struct {
	Answer string `json:"answer"`
}

type ManualFlagRequest struct {
	Reason string `json:"reason"`
}

const (
	FlagSet   = "FLAG"
	FlagClear = "UNFLAG"
)

func IsValidManualFlagAction(a string) bool {
	return a == FlagSet || a == FlagClear
}

type ProductResponse struct {
	Product Product       `json:"product"`
	Blocks  []ManualBlock `json:"blocks"`
}
