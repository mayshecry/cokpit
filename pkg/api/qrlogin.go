package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"sync"
	"time"

	"cockpit/pkg/auth"

	"github.com/skip2/go-qrcode"
)

const qrTicketTTL = 150 * time.Second

type qrTicket struct {
	status       string
	token        string
	tokenExpires time.Time
	user         auth.User
	expiresAt    time.Time
}

type qrTicketMgr struct {
	mu sync.Mutex
	m  map[string]*qrTicket
}

func newQrTicketMgr() *qrTicketMgr { return &qrTicketMgr{m: map[string]*qrTicket{}} }

func (q *qrTicketMgr) sweepLocked() {
	now := time.Now()
	for id, t := range q.m {
		if now.After(t.expiresAt) {
			delete(q.m, id)
		}
	}
}

func (s *Server) handleQRLoginCreate(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ok, retry := s.loginLimiter.Allow(ip+"|qr", 30, time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "too many QR requests — wait a moment")
		return
	}
	id := randHex(16)
	t := &qrTicket{status: "pending", expiresAt: time.Now().Add(qrTicketTTL)}
	s.qr.mu.Lock()
	s.qr.sweepLocked()
	s.qr.m[id] = t
	s.qr.mu.Unlock()

	scheme, host := effectiveURL(r)
	url := scheme + "://" + host + "/qr-approve.html?t=" + id

	png, err := qrcode.Encode(url, qrcode.Highest, 320)
	if err != nil {
		s.qr.mu.Lock()
		delete(s.qr.m, id)
		s.qr.mu.Unlock()
		s.writeError(w, http.StatusInternalServerError, "internal_error", "QR generation failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"ticket":  id,
		"expires": qrTicketTTL.Milliseconds() / 1000,
		"url":     url,
		"dataUrl": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	})
}

func (s *Server) handleQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	id := cleanString(r.URL.Query().Get("ticket"))
	s.qr.mu.Lock()
	defer s.qr.mu.Unlock()
	s.qr.sweepLocked()
	t, ok := s.qr.m[id]
	if !ok {
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "expired"})
		return
	}
	if t.expiresAt.Before(time.Now()) {
		delete(s.qr.m, id)
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "expired"})
		return
	}
	if t.status == "approved" {

		token, user, expiresAt := t.token, t.user, t.tokenExpires
		delete(s.qr.m, id)
		s.setSessionCookie(w, r, token, expiresAt)
		s.writeJSON(w, http.StatusOK, map[string]any{"status": "approved", "token": token, "user": user})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": t.status})
}

func (s *Server) handleQRLoginApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ticket string `json:"ticket"`
		Action string `json:"action"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	s.qr.mu.Lock()
	defer s.qr.mu.Unlock()
	t, ok := s.qr.m[req.Ticket]
	if !ok || t.expiresAt.Before(time.Now()) || t.status != "pending" {
		s.writeError(w, http.StatusGone, "ticket_expired", "this QR code has expired — refresh the login screen")
		return
	}
	if req.Action == "reject" {
		t.status = "rejected"
		s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	user := s.currentUser(r)
	token, err := auth.NewToken()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.sessionHours)
	if err := s.store.IssueToken(r.Context(), user.ID, token, now, expiresAt); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	user.PasswordHash = ""
	user = s.userWithDeptPerms(r.Context(), user)
	t.status, t.token, t.tokenExpires, t.user = "approved", token, expiresAt, user
	s.writeJSON(w, http.StatusOK, map[string]string{"user": user.Username, "ok": "1"})
}
