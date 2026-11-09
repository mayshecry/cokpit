package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cockpit/pkg/db"
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
		if err := order.ValidateTargetCompletion(provided, now); err != nil {
			s.writeError(w, http.StatusBadRequest, targetErrorCode(err), err.Error())
			return
		}
		target = provided
	}

	customerName := cleanString(req.CustomerName)
	profileID := int64(0)
	profileName := ""
	if req.WorkflowProfileID < 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "workflowProfileId must be positive")
		return
	}
	if req.WorkflowProfileID > 0 {
		profile, profileErr := s.store.GetWorkflowProfile(r.Context(), req.WorkflowProfileID)
		if errors.Is(profileErr, db.ErrWorkflowProfileNotFound) {
			s.writeError(w, http.StatusBadRequest, "invalid_workflow_profile", "selected workflow profile does not exist")
			return
		}
		if profileErr != nil {
			s.writeInternalError(w, r, profileErr)
			return
		}
		if !strings.EqualFold(strings.TrimSpace(profile.Customer), customerName) {
			s.writeError(w, http.StatusBadRequest, "invalid_workflow_profile", "selected workflow profile belongs to a different customer")
			return
		}
		profileID = profile.ID
		profileName = profile.Name
	}
	created, err := s.store.CreateOrderWithWorkflow(r.Context(), number, target, now, s.currentUser(r).Username, customerName, profileID, profileName)
	if err != nil {
		if errors.Is(err, db.ErrDuplicateOrderNumber) {

			if existing, lookupErr := s.store.OrderByNumber(r.Context(), number); lookupErr == nil {
				s.writeJSON(w, http.StatusConflict, apiError{
					Error: apiErrorBody{
						Code:    "duplicate_order_number",
						Message: "an order named " + number + " already exists",
						Extra: map[string]any{
							"existingOrder": map[string]any{
								"id":          existing.ID,
								"orderNumber": existing.OrderNumber,
								"status":      string(existing.Status),
							},
						},
					},
				})
				return
			}
		}
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(created.ID, s.currentUser(r).Username)

	info := order.OmnitrackerInfoRequest{
		DebitNumber:       cleanString(req.DebitNumber),
		CustomerName:      cleanString(req.CustomerName),
		OmnitrackerTicket: cleanString(req.OmnitrackerTicket),
		Device:            cleanString(req.Device),
		AssetNumber:       cleanString(req.AssetNumber),
		Configuration:     cleanString(req.Configuration),
	}
	if info.DebitNumber != "" || info.CustomerName != "" || info.OmnitrackerTicket != "" ||
		info.Device != "" || info.AssetNumber != "" || info.Configuration != "" {
		if updated, err := s.store.UpdateOmnitrackerInfo(r.Context(), created.ID, info, s.now().UTC()); err == nil {
			created = updated
			s.publishOrder(created.ID, s.currentUser(r).Username)
		}
	}
	sla := order.ComputeSLAPaused(created.TargetCompletion, s.now().UTC(), created.PausedSeconds, nil)
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"order":        created,
		"targetIsPast": order.IsPastTarget(created.TargetCompletion, s.now().UTC()),
		"sla":          sla,
	})
}

func targetErrorCode(err error) string {
	if errors.Is(err, order.ErrTargetTooFar) {
		return "target_too_far"
	}
	return "invalid_target"
}

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	q := db.ListQuery{}
	if raw := r.URL.Query().Get("status"); raw != "" {
		st := order.Status(raw)
		if !order.IsValidStatus(st) {
			s.writeError(w, http.StatusBadRequest, "invalid_status",
				"invalid status filter; allowed: Received, Processing, QC_Review, Completed, On_Hold")
			return
		}
		q.Status = &st
	}
	if raw := r.URL.Query().Get("assignee"); raw == "me" {
		q.Assignee = s.currentUser(r).Username
	} else if raw == "unassigned" {
		q.OnlyUnassigned = true
	} else if raw != "" {
		q.Assignee = raw
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 5000 {
			q.Limit = n
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			q.Offset = n
		}
	}

	orders, total, err := s.store.ListOrdersFiltered(r.Context(), q)
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	s.annotateOrders(r.Context(), orders)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"orders": orders,
		"total":  total,
	})
}

func (s *Server) annotateOrders(ctx context.Context, orders []order.Order) {
	if len(orders) == 0 {
		return
	}
	starts, err := s.store.ActiveHoldStarts(ctx)
	if err != nil {
		starts = map[int64]time.Time{}
	}
	now := s.now().UTC()
	for i := range orders {
		o := &orders[i]
		if hs, ok := starts[o.ID]; ok {
			t := hs
			o.HoldSince = &t
		}
		sla := order.ComputeSLAPaused(o.TargetCompletion, now, o.PausedSeconds, o.HoldSince)
		o.SLA = &sla
	}
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
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}

	holds, err := s.store.ActiveHolds(r.Context(), id)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	checks, err := s.store.QCChecks(r.Context(), id)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	var holdSince *time.Time
	if len(holds) > 0 {
		oldest := holds[0]
		for _, h := range holds {
			if h.CreatedAt.Before(oldest.CreatedAt) {
				oldest = h
			}
		}
		holdSince = &oldest.CreatedAt
	}
	detail := order.OrderDetail{
		Order:       o,
		ActiveHolds: holds,
		QCChecks:    checks,
		SLA:         order.ComputeSLAPaused(o.TargetCompletion, s.now().UTC(), o.PausedSeconds, holdSince),
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
			"status must be one of Received, Processing, QC_Review, Completed")
		return
	}

	updated, err := s.store.TransitionOrderGuarded(r.Context(), id, string(req.Status), s.currentUser(r).Username, req.ExpectedUpdatedAt, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]order.Order{"order": updated})
}

func (s *Server) handleUpdateOmnitrackerInfo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	var req order.OmnitrackerInfoRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}

	updated, err := s.store.UpdateOmnitrackerInfo(r.Context(), id, req, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]order.Order{"order": updated})
}

func (s *Server) handleGenerateBarcode(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	var req order.GenerateBarcodeRequest
	if err := s.decodeJSON(w, r, &req); err != nil {

	}

	barcode, err := s.store.GenerateBarcode(r.Context(), id, req.Regenerate, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"barcode": barcode})
}

func (s *Server) handleGetBarcode(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	o, err := s.store.GetOrder(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"barcode": o.Barcode})
}

func (s *Server) handleScanBarcode(w http.ResponseWriter, r *http.Request) {
	barcode := r.URL.Query().Get("code")
	if barcode == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "barcode is required")
		return
	}

	o, err := s.store.OrderByBarcode(r.Context(), barcode)
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}

	deviceInfo := r.UserAgent()
	scanType := r.URL.Query().Get("type")
	if scanType == "" {
		scanType = "view"
	}
	_, _ = s.store.RecordBarcodeScan(r.Context(), o.ID, barcode, s.currentUser(r).Username, scanType, deviceInfo, s.now().UTC())

	s.writeJSON(w, http.StatusOK, map[string]any{
		"order":   o,
		"scanUrl": "/scan?code=" + barcode,
		"omniUrl": s.omnitrackerURL(o),
	})
}

func (s *Server) handleBarcodeScanHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}

	events, err := s.store.BarcodeScanHistory(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		if status == http.StatusInternalServerError {
			s.writeInternalError(w, r, err)
			return
		}
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) omnitrackerURL(o order.Order) string {
	if o.OmnitrackerTicket == "" {
		return ""
	}

	base := s.config.OmnitrackerBaseURL
	if base == "" {
		return ""
	}
	return base + "/ticket/" + o.OmnitrackerTicket
}
