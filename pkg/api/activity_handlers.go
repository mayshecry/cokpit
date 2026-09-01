package api

import (
	"net/http"

	"cockpit/pkg/attention"
	"cockpit/pkg/db"
	"cockpit/pkg/mention"
	"cockpit/pkg/order"
)

func (s *Server) handleAttention(w http.ResponseWriter, r *http.Request) {
	orders, err := s.store.ListOrders(r.Context(), nil)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	holds, err := s.store.AllActiveHolds(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]attention.Projection{
		"attention": attention.Project(s.now().UTC(), orders, holds),
	})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	var req order.ScanRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	code := cleanString(req.Code)
	if code == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "code is required")
		return
	}
	action := cleanString(req.Action)
	if action == "" {
		action = "LOOKUP"
	}
	if action != "LOOKUP" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "action must be LOOKUP")
		return
	}

	o, err := s.store.OrderByNumber(r.Context(), code)
	if err != nil {
		if err == db.ErrNotFound {
			s.writeError(w, http.StatusNotFound, "no_match",
				"no order matches the scanned code "+code)
			return
		}
		status, c, msg := s.classifyError(err)
		s.writeError(w, status, c, msg)
		return
	}

	user := s.currentUser(r)
	event, err := s.store.RecordScan(r.Context(), o.ID, o.OrderNumber, user.Username, action, s.now().UTC())
	if err != nil {
		status, c, msg := s.classifyError(err)
		s.writeError(w, status, c, msg)
		return
	}

	holds, err := s.store.ActiveHolds(r.Context(), o.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	checks, err := s.store.QCChecks(r.Context(), o.ID)
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

	s.writeJSON(w, http.StatusOK, map[string]any{
		"matched": true,
		"action":  action,
		"order":   o,
		"detail":  detail,
		"event":   event,
	})
}

func (s *Server) handleListScans(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	if _, err := s.store.GetOrder(r.Context(), id); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	events, err := s.store.ScanEvents(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.ScanEvent{"scanEvents": events})
}

const maxCommentLen = 2000

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	if _, err := s.store.GetOrder(r.Context(), id); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	comments, err := s.store.Comments(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.Comment{"comments": comments})
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req order.CommentRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	body := cleanString(req.Body)
	if body == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "body is required")
		return
	}
	if len(body) > maxCommentLen {
		s.writeError(w, http.StatusBadRequest, "bad_request",
			"comment must be at most 2000 characters")
		return
	}

	o, err := s.store.GetOrder(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}

	parsed := mention.Parse(body)
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	known := make(map[string]bool, len(users))
	for _, u := range users {
		known[u.Username] = true
	}
	resolved := mention.Resolve(parsed, known)

	user := s.currentUser(r)
	comment, err := s.store.AddComment(r.Context(), o.ID, user.Username, user.DisplayName, body, resolved, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]order.Comment{"comment": comment})
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	unreadOnly := r.URL.Query().Get("unread") == "true"
	notes, unread, err := s.store.Notifications(r.Context(), s.currentUser(r).Username, unreadOnly, 100)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, order.NotificationsResponse{
		Notifications: notes,
		Unread:        unread,
	})
}

func (s *Server) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	for _, id := range req.IDs {
		if id <= 0 {
			s.writeError(w, http.StatusBadRequest, "bad_request", "ids must be positive integers")
			return
		}
	}
	marked, err := s.store.MarkNotificationsRead(r.Context(), s.currentUser(r).Username, req.IDs, s.now().UTC())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int64{"marked": marked})
}
