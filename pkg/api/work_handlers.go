package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
	"cockpit/pkg/order"
)

// workOrder resolves the order and enforces that the caller is the assignee
// (or an admin), which is the rule for clocking in on an order.
func (s *Server) workOrder(w http.ResponseWriter, r *http.Request) (order.Order, bool) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return order.Order{}, false
	}
	o, err := s.store.GetOrder(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return order.Order{}, false
	}
	me := s.currentUser(r)
	if o.Assignee != me.Username && me.Role != auth.RoleAdmin {
		s.writeError(w, http.StatusForbidden, "not_assignee", "this order is not assigned to you")
		return order.Order{}, false
	}
	return o, true
}

func (s *Server) handleGetWork(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	ws, err := s.store.ActiveWorkSession(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"session": ws})
}

func (s *Server) handleStartWork(w http.ResponseWriter, r *http.Request) {
	o, ok := s.workOrder(w, r)
	if !ok {
		return
	}
	if o.Status == order.StatusCompleted {
		s.writeError(w, http.StatusConflict, "order_completed", "completed orders cannot be clocked into")
		return
	}
	me := s.currentUser(r)
	ws, err := s.store.StartWorkSession(r.Context(), o.ID, me.Username, s.now().UTC())
	if err != nil {
		if errors.Is(err, db.ErrWorkSessionTaken) {
			s.writeError(w, http.StatusConflict, "session_taken", "another user is already clocked in on this order")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.publishOrder(o.ID, me.Username)
	s.writeJSON(w, http.StatusOK, map[string]any{"session": ws})
}

type workUpdateRequest struct {
	Step   int             `json:"step"`
	Checks json.RawMessage `json:"checks"`
}

func (s *Server) handleUpdateWork(w http.ResponseWriter, r *http.Request) {
	o, ok := s.workOrder(w, r)
	if !ok {
		return
	}
	var req workUpdateRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	checks := string(req.Checks)
	if len(checks) == 0 {
		checks = "{}"
	}
	if len(checks) > 8192 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "checks payload too large")
		return
	}
	var probe any
	if err := json.Unmarshal([]byte(checks), &probe); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "checks must be a JSON object")
		return
	}
	me := s.currentUser(r)
	ws, err := s.store.UpdateWorkSession(r.Context(), o.ID, me.Username, req.Step, checks)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, http.StatusConflict, "no_session", "no active work session on this order")
			return
		}
		if errors.Is(err, db.ErrWorkSessionTaken) {
			s.writeError(w, http.StatusConflict, "session_taken", "another user is clocked in on this order")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"session": ws})
}

type workEndRequest struct {
	Completed bool `json:"completed"`
}

func (s *Server) handleEndWork(w http.ResponseWriter, r *http.Request) {
	o, ok := s.workOrder(w, r)
	if !ok {
		return
	}
	var req workEndRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	me := s.currentUser(r)
	ws, err := s.store.EndWorkSession(r.Context(), o.ID, me.Username, s.now().UTC(), req.Completed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, http.StatusConflict, "no_session", "no active work session on this order")
			return
		}
		if errors.Is(err, db.ErrWorkSessionTaken) {
			s.writeError(w, http.StatusConflict, "session_taken", "another user is clocked in on this order")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.publishOrder(o.ID, me.Username)
	s.writeJSON(w, http.StatusOK, map[string]any{"session": ws})
}

func (s *Server) handleListMyWork(w http.ResponseWriter, r *http.Request) {
	me := s.currentUser(r)
	sessions, err := s.store.ActiveSessionsFor(r.Context(), me.Username)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}
