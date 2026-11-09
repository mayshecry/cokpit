package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"cockpit/pkg/db"
)

type workflowProfileRequest struct {
	Name  string          `json:"name"`
	Steps json.RawMessage `json:"steps"`
}

func (s *Server) handleListWorkflowProfiles(w http.ResponseWriter, r *http.Request) {
	customer := strings.TrimSpace(r.URL.Query().Get("customer"))
	if customer == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "customer is required")
		return
	}
	profiles, err := s.store.ListWorkflowProfiles(r.Context(), customer)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *Server) handleGetWorkflowProfile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "workflow profile id must be an integer")
		return
	}
	profile, err := s.store.GetWorkflowProfile(r.Context(), id)
	if errors.Is(err, db.ErrWorkflowProfileNotFound) {
		s.writeError(w, http.StatusNotFound, "not_found", "workflow profile not found")
		return
	}
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	var steps any
	if err := json.Unmarshal([]byte(profile.StepsJSON), &steps); err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	profile.Steps = steps
	profile.StepsJSON = ""
	s.writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}

func (s *Server) handleCreateWorkflowProfile(w http.ResponseWriter, r *http.Request) {
	customer := strings.TrimSpace(r.URL.Query().Get("customer"))
	if customer == "" || utf8.RuneCountInString(customer) > 80 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "a customer name of 1 to 80 characters is required")
		return
	}
	var req workflowProfileRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "profile name must contain 1 to 80 characters")
		return
	}
	steps, err := validateWorkFlow(req.Steps)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if steps == nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "a workflow profile needs at least one step")
		return
	}
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	by := s.currentUser(r).Username
	profile, err := s.store.CreateWorkflowProfile(r.Context(), customer, name, string(stepsJSON), by, s.now().UTC())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			s.writeError(w, http.StatusConflict, "profile_exists", "a workflow profile with that name already exists for this customer")
			return
		}
		s.writeInternalError(w, r, err)
		return
	}
	profile.Steps = steps
	profile.StepsJSON = ""
	s.writeJSON(w, http.StatusCreated, map[string]any{"profile": profile})
}

func (s *Server) handleUpdateWorkflowProfile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "workflow profile id must be an integer")
		return
	}
	var req workflowProfileRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "profile name must contain 1 to 80 characters")
		return
	}
	steps, err := validateWorkFlow(req.Steps)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if steps == nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "a workflow profile needs at least one step")
		return
	}
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "could not encode workflow")
		return
	}
	profile, err := s.store.UpdateWorkflowProfile(r.Context(), id, name, string(stepsJSON), s.currentUser(r).Username, s.now().UTC())
	if errors.Is(err, db.ErrWorkflowProfileNotFound) {
		s.writeError(w, http.StatusNotFound, "not_found", "workflow profile not found")
		return
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			s.writeError(w, http.StatusConflict, "profile_exists", "a workflow profile with that name already exists for this customer")
			return
		}
		s.writeInternalError(w, r, err)
		return
	}
	profile.Steps = steps
	profile.StepsJSON = ""
	s.writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}
