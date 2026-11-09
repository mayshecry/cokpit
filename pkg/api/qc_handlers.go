package api

import (
	"net/http"
	"strconv"
)

var QCReasonCatalog = []string{
	"Cable seating out of spec",
	"Firmware not at baseline",
	"Config template wrong",
	"Self-test skipped",
	"Asset tag missing",
	"Cosmetic damage missed",
	"Paperwork incomplete (debit/ticket)",
}

func (s *Server) handleQCQueue(w http.ResponseWriter, r *http.Request) {
	queue, err := s.store.QCQueue(r.Context())
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"queue": queue})
}

func (s *Server) handleQCReasons(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"reasons": QCReasonCatalog})
}

func (s *Server) handleQCContext(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid order id")
		return
	}
	customer, code, name, err := s.store.OrderContextKeys(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	recent, err := s.store.QCContext(r.Context(), id, customer, code, 6)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"customer":    customer,
		"productCode": code,
		"productName": name,
		"recent":      recent,
	})
}
