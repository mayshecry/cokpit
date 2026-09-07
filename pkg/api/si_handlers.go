package api

import (
	"errors"
	"net/http"
	"strconv"

	"cockpit/pkg/si"
)

func (s *Server) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	customers, err := s.store.ListCustomers(r.Context())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"customers": customers})
}

type createCustomerRequest struct {
	Number string `json:"number"`
	Name   string `json:"name"`
}

func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req createCustomerRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	customer, err := s.store.CreateCustomer(r.Context(), cleanString(req.Number), cleanString(req.Name), s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]si.Customer{"customer": customer})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	number := r.PathValue("number")
	customer, err := s.store.CustomerByNumber(r.Context(), number)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	projects, err := s.store.ListProjects(r.Context(), customer.ID)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

type createProjectRequest struct {
	CustomerID int64  `json:"customerId"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	project, err := s.store.CreateProject(r.Context(), req.CustomerID, cleanString(req.Code), cleanString(req.Name), req.Description, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]si.Project{"project": project})
}

func (s *Server) handleListSIs(w http.ResponseWriter, r *http.Request) {
	var status *si.Status
	if raw := r.URL.Query().Get("status"); raw != "" {
		st := si.Status(raw)
		if !si.IsValidStatus(st) {
			s.writeError(w, http.StatusBadRequest, "invalid_status", "invalid SI status filter")
			return
		}
		status = &st
	}
	var projectID int64
	if raw := r.URL.Query().Get("project"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "bad_request", "project filter must be an integer")
			return
		}
		projectID = n
	}
		sis, err := s.store.ListSIs(r.Context(), r.URL.Query().Get("customer"), projectID, status)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"sis": sis})
}

func (s *Server) handleCreateSI(w http.ResponseWriter, r *http.Request) {
	var req si.CreateSIRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	created, err := s.store.CreateSI(r.Context(), req, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]si.SI{"si": created})
}
func (s *Server) handleGetSI(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "SI id must be an integer")
		return
	}
	siRec, err := s.store.GetSI(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]si.SI{"si": siRec})
}

func (s *Server) handleUpdateSI(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "SI id must be an integer")
		return
	}
	var req si.UpdateSIRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	updated, err := s.store.UpdateSI(r.Context(), id, req, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]si.SI{"si": updated})
}

func (s *Server) handleTransitionSI(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "SI id must be an integer")
		return
	}
	var req si.TransitionRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if !si.IsValidStatus(req.Status) {
		s.writeError(w, http.StatusBadRequest, "invalid_status", "invalid SI status")
		return
	}
	updated, err := s.store.TransitionSI(r.Context(), id, req.Status, req.Note, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		if errors.Is(err, si.ErrInvalidTransition) {
			s.writeError(w, http.StatusConflict, "invalid_transition", err.Error())
			return
		}
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]si.SI{"si": updated})
}

func (s *Server) handleSetSIProjects(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "SI id must be an integer")
		return
	}
	var req si.SetProjectsRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	updated, err := s.store.SetSIProjects(r.Context(), id, req.ProjectIDs, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]si.SI{"si": updated})
}

func (s *Server) handleSIEvents(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "SI id must be an integer")
		return
	}
	events, err := s.store.SIEvents(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleSeedDemoSI(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.SeedDemoSI(r.Context(), s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"seeded": count})
}

// --- Project checklist handlers ---

func (s *Server) handleListChecklist(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseID(r, "projectId")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "project id must be an integer")
		return
	}
	items, err := s.store.ProjectChecklist(r.Context(), projectID)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAddChecklistItem(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseID(r, "projectId")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "project id must be an integer")
		return
	}
	var req si.AddChecklistItemRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	item, err := s.store.AddProjectChecklistItem(r.Context(), projectID, req.Label, req.Description, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]si.ProjectChecklistItem{"item": item})
}

func (s *Server) handleSITickChecklistItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseID(r, "itemId")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "item id must be an integer")
		return
	}
	username := s.currentUser(r).Username
	item, err := s.store.TickProjectChecklistItem(r.Context(), itemID, username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]si.ProjectChecklistItem{"item": item})
}

func (s *Server) handleSIUntickChecklistItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseID(r, "itemId")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "item id must be an integer")
		return
	}
	item, err := s.store.UntickProjectChecklistItem(r.Context(), itemID, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]si.ProjectChecklistItem{"item": item})
}

func (s *Server) handleDeleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseID(r, "itemId")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "item id must be an integer")
		return
	}
	if err := s.store.DeleteProjectChecklistItem(r.Context(), itemID); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}