package api

import (
	"net/http"
	"time"

	"cockpit/pkg/order"
)

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

func (s *Server) handleGetOrderWorkedBy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	users, err := s.store.GetWorkedByUsers(r.Context(), id)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleMarkOrderShared(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req order.MarkSharedRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	now := time.Now().UTC()
	if err := s.store.MarkOrderShared(r.Context(), id, req.ParentOrderID, now); err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.publishOrder(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]any{"shared": true})
}

func (s *Server) handleGetOrderSharedInfo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	info, err := s.store.GetOrderSharedInfo(r.Context(), id)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"shared": info})
}

func (s *Server) handleShareOrderWithUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req order.ShareOrderRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if req.SharedWith == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "sharedWith is required")
		return
	}
	now := time.Now().UTC()
	if err := s.store.ShareOrder(r.Context(), id, req.SharedWith, now); err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	if o, gerr := s.store.GetOrder(r.Context(), id); gerr == nil {
		_ = s.store.Notify(r.Context(), req.SharedWith, "share_invite", id, o.OrderNumber, id,
			"Order "+o.OrderNumber+" shared with you by @"+s.currentUser(r).Username, now)
		s.publishNotification(req.SharedWith)
	}
	s.publishOrder(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]any{"sharedWith": req.SharedWith})
}
