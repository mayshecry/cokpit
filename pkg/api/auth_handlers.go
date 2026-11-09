package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string    `json:"token"`
	User  auth.User `json:"user"`
}

const unknownUserPasswordHash = "pbkdf2-sha256$600000$92f5615e837dd02623bff92cf6257f78$67c208648614bdb161766ac377be0ba7e0298ca76a6770823a3c9b740810af69"

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}

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
	if errors.Is(err, db.ErrNotFound) {
		_, _ = auth.CheckPassword(unknownUserPasswordHash, req.Password)
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid username or password")
		return
	}
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	ok, err := auth.CheckPassword(user.PasswordHash, req.Password)
	if err != nil || !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid username or password")
		return
	}

	s.loginLimiter.Reset(ip + "|" + uname)

	token, err := auth.NewToken()
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.sessionHours)
	if err := s.store.IssueToken(r.Context(), user.ID, token, now, expiresAt); err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	user.PasswordHash = ""
	user = s.userWithDeptPerms(r.Context(), user)
	s.setSessionCookie(w, r, token, expiresAt)
	s.writeJSON(w, http.StatusOK, loginResponse{Token: token, User: user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteToken(r.Context(), s.requestToken(r)); err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.clearSessionCookie(w, r)
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

	if _, err := r.Cookie(sessionCookieName); err != nil {
		if token := s.bearerToken(r); token != "" {
			s.setSessionCookie(w, r, token, s.now().UTC().Add(s.sessionHours))
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]auth.User{"user": user})
}

func (s *Server) handlePermissionCatalogue(w http.ResponseWriter, r *http.Request) {
	matrix := auth.RolePermissionMatrix()
	roles := make(map[string][]string, len(matrix))
	for role, perms := range matrix {
		roles[string(role)] = auth.PermissionStrings(perms)
	}
	all := auth.PermissionStrings(auth.AllPermissions)
	sort.Strings(all)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"roles":       roles,
		"permissions": all,
	})
}
