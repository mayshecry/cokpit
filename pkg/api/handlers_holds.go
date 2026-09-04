package api

import (
	"net/http"

	"cockpit/pkg/order"
)

func (s *Server) handlePlaceHold(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	var req order.HoldRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	reason := cleanString(req.Reason)
	if reason == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "reason is required")
		return
	}

	hold, updated, err := s.store.PlaceHoldGuarded(r.Context(), id, reason, s.currentUser(r).Username, req.ExpectedUpdatedAt, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"hold":  hold,
		"order": updated,
	})
}

func (s *Server) handleResolveHold(w http.ResponseWriter, r *http.Request) {
	holdID, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "hold id must be an integer")
		return
	}

	hold, updated, err := s.store.ResolveHold(r.Context(), holdID, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(hold.OrderID, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"hold":  hold,
		"order": updated,
	})
}

func (s *Server) handleSubmitQC(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	var req order.QCRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if !order.IsValidQC(req.Status) {
		s.writeError(w, http.StatusBadRequest, "invalid_status", "status must be PASS or FAIL")
		return
	}
	inspector := cleanString(req.InspectorID)
	if inspector == "" {
		inspector = s.currentUser(r).Username
	}

	check, err := s.store.SubmitQC(r.Context(), id, req.Status, inspector, req.Notes, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusCreated, map[string]order.QCCheck{"qcCheck": check})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	logs, err := s.store.AuditLog(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.AuditLog{"auditLogs": logs})
}
