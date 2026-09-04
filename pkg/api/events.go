package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type eventMessage struct {
	Type    string `json:"type"`
	OrderID int64  `json:"orderId,omitempty"`
	By      string `json:"by,omitempty"`
	For     string `json:"for,omitempty"`
}

type sseSub struct {
	ch   chan eventMessage
	user string
}

type eventHub struct {
	mu   sync.Mutex
	subs map[*sseSub]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: map[*sseSub]struct{}{}}
}

func (h *eventHub) subscribe(user string) *sseSub {
	sub := &sseSub{ch: make(chan eventMessage, 16), user: user}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *eventHub) unsubscribe(sub *sseSub) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

func (h *eventHub) publish(msg eventMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if msg.For != "" && sub.user != msg.For {
			continue
		}
		select {
		case sub.ch <- msg:
		default:
		}
	}
}

func (s *Server) publishOrder(orderID int64, by string) {
	if s.hub != nil {
		s.hub.publish(eventMessage{Type: "orders", OrderID: orderID, By: by})
	}
}

func (s *Server) publishChecklist(orderID int64, by string) {
	if s.hub != nil {
		s.hub.publish(eventMessage{Type: "checklist", OrderID: orderID, By: by})
	}
}

func (s *Server) publishManual(orderID int64, by string) {
	if s.hub != nil {
		s.hub.publish(eventMessage{Type: "manual", OrderID: orderID, By: by})
	}
}

func (s *Server) publishComment(orderID int64, by string) {
	if s.hub != nil {
		s.hub.publish(eventMessage{Type: "comment", OrderID: orderID, By: by})
	}
}

func (s *Server) publishNotification(forUser string) {
	if s.hub != nil {
		s.hub.publish(eventMessage{Type: "notification", For: forUser})
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "streaming unsupported")
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	user := s.currentUser(r)
	sub := s.hub.subscribe(user.Username)
	defer s.hub.unsubscribe(sub)

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case msg := <-sub.ch:
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Type, data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) requireAuthSSE(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.bearerToken(r)
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token == "" {
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		user, err := s.store.UserByToken(r.Context(), token, s.now().UTC())
		if err != nil {
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		next(w, r.WithContext(ctx))
	}
}
