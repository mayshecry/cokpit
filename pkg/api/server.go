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
)

const maxBodyBytes = 1 << 20

type Server struct {
	store        *db.Store
	now          func() time.Time
	slaHrs       time.Duration
	sessionHours time.Duration
	logger       *log.Logger
}

type ServerConfig struct {
	Now func() time.Time

	SLATargetDefaultHours int

	SessionHours int

	Logger *log.Logger
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
	mux.HandleFunc("POST /api/v1/orders/{id}/holds", s.requireAuth(s.requirePerm(auth.PermHoldCreate, s.handlePlaceHold)))
	mux.HandleFunc("POST /api/v1/holds/{id}/resolve", s.requireAuth(s.requirePerm(auth.PermHoldResolve, s.handleResolveHold)))
	mux.HandleFunc("POST /api/v1/orders/{id}/qc", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleSubmitQC)))
	mux.HandleFunc("GET /api/v1/orders/{id}/audit", s.requireAuth(s.requirePerm(auth.PermAuditView, s.handleAudit)))
	mux.HandleFunc("GET /api/v1/orders/{id}/checklist", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetChecklist)))
	mux.HandleFunc("POST /api/v1/orders/{id}/checklist", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleSetChecklist)))
	mux.HandleFunc("POST /api/v1/checklist/{id}/tick", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleTickChecklistItem)))
	mux.HandleFunc("POST /api/v1/checklist/{id}/untick", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleUntickChecklistItem)))

	mux.HandleFunc("GET /api/v1/attention", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleAttention)))
	mux.HandleFunc("POST /api/v1/scan", s.requireAuth(s.requirePerm(auth.PermScanUse, s.handleScan)))
	mux.HandleFunc("GET /api/v1/orders/{id}/scans", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListScans)))
	mux.HandleFunc("GET /api/v1/orders/{id}/comments", s.requireAuth(s.requirePerm(auth.PermCommentRead, s.handleListComments)))
	mux.HandleFunc("POST /api/v1/orders/{id}/comments", s.requireAuth(s.requirePerm(auth.PermCommentPost, s.handleAddComment)))
	mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.handleListNotifications))
	mux.HandleFunc("POST /api/v1/notifications/read", s.requireAuth(s.handleMarkNotificationsRead))

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
	Code    string `json:"code"`
	Message string `json:"message"`
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
