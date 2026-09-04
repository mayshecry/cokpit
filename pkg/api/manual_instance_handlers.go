package api

import (
	"net/http"
	"strings"

	"cockpit/pkg/order"
)

func (s *Server) handleInstantiateManual(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req order.InstantiateManualRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if req.ProductID <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "productId is required")
		return
	}
	manual, err := s.store.InstantiateManual(r.Context(), id, req.ProductID, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishManual(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusCreated, map[string]order.OrderManual{"manual": manual})
}

func (s *Server) handleListOrderManuals(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	manuals, err := s.store.OrderManuals(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.OrderManual{"manuals": manuals})
}

func (s *Server) handleAnswerManualBlock(w http.ResponseWriter, r *http.Request) {
	manualID, err := parseID(r, "mid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "manual id must be an integer")
		return
	}
	blockID, err := parseID(r, "bid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "block id must be an integer")
		return
	}
	var req order.ManualAnswerRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if !order.IsValidManualAnswer(req.Answer) {
		s.writeError(w, http.StatusBadRequest, "bad_request", "answer must be YES, NO or CLEAR")
		return
	}
	block, err := s.store.AnswerManualBlock(r.Context(), manualID, blockID, req.Answer, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishManual(manualID, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]order.OrderManualBlock{"block": block})
}

func (s *Server) handleFlagManualBlock(w http.ResponseWriter, r *http.Request) {
	manualID, err := parseID(r, "mid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "manual id must be an integer")
		return
	}
	blockID, err := parseID(r, "bid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "block id must be an integer")
		return
	}
	var req order.ManualFlagRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	set := r.URL.Query().Get("clear") != "true"
	if set && strings.TrimSpace(req.Reason) == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "a reason is required to flag a block")
		return
	}
	block, notified, err := s.store.FlagManualBlock(r.Context(), manualID, blockID, set, req.Reason, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishManual(manualID, s.currentUser(r).Username)
	if notified != "" {
		s.publishNotification(notified)
	}
	s.writeJSON(w, http.StatusOK, map[string]order.OrderManualBlock{"block": block})
}

func (s *Server) handleDeleteManual(w http.ResponseWriter, r *http.Request) {
	manualID, err := parseID(r, "mid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "manual id must be an integer")
		return
	}
	if err := s.store.DeleteOrderManual(r.Context(), manualID, s.currentUser(r).Username, s.now().UTC()); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListManuals(w http.ResponseWriter, r *http.Request) {
	overviews, err := s.store.ManualOverviews(r.Context())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.ManualOverview{"manuals": overviews})
}
