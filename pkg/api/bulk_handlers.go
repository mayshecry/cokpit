package api

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"cockpit/pkg/order"
)

func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return
	}
	var req order.AssignRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	assignee := cleanString(req.Assignee)
	if assignee != "" {
		users, err := s.store.ListUsers(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		known := false
		for _, u := range users {
			if u.Username == assignee {
				known = true
				break
			}
		}
		if !known {
			s.writeError(w, http.StatusBadRequest, "unknown_user", "no user named "+assignee)
			return
		}
	}

	updated, err := s.store.SetAssignee(r.Context(), id, assignee, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(id, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]order.Order{"order": updated})
}

func (s *Server) handleBulkTransition(w http.ResponseWriter, r *http.Request) {
	var req order.BulkTransitionRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if !validCommandStatus(req.Status) {
		s.writeError(w, http.StatusBadRequest, "invalid_status",
			"status must be one of Processing, QC_Review, Completed")
		return
	}
	if len(req.IDs) == 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "ids must not be empty")
		return
	}
	if len(req.IDs) > 200 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "at most 200 orders per bulk call")
		return
	}

	user := s.currentUser(r)
	now := s.now().UTC()
	results := make([]order.BulkItemResult, 0, len(req.IDs))
	for _, id := range req.IDs {
		if _, err := s.store.TransitionOrder(r.Context(), id, string(req.Status), user.Username, now); err != nil {
			_, code, msg := s.classifyError(err)
			results = append(results, order.BulkItemResult{ID: id, OK: false, Code: code, Message: msg})
			continue
		}
		results = append(results, order.BulkItemResult{ID: id, OK: true})
		s.publishOrder(id, user.Username)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleBulkResolve(w http.ResponseWriter, r *http.Request) {
	var req order.BulkResolveRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if len(req.OrderIDs) == 0 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "orderIds must not be empty")
		return
	}
	if len(req.OrderIDs) > 200 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "at most 200 orders per bulk call")
		return
	}

	user := s.currentUser(r)
	now := s.now().UTC()
	results := make([]order.BulkItemResult, 0, len(req.OrderIDs))
	for _, id := range req.OrderIDs {
		holds, err := s.store.ActiveHolds(r.Context(), id)
		if err != nil {
			results = append(results, order.BulkItemResult{ID: id, OK: false, Code: "internal_error", Message: "internal server error"})
			continue
		}
		if len(holds) == 0 {
			results = append(results, order.BulkItemResult{ID: id, OK: false, Code: "no_active_hold", Message: "no active hold"})
			continue
		}
		failed := false
		for _, h := range holds {
			if _, _, err := s.store.ResolveHold(r.Context(), h.ID, user.Username, now); err != nil {
				_, code, msg := s.classifyError(err)
				results = append(results, order.BulkItemResult{ID: id, OK: false, Code: code, Message: msg})
				failed = true
				break
			}
		}
		if !failed {
			results = append(results, order.BulkItemResult{ID: id, OK: true})
			s.publishOrder(id, user.Username)
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleAdminBackup(w http.ResponseWriter, r *http.Request) {
	tmp, err := os.CreateTemp("", "cockpit-backup-*.db")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "cannot create temp file")
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if _, err := s.store.DB().ExecContext(r.Context(), `VACUUM INTO ?`, tmpPath); err != nil {
		s.logger.Printf("backup: %v", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "backup failed")
		return
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "backup failed")
		return
	}
	defer f.Close()

	name := "cockpit-backup-" + time.Now().UTC().Format("20060102-150405") + ".db"
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, time.Now(), f)
}

func (s *Server) handleListUsersBrief(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	type brief struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
	}
	out := make([]brief, 0, len(users))
	for _, u := range users {
		out = append(out, brief{Username: u.Username, DisplayName: u.DisplayName})
	}
	s.writeJSON(w, http.StatusOK, map[string][]brief{"users": out})
}
