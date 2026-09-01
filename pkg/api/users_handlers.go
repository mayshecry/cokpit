package api

import (
	"net/http"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
)

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	for i := range users {
		users[i].PasswordHash = ""
	}
	s.writeJSON(w, http.StatusOK, map[string][]auth.User{"users": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	username := cleanString(req.Username)
	if username == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "username is required")
		return
	}
	if len(req.Password) < 8 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "password must be at least 8 characters")
		return
	}
	role := auth.Role(cleanString(req.Role))
	if !auth.IsValidRole(role) {
		s.writeError(w, http.StatusBadRequest, "invalid_role", "role must be one of viewer, operator, qc, admin")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	user, err := s.store.CreateUser(r.Context(), username, hash, cleanString(req.DisplayName), role, s.now().UTC())
	if err != nil {
		if err == db.ErrDuplicateUsername {
			s.writeError(w, http.StatusConflict, "duplicate_username", "username already exists")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	user.PasswordHash = ""
	s.writeJSON(w, http.StatusCreated, map[string]auth.User{"user": user})
}

type setRoleRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleSetUserRole(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "user id must be an integer")
		return
	}
	var req setRoleRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	role := auth.Role(cleanString(req.Role))
	if !auth.IsValidRole(role) {
		s.writeError(w, http.StatusBadRequest, "invalid_role", "role must be one of viewer, operator, qc, admin")
		return
	}
	user, err := s.store.SetUserRole(r.Context(), id, role)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	user.PasswordHash = ""
	s.writeJSON(w, http.StatusOK, map[string]auth.User{"user": user})
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "user id must be an integer")
		return
	}
	var req resetPasswordRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if len(req.Password) < 8 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	user, err := s.store.SetUserPassword(r.Context(), id, hash)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	user.PasswordHash = ""
	s.writeJSON(w, http.StatusOK, map[string]auth.User{"user": user})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "user id must be an integer")
		return
	}
	if id == s.currentUser(r).ID {
		s.writeError(w, http.StatusConflict, "cannot_delete_self", "you cannot delete your own account")
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
