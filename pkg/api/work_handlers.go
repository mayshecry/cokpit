package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

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

/* ---- configurable work flow ---- */

type wfCheck struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type wfStep struct {
	Key    string    `json:"key"`
	Title  string    `json:"title"`
	Desc   string    `json:"desc"`
	Checks []wfCheck `json:"checks"`
	Next   *string   `json:"next"`
	Pick   bool      `json:"pick"`
	QcGate bool      `json:"qcGate"`
}

var wfNextAllowed = map[string]bool{"Received": true, "Processing": true, "QC_Review": true, "Completed": true}

func validateWorkFlow(raw json.RawMessage) ([]wfStep, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var steps []wfStep
	if err := json.Unmarshal([]byte(trimmed), &steps); err != nil {
		return nil, errors.New("steps must be an array")
	}
	if len(steps) < 1 || len(steps) > 8 {
		return nil, errors.New("a work flow needs between 1 and 8 steps")
	}
	seen := map[string]bool{}
	keyOk := func(k string) bool {
		if k == "" || len(k) > 24 {
			return false
		}
		for _, c := range k {
			if !(c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
				return false
			}
		}
		return true
	}
	for i, st := range steps {
		if !keyOk(st.Key) || seen[st.Key] {
			return nil, errors.New("every step needs a unique key (a-z, 0-9, - or _)")
		}
		seen[st.Key] = true
		if len(st.Title) > 60 || len(st.Desc) > 200 {
			return nil, errors.New("step title/description too long")
		}
		if len(st.Checks) > 12 {
			return nil, errors.New("max 12 checks per step")
		}
		cseen := map[string]bool{}
		for _, c := range st.Checks {
			if !keyOk(c.Key) || cseen[c.Key] {
				return nil, errors.New("check keys must be unique per step")
			}
			cseen[c.Key] = true
			if len(c.Label) > 120 || strings.TrimSpace(c.Label) == "" {
				return nil, errors.New("every check needs a short label")
			}
		}
		if st.Next != nil && !wfNextAllowed[*st.Next] {
			return nil, errors.New("unknown transition target on step " + fmt.Sprint(i+1))
		}
	}
	return steps, nil
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	value, err := s.store.GetSetting(r.Context(), "work_flow")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	resp := map[string]any{"steps": nil}
	if value != "" {
		var steps any
		if err := json.Unmarshal([]byte(value), &steps); err == nil {
			resp["steps"] = steps
		}
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePutWorkflow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Steps json.RawMessage `json:"steps"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	steps, err := validateWorkFlow(req.Steps)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	value := ""
	if steps != nil {
		value = string(req.Steps)
	}
	me := s.currentUser(r)
	if err := s.store.SetSetting(r.Context(), "work_flow", value, me.Username, s.now().UTC()); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"steps": steps})
}

func (s *Server) handleListAllWork(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ActiveSessionsAll(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}
