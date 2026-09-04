package api

import (
	"net/http"

	"cockpit/pkg/order"
)

type checklistRequest struct {
	Items []order.ChecklistItemInput `json:"items"`
}

func (s *Server) handleGetChecklist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	items, err := s.store.ChecklistItems(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.ChecklistItem{"items": items})
}

func (s *Server) handleSetChecklist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req checklistRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if len(req.Items) == 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "items must not be empty")
		return
	}
	items, err := s.store.SetChecklist(r.Context(), id, req.Items, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishChecklist(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string][]order.ChecklistItem{"items": items})
}

func (s *Server) handleTickChecklistItem(w http.ResponseWriter, r *http.Request) {
	s.toggleChecklistItem(w, r, true)
}

func (s *Server) handleUntickChecklistItem(w http.ResponseWriter, r *http.Request) {
	s.toggleChecklistItem(w, r, false)
}

func (s *Server) toggleChecklistItem(w http.ResponseWriter, r *http.Request, tick bool) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "checklist item id must be an integer")
		return
	}
	username := s.currentUser(r).Username
	var item order.ChecklistItem
	if tick {
		item, err = s.store.TickChecklistItem(r.Context(), id, username, s.now().UTC())
	} else {
		item, err = s.store.UntickChecklistItem(r.Context(), id, username, s.now().UTC())
	}
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishChecklist(item.OrderID, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]order.ChecklistItem{"item": item})
}

func (s *Server) handleListChecklists(w http.ResponseWriter, r *http.Request) {
	overviews, err := s.store.ChecklistOverviews(r.Context())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.ChecklistOverview{"checklists": overviews})
}

func (s *Server) handleSeedDemoChecklists(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.SeedDemoChecklists(r.Context(), s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	if n > 0 {
		s.logger.Printf("seeded %d demo pick lists (loaded from the GUI)", n)
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"created": n})
}
