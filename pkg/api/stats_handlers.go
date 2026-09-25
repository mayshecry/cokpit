package api

import (
	"net/http"
	"strconv"
	"time"

	"cockpit/pkg/order"
)

// GET /api/v1/stats/ops?days=14 — aggregates for the ops dashboard.
func (s *Server) handleOpsStats(w http.ResponseWriter, r *http.Request) {
	days := 14
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 60 {
			days = n
		}
	}
	now := s.now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
	sinceMs := startOfDay.UnixMilli()
	ctx := r.Context()

	comp, err := s.store.CompletedPerDay(ctx, sinceMs)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	created, err := s.store.CreatedPerDay(ctx, sinceMs)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	clocked, err := s.store.ClockedPerDay(ctx, sinceMs)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	avgMs, avgOrders, err := s.store.HandleAvgMs(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	board, err := s.store.Leaderboard(ctx, sinceMs)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	// Live SLA mix + open/clocked counters over the current order book.
	orders, err := s.store.ListOrders(ctx, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.annotateOrders(ctx, orders)
	mix := map[string]int{"ON_TIME": 0, "WARNING": 0, "BREACHED": 0}
	open, completedToday := 0, 0
	todayStr := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	for _, o := range orders {
		if o.Status == order.StatusCompleted {
			continue
		}
		open++
		if o.SLA != nil {
			mix[string(o.SLA.Status)]++
		}
	}
	for _, dc := range comp {
		if dc.Day == todayStr {
			completedToday = dc.Count
		}
	}

	names := map[string]string{}
	if users, err := s.store.ListUsers(ctx); err == nil {
		for _, u := range users {
			names[u.Username] = u.DisplayName
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"days":           days,
		"now":            now.UnixMilli(),
		"throughput":     comp,
		"created":        created,
		"clockedPerDay":  clocked,
		"handleAvgMs":    avgMs,
		"handleOrders":   avgOrders,
		"leaderboard":    board,
		"slaMix":         mix,
		"open":           open,
		"completedToday": completedToday,
		"userNames":      names,
	})
}
