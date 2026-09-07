package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
	"cockpit/pkg/order"
	"cockpit/pkg/si"
)

const maxBodyBytes = 1 << 20

type Server struct {
	store        *db.Store
	now          func() time.Time
	slaHrs       time.Duration
	sessionHours time.Duration
	logger       *log.Logger
	hub          *eventHub
	config       ServerConfig
}

type ServerConfig struct {
	Now func() time.Time

	SLATargetDefaultHours int

	SessionHours int

	Logger *log.Logger

	// OmnitrackerBaseURL is the base URL for Omnitracker deep links (e.g., "https://omnitracker.example.com")
	OmnitrackerBaseURL string
}

func New(store *db.Store, cfg ServerConfig) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SLATargetDefaultHours <= 0 {
		cfg.SLATargetDefaultHours = 24
	}
	if cfg.SessionHours <= 0 {
		cfg.SessionHours = 24
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	return &Server{
		store:        store,
		now:          cfg.Now,
		slaHrs:       time.Duration(cfg.SLATargetDefaultHours) * time.Hour,
		sessionHours: time.Duration(cfg.SessionHours) * time.Hour,
		logger:       cfg.Logger,
		hub:          newEventHub(),
		config:       cfg,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("GET /api/v1/users", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleListUsers)))
	mux.HandleFunc("POST /api/v1/users", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleCreateUser)))
	mux.HandleFunc("POST /api/v1/users/{id}/role", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleSetUserRole)))
	mux.HandleFunc("POST /api/v1/users/{id}/password", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleResetPassword)))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleDeleteUser)))
	mux.HandleFunc("GET /api/v1/orders", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListOrders)))
	mux.HandleFunc("POST /api/v1/orders", s.requireAuth(s.requirePerm(auth.PermOrderCreate, s.handleCreateOrder)))
	mux.HandleFunc("GET /api/v1/orders/{id}", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetOrder)))
	mux.HandleFunc("POST /api/v1/orders/{id}/transition", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleTransition)))
	mux.HandleFunc("POST /api/v1/orders/{id}/assign", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleAssign)))
	mux.HandleFunc("POST /api/v1/orders/{id}/holds", s.requireAuth(s.requirePerm(auth.PermHoldCreate, s.handlePlaceHold)))
	mux.HandleFunc("POST /api/v1/holds/{id}/resolve", s.requireAuth(s.requirePerm(auth.PermHoldResolve, s.handleResolveHold)))
	mux.HandleFunc("POST /api/v1/orders/{id}/qc", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleSubmitQC)))
	mux.HandleFunc("GET /api/v1/orders/{id}/audit", s.requireAuth(s.requirePerm(auth.PermAuditView, s.handleAudit)))
	mux.HandleFunc("POST /api/v1/orders/bulk-transition", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleBulkTransition)))
	mux.HandleFunc("POST /api/v1/holds/bulk-resolve", s.requireAuth(s.requirePerm(auth.PermHoldResolve, s.handleBulkResolve)))
	mux.HandleFunc("GET /api/v1/orders/{id}/checklist", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetChecklist)))
	mux.HandleFunc("POST /api/v1/orders/{id}/checklist", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleSetChecklist)))
	mux.HandleFunc("POST /api/v1/checklist/{id}/tick", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleTickChecklistItem)))
	mux.HandleFunc("POST /api/v1/checklist/{id}/untick", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleUntickChecklistItem)))
	mux.HandleFunc("GET /api/v1/checklists", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleListChecklists)))
	mux.HandleFunc("POST /api/v1/checklists/demo", s.requireAuth(s.requirePerm(auth.PermOrderCreate, s.handleSeedDemoChecklists)))
	mux.HandleFunc("GET /api/v1/products", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListProducts)))
	mux.HandleFunc("POST /api/v1/products", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleCreateProduct)))
	mux.HandleFunc("GET /api/v1/products/{id}", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetProduct)))
	mux.HandleFunc("POST /api/v1/products/{id}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleUpdateProduct)))
	mux.HandleFunc("DELETE /api/v1/products/{id}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleDeleteProduct)))
	mux.HandleFunc("POST /api/v1/products/{id}/blocks", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleAddManualBlock)))
	mux.HandleFunc("POST /api/v1/products/{pid}/blocks/{bid}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleUpdateManualBlock)))
	mux.HandleFunc("POST /api/v1/products/{pid}/blocks/{bid}/move", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleMoveManualBlock)))
	mux.HandleFunc("DELETE /api/v1/products/{pid}/blocks/{bid}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleDeleteManualBlock)))
	mux.HandleFunc("POST /api/v1/orders/{id}/manuals", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleInstantiateManual)))
	mux.HandleFunc("GET /api/v1/orders/{id}/manuals", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleListOrderManuals)))
	mux.HandleFunc("POST /api/v1/manuals/{mid}/blocks/{bid}/answer", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleAnswerManualBlock)))
	mux.HandleFunc("POST /api/v1/manuals/{mid}/blocks/{bid}/flag", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleFlagManualBlock)))
	mux.HandleFunc("DELETE /api/v1/manuals/{mid}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleDeleteManual)))
	mux.HandleFunc("GET /api/v1/manuals", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleListManuals)))
	mux.HandleFunc("GET /api/v1/attention", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleAttention)))
	mux.HandleFunc("POST /api/v1/scan", s.requireAuth(s.requirePerm(auth.PermScanUse, s.handleScan)))
	mux.HandleFunc("GET /api/v1/orders/{id}/scans", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListScans)))
	mux.HandleFunc("GET /api/v1/orders/{id}/comments", s.requireAuth(s.requirePerm(auth.PermCommentRead, s.handleListComments)))
	mux.HandleFunc("POST /api/v1/orders/{id}/comments", s.requireAuth(s.requirePerm(auth.PermCommentPost, s.handleAddComment)))

	// Barcode / Omnitracker endpoints
	mux.HandleFunc("GET /api/v1/orders/{id}/barcode", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetBarcode)))
	mux.HandleFunc("POST /api/v1/orders/{id}/barcode", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleGenerateBarcode)))
	mux.HandleFunc("POST /api/v1/orders/{id}/omnitracker", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleUpdateOmnitrackerInfo)))
	mux.HandleFunc("GET /api/v1/orders/{id}/barcode/history", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleBarcodeScanHistory)))
	mux.HandleFunc("GET /api/v1/scan/barcode", s.requireAuth(s.requirePerm(auth.PermScanUse, s.handleScanBarcode)))

	mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.handleListNotifications))
	mux.HandleFunc("POST /api/v1/notifications/read", s.requireAuth(s.handleMarkNotificationsRead))
	mux.HandleFunc("GET /api/v1/users/brief", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListUsersBrief)))
	mux.HandleFunc("GET /api/v1/si/customers", s.requireAuth(s.requirePerm(auth.PermSIList, s.handleListCustomers)))
	mux.HandleFunc("POST /api/v1/si/customers", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleCreateCustomer)))
	mux.HandleFunc("GET /api/v1/si/customers/{number}/projects", s.requireAuth(s.requirePerm(auth.PermSIList, s.handleListProjects)))
	mux.HandleFunc("POST /api/v1/si/projects", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleCreateProject)))
	mux.HandleFunc("GET /api/v1/si/sis", s.requireAuth(s.requirePerm(auth.PermSIList, s.handleListSIs)))
	mux.HandleFunc("POST /api/v1/si/sis", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleCreateSI)))
	mux.HandleFunc("GET /api/v1/si/sis/{id}", s.requireAuth(s.requirePerm(auth.PermSIView, s.handleGetSI)))
	mux.HandleFunc("POST /api/v1/si/sis/{id}", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleUpdateSI)))
	mux.HandleFunc("POST /api/v1/si/sis/{id}/transition", s.requireAuth(s.requirePerm(auth.PermSITransition, s.handleTransitionSI)))
	mux.HandleFunc("POST /api/v1/si/sis/{id}/projects", s.requireAuth(s.requirePerm(auth.PermSIProjects, s.handleSetSIProjects)))
	mux.HandleFunc("GET /api/v1/si/sis/{id}/events", s.requireAuth(s.requirePerm(auth.PermSIView, s.handleSIEvents)))
	mux.HandleFunc("POST /api/v1/si/seed", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleSeedDemoSI)))
	mux.HandleFunc("GET /api/v1/si/projects/{projectId}/checklist", s.requireAuth(s.requirePerm(auth.PermSIView, s.handleListChecklist)))
	mux.HandleFunc("POST /api/v1/si/projects/{projectId}/checklist", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleAddChecklistItem)))
	mux.HandleFunc("POST /api/v1/si/projects/checklist/{itemId}/tick", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleSITickChecklistItem)))
	mux.HandleFunc("POST /api/v1/si/projects/checklist/{itemId}/untick", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleSIUntickChecklistItem)))
	mux.HandleFunc("DELETE /api/v1/si/projects/checklist/{itemId}", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleDeleteChecklistItem)))
	mux.HandleFunc("GET /api/v1/admin/backup", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleAdminBackup)))
	mux.HandleFunc("GET /api/v1/events", s.requireAuthSSE(s.requirePerm(auth.PermOrderList, s.handleEvents)))

	mux.HandleFunc("/api/", s.handleAPINotFound)
	mux.Handle("/", http.FileServer(http.Dir("frontend")))

	return s.recoverMiddleware(s.logMiddleware(mux))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			s.logger.Printf("json encode: %v", err)
		}
	}
}

type apiError struct {
	Error apiErrorBody `json:"error"`
}

type apiErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Extra   map[string]any `json:"extra,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, apiError{Error: apiErrorBody{Code: code, Message: message}})
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("request body contains trailing data")
	}
	return nil
}

func parseID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func (s *Server) classifyError(err error) (int, string, string) {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, db.ErrDuplicateOrderNumber):
		return http.StatusConflict, "duplicate_order_number", err.Error()
	case errors.Is(err, db.ErrDuplicateUsername):
		return http.StatusConflict, "duplicate_username", err.Error()
	case errors.Is(err, db.ErrDuplicateProductCode):
		return http.StatusConflict, "duplicate_product_code", err.Error()
	case errors.Is(err, db.ErrDuplicateManual):
		return http.StatusConflict, "duplicate_manual", err.Error()
	case errors.Is(err, db.ErrUnknownAssignee):
		return http.StatusBadRequest, "unknown_assignee", err.Error()
	case errors.Is(err, db.ErrBadAnswer):
		return http.StatusBadRequest, "invalid_answer", err.Error()
	case errors.Is(err, db.ErrBadFlagReason):
		return http.StatusBadRequest, "flag_reason_required", err.Error()
	case errors.Is(err, db.ErrFlagUnanswered):
		return http.StatusConflict, "flag_unanswered", err.Error()
	case errors.Is(err, db.ErrManualEdge):
		return http.StatusConflict, "manual_edge", err.Error()
	case errors.Is(err, db.ErrHoldActive):
		return http.StatusConflict, "hold_active", err.Error()
	case errors.Is(err, db.ErrHoldResolved):
		return http.StatusConflict, "hold_already_resolved", err.Error()
	case errors.Is(err, db.ErrNoQCPass):
		return http.StatusConflict, "qc_not_passed", err.Error()
	case errors.Is(err, order.ErrInvalidStatus):
		return http.StatusBadRequest, "invalid_status", err.Error()
	case errors.Is(err, order.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition", err.Error()
	case errors.Is(err, si.ErrInvalidStatus):
		return http.StatusBadRequest, "invalid_status", err.Error()
	case errors.Is(err, si.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition", err.Error()
	case errors.Is(err, si.ErrValidation):
		return http.StatusBadRequest, "validation_error", err.Error()
	case errors.Is(err, db.ErrStaleWrite):
		return http.StatusConflict, "stale_write", err.Error()
	case errors.Is(err, order.ErrActiveHold):
		return http.StatusConflict, "active_hold", err.Error()
	case errors.Is(err, order.ErrCompletedTransition):
		return http.StatusConflict, "order_completed", err.Error()
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error"
	}
}

func validCommandStatus(status order.Status) bool {
	switch status {
	case order.StatusProcessing, order.StatusQCReview, order.StatusCompleted:
		return true
	default:
		return false
	}
}

func cleanString(v string) string { return strings.TrimSpace(v) }
