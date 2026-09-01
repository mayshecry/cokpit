package api

import (
	"net/http"

	"cockpit/pkg/order"
)

func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req order.CreateOrderRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}

	number := cleanString(req.OrderNumber)
	if number == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "orderNumber is required")
		return
	}

	now := s.now().UTC()
	target := now.Add(s.slaHrs)
	if req.TargetCompletion != nil {
		provided := req.TargetCompletion.UTC()
		if !provided.After(now) {
			s.writeError(w, http.StatusBadRequest, "bad_request", "targetCompletionAt must be in the future")
			return
		}
		target = provided
	}

	created, err := s.store.CreateOrder(r.Context(), number, target, now, s.currentUser(r).Username)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]order.Order{"order": created})
}

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	var filter *order.Status
	if raw := r.URL.Query().Get("status"); raw != "" {
		st := order.Status(raw)
		if !order.IsValidStatus(st) {
			s.writeError(w, http.StatusBadRequest, "invalid_status",
				"invalid status filter; allowed: Received, Processing, QC_Review, Completed, On_Hold")
			return
		}
		filter = &st
	}

	orders, err := s.store.ListOrders(r.Context(), filter)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.Order{"orders": orders})
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	o, err := s.store.GetOrder(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}

	holds, err := s.store.ActiveHolds(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	checks, err := s.store.QCChecks(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	detail := order.OrderDetail{
		Order:       o,
		ActiveHolds: holds,
		QCChecks:    checks,
		SLA:         order.ComputeSLA(o.TargetCompletion, s.now().UTC()),
		OnHold:      len(holds) > 0,
	}
	s.writeJSON(w, http.StatusOK, map[string]order.OrderDetail{"order": detail})
}

func (s *Server) handleTransition(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	var req order.TransitionRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if !validCommandStatus(req.Status) {
		s.writeError(w, http.StatusBadRequest, "invalid_status",
			"status must be one of Processing, QC_Review, Completed")
		return
	}

	updated, err := s.store.TransitionOrder(r.Context(), id, string(req.Status), s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]order.Order{"order": updated})
}
