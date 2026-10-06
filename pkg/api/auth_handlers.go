package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	// Brute-force throttle before any credential work (reads are cheap,
	// PBKDF2 is not): strict per IP+username (5 attempts / 5 min), plus a
	// broader per-IP ceiling (20 / minute). 429 + Retry-After when hit.
	ip := clientIP(r)
	uname := strings.ToLower(cleanString(req.Username))
	if ok, retry := s.loginLimiter.Allow(ip+"|login", 20, time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts — try again in "+strconv.Itoa(retry)+"s")
		return
	}
	if ok, retry := s.loginLimiter.Allow(ip+"|"+uname, 5, 5*time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		mins := retry / 60
		if mins < 1 {
			mins = 1
		}
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed logins for this account — try again in "+strconv.Itoa(mins)+"m "+strconv.Itoa(retry%60)+"s")
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

	// Successful auth: clear this account+IP window again so a typo streak
	// earlier in the day never carries over.
	s.loginLimiter.Reset(ip + "|" + uname)

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
	user = s.userWithDeptPerms(r.Context(), user)
	s.writeJSON(w, http.StatusOK, loginResponse{Token: token, User: user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteToken(r.Context(), s.bearerToken(r)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) userWithDeptPerms(ctx context.Context, user auth.User) auth.User {
	if user.Role == auth.RoleAdmin {
		return user
	}
	if perms, err := s.store.UserDepartmentPermissions(ctx, user.ID); err == nil {
		user.Permissions = perms
	}
	return user
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	user.PasswordHash = ""
	user = s.userWithDeptPerms(r.Context(), user)
	s.writeJSON(w, http.StatusOK, map[string]auth.User{"user": user})
}
