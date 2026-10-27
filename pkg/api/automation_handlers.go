package api

import (
	"context"
	"net/http"
	"strconv"

	"cockpit/pkg/automation"
)

func (s *Server) handleListAutomationRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.store.ListAutomationRules(r.Context(), false)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleCreateAutomationRule(w http.ResponseWriter, r *http.Request) {
	var rule automation.Rule
	if err := s.decodeJSON(w, r, &rule); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	rule.ID = 0
	rule.CreatedBy = s.currentUser(r).Username
	created, err := s.store.CreateAutomationRule(r.Context(), rule, rule.CreatedBy, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishAutomation()
	s.writeJSON(w, http.StatusCreated, map[string]any{"rule": created})
}

func (s *Server) handleUpdateAutomationRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid automation rule id")
		return
	}
	var rule automation.Rule
	if err := s.decodeJSON(w, r, &rule); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	rule.ID = id
	updated, err := s.store.UpdateAutomationRule(r.Context(), rule, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishAutomation()
	s.writeJSON(w, http.StatusOK, map[string]any{"rule": updated})
}

func (s *Server) handleDeleteAutomationRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid automation rule id")
		return
	}
	if err := s.store.DeleteAutomationRule(r.Context(), id); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishAutomation()
	s.writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// handlePreviewAutomationRule is a dry run: it reports which orders a rule
// would act on right now and who it would reach, and writes nothing.
func (s *Server) handlePreviewAutomationRule(w http.ResponseWriter, r *http.Request) {
	var rule automation.Rule
	if err := s.decodeJSON(w, r, &rule); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 25 {
				n = 25
			}
			limit = n
		}
	}
	preview, err := s.store.PreviewAutomation(r.Context(), rule, s.now().UTC(), limit)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		// Clamp: the value goes straight into SQL LIMIT, so a negative or huge
		// number must not be able to dump (or starve) the whole table.
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}
	runs, err := s.store.ListAutomationRuns(r.Context(), limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleEvaluateAutomations(w http.ResponseWriter, r *http.Request) {
	runs, err := s.RunAutomations(r.Context())
	if err != nil {
		s.logger.Printf("automation evaluation: %v", err)
		s.writeError(w, http.StatusInternalServerError, "automation_evaluation_failed", "automation evaluation failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "executed": len(runs)})
}

func (s *Server) RunAutomations(ctx context.Context) ([]automation.Run, error) {
	s.automationMu.Lock()
	defer s.automationMu.Unlock()
	runs, err := s.store.EvaluateAutomations(ctx, s.now().UTC())
	if len(runs) > 0 {
		s.publishAutomation()
	}
	for _, run := range runs {
		if run.Action == automation.ActionAssign || run.Action == automation.ActionEscalate {
			s.publishOrder(run.OrderID, "automation")
		}
		for _, username := range run.NotifiedUsers {
			s.publishNotification(username)
		}
	}
	return runs, err
}
