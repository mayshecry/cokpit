package api

import (
	"net/http"

	"cockpit/pkg/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string    `json:"token"`
	User  auth.User `json:"user"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	user, err := s.store.UserByUsername(r.Context(), cleanString(req.Username))
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid username or password")
		return
	}
	ok, err := auth.CheckPassword(user.PasswordHash, req.Password)
	if err != nil || !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid username or password")
		return
	}

	token, err := auth.NewToken()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	now := s.now().UTC()
	if err := s.store.IssueToken(r.Context(), user.ID, token, now, now.Add(s.sessionHours)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	user.PasswordHash = ""
	s.writeJSON(w, http.StatusOK, loginResponse{Token: token, User: user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteToken(r.Context(), s.bearerToken(r)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	user.PasswordHash = ""
	s.writeJSON(w, http.StatusOK, map[string]auth.User{"user": user})
}
