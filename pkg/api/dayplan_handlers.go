package api

import (
	"net/http"
	"strings"
	"time"
)

type putGoalRequest struct {
	Day      string `json:"day"`
	Username string `json:"username"`
	Goal     string `json:"goal"`
}

type sendGoalRequest struct {
	Day      string `json:"day"`
	Username string `json:"username"`
}

func cleanDay(v string) string {
	v = strings.TrimSpace(v)
	if len(v) != 10 || v[4] != '-' || v[7] != '-' {
		return ""
	}
	return v
}

// GET /api/v1/dayplan?day=YYYY-MM-DD — goals for one day (any authenticated order viewer).
func (s *Server) handleGetDayplan(w http.ResponseWriter, r *http.Request) {
	day := cleanDay(r.URL.Query().Get("day"))
	if day == "" {
		day = s.now().UTC().Format("2006-01-02")
	}
	goals, err := s.store.DayGoals(r.Context(), day)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"day": day, "goals": goals})
}

// PUT /api/v1/dayplan/goal — QC and above set a person's goal for the day.
func (s *Server) handlePutDayGoal(w http.ResponseWriter, r *http.Request) {
	var req putGoalRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	day := cleanDay(req.Day)
	username := cleanString(req.Username)
	goal := strings.TrimSpace(req.Goal)
	if day == "" || username == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "day and username are required")
		return
	}
	if len(goal) > 300 {
		s.writeError(w, http.StatusBadRequest, "goal_too_long", "goal must be 300 characters or less")
		return
	}
	g, err := s.store.SetDayGoal(r.Context(), day, username, goal, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"goal": g})
}

// POST /api/v1/dayplan/send — push the goal to the person's notification bell.
func (s *Server) handleSendDayGoal(w http.ResponseWriter, r *http.Request) {
	var req sendGoalRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	day := cleanDay(req.Day)
	username := cleanString(req.Username)
	if day == "" || username == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "day and username are required")
		return
	}
	goals, err := s.store.DayGoals(r.Context(), day)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	var found *struct {
		id   int64
		goal string
	}
	for i := range goals {
		if goals[i].Username == username {
			found = &struct {
				id   int64
				goal string
			}{goals[i].ID, goals[i].Goal}
			break
		}
	}
	if found == nil {
		s.writeError(w, http.StatusConflict, "no_goal", "no goal saved for this person today")
		return
	}
	now := s.now().UTC()
	body := "Day goal: " + found.goal
	if strings.TrimSpace(found.goal) == "" {
		body = "Check today's plan on the Workload board."
	}
	if err := s.store.Notify(r.Context(), username, "goal", 0, "", now.UnixMilli(), body, now); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if err := s.store.MarkGoalSent(r.Context(), found.id, now); err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.publishNotification(username)
	s.writeJSON(w, http.StatusOK, map[string]any{"sent": true, "day": day, "username": username})
}

// GET /api/v1/stats/qc?month=YYYY-MM — QC results per order assignee (QC and above).
func (s *Server) handleQCStats(w http.ResponseWriter, r *http.Request) {
	month := cleanDay(r.URL.Query().Get("month") + "-01")
	now := s.now().UTC()
	if month == "" {
		month = now.Format("2006-01-02")
	}
	from, err := time.Parse("2006-01-02", month)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "month must be YYYY-MM")
		return
	}
	to := from.AddDate(0, 1, 0)
	dash, err := s.store.QCDashboard(r.Context(), from.UnixMilli(), to.UnixMilli(), from.AddDate(0, -1, 0).UnixMilli(), from.UnixMilli())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	names := map[string]string{}
	if users, err := s.store.ListUsers(r.Context()); err == nil {
		for _, u := range users {
			names[u.Username] = u.DisplayName
		}
	}
	resp := map[string]any{
		"month":     month[:7],
		"userNames": names,
		"checks":    dash.Checks,
		"pass":      dash.Pass,
		"fail":      dash.Fail,
		"rate":      dash.Rate,
		"prevRate":  dash.PrevRate,
		"perUser":   dash.PerUser,
		"trend":     dash.Trend,
		"reasons":   dash.Reasons,
		"recent":    dash.Recent,
		"history":   dash.History,
	}
	s.writeJSON(w, http.StatusOK, resp)
}
