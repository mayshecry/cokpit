package api

import (
	"net/http"
	"strconv"

	"cockpit/pkg/auth"
	"cockpit/pkg/capa"
	"cockpit/pkg/db"
)

func (s *Server) handleListCAPA(w http.ResponseWriter, r *http.Request) {
	filter := db.CAPAFilter{}
	if raw := r.URL.Query().Get("status"); raw != "" {
		status := capa.Status(raw)
		if !capa.IsValidStatus(status) {
			s.writeError(w, http.StatusBadRequest, "invalid_status", "invalid CAPA status")
			return
		}
		filter.Status = &status
	}
	filter.Owner = r.URL.Query().Get("owner")
	if filter.Owner == "me" {
		filter.Owner = s.currentUser(r).Username
	}
	filter.OverdueOnly = r.URL.Query().Get("overdue") == "true" || r.URL.Query().Get("overdue") == "1"
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			filter.Limit = n
		}
	}
	now := s.now().UTC()
	actions, err := s.store.ListCAPA(r.Context(), filter, now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	summary, err := s.store.CAPASummary(r.Context(), now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"actions": actions, "summary": summary})
}

func (s *Server) handleGetCAPA(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid CAPA id")
		return
	}
	action, err := s.store.GetCAPA(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	events, err := s.store.CAPAEvents(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"action": action, "events": events})
}

func (s *Server) handleCreateCAPA(w http.ResponseWriter, r *http.Request) {
	var req capa.CreateRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	created, err := s.store.CreateCAPA(r.Context(), req, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishCAPAChange(created)
	s.writeJSON(w, http.StatusCreated, map[string]any{"action": created})
}

func (s *Server) handleUpdateCAPA(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid CAPA id")
		return
	}
	var req capa.UpdateRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	current, err := s.store.GetCAPA(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	user := s.currentUser(r)
	isQC := s.userHasPerm(r, user, auth.PermQCSubmit)
	if !isQC {
		isOwner := current.Owner == user.Username && s.userHasPerm(r, user, auth.PermOrderTransition)
		if !isOwner {
			s.writeError(w, http.StatusForbidden, "forbidden", "only the owner or QC can update this action")
			return
		}

		if req.Title != nil || req.FailReason != nil || req.Owner != nil || req.DueAt != nil {
			s.writeError(w, http.StatusForbidden, "forbidden", "only QC can change CAPA ownership, due date or classification")
			return
		}
	}
	updated, err := s.store.UpdateCAPA(r.Context(), id, req, user.Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishCAPAChange(updated)
	s.writeJSON(w, http.StatusOK, map[string]any{"action": updated})
}

func (s *Server) handleVerifyCAPA(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid CAPA id")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	verified, err := s.store.VerifyCAPA(r.Context(), id, req.Note, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishCAPAChange(verified)
	s.writeJSON(w, http.StatusOK, map[string]any{"action": verified})
}

func (s *Server) publishCAPAChange(action capa.CorrectiveAction) {
	if s.hub != nil {
		s.hub.publish(eventMessage{Type: "capa", OrderID: action.OrderID, By: "capa"})
	}
	s.publishOrder(action.OrderID, "capa")
	s.publishNotification(action.Owner)
	if action.CreatedBy != action.Owner {
		s.publishNotification(action.CreatedBy)
	}
}
