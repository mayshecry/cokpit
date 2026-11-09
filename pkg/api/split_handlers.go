package api

import (
	"errors"
	"net/http"
	"time"

	"cockpit/pkg/db"
	"cockpit/pkg/order"
)

func (s *Server) handleCreateOrderSplit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req order.SplitRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	req.RequestedBy = s.currentUser(r).Username
	now := time.Now().UTC()
	split, err := s.store.CreateOrderSplit(r.Context(), id, req, now)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "order not found")
			return
		}
		if err.Error() == "target user is required" || err.Error() == "target user does not exist" || err.Error() == "cannot split a completed order" {
			s.writeError(w, http.StatusBadRequest, "invalid_split", err.Error())
			return
		}
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"split": split})
}

func (s *Server) handleAcceptOrderSplit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "split id must be an integer")
		return
	}
	var req order.SplitAcceptRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	req.AcceptedBy = s.currentUser(r).Username
	now := time.Now().UTC()
	split, err := s.store.AcceptOrderSplit(r.Context(), id, req, now)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "split not found")
			return
		}
		if err.Error() == "split is already accepted" {
			s.writeError(w, http.StatusConflict, "already_accepted", err.Error())
			return
		}
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"split": split})
}

func (s *Server) handleListOrderSplits(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	splits, err := s.store.ListOrderSplits(r.Context(), status)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"splits": splits})
}

func (s *Server) handleGetOrderSplits(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	splits, err := s.store.GetOrderSplits(r.Context(), id)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"splits": splits})
}

func (s *Server) handleRecordWorkflowStepResult(w http.ResponseWriter, r *http.Request) {
	var req order.WorkflowStepResultRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	req.PerformedBy = s.currentUser(r).Username
	now := time.Now().UTC()
	result, err := s.store.RecordWorkflowStepResult(r.Context(), req, now)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"result": result})
}

func (s *Server) handleGetWorkflowStepResults(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	results, err := s.store.GetWorkflowStepResults(r.Context(), id)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleRerunWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req order.RerunWorkflowRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	req.PerformedBy = s.currentUser(r).Username
	req.OrderID = id
	now := time.Now().UTC()

	if _, err := s.store.RecordWorkflowStepResult(r.Context(), order.WorkflowStepResultRequest{
		OrderID:        id,
		StepKey:        "workflow_rerun",
		StepIndex:      -1,
		PerformedBy:    req.PerformedBy,
		Correct:        true,
		CustomerImpact: false,
		Notes:          "workflow rerun initiated",
	}, now); err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	o, err := s.store.GetOrder(r.Context(), id)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	if _, err := s.store.TransitionOrder(r.Context(), id, string(order.StatusProcessing), req.PerformedBy, now); err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.publishOrder(id, req.PerformedBy)
	s.writeJSON(w, http.StatusOK, map[string]any{"order": o, "reran": true})
}
