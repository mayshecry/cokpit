package api

import (
	"net/http"
	"strconv"
)

// QCReasonCatalog is the structured set of fail reasons offered on the bench
// and in the drawer QC form; the same values feed the Quality "why it fails" panel.
var QCReasonCatalog = []string{
	"Cable seating out of spec",
	"Firmware not at baseline",
	"Config template wrong",
	"Self-test skipped",
	"Asset tag missing",
	"Cosmetic damage missed",
	"Paperwork incomplete (debit/ticket)",
}

// GET /api/v1/qc/queue — orders waiting on the QC bench (QC and above).
func (s *Server) handleQCQueue(w http.ResponseWriter, r *http.Request) {
	queue, err := s.store.QCQueue(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"queue": queue})
}

// GET /api/v1/qc/reasons — fail-reason catalog for QC submissions.
func (s *Server) handleQCReasons(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"reasons": QCReasonCatalog})
}

// GET /api/v1/qc/context/{id} — recent QC verdicts for the same customer or
// product, so the inspector sees patterns before calling the current order.
func (s *Server) handleQCContext(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid order id")
		return
	}
	customer, code, name, err := s.store.OrderContextKeys(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	recent, err := s.store.QCContext(r.Context(), id, customer, code, 6)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"customer":    customer,
		"productCode": code,
		"productName": name,
		"recent":      recent,
	})
}
