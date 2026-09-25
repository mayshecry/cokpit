package db

import (
	"context"
	"fmt"
	"time"
)

// DayGoal is one person's goal for a calendar day (day = YYYY-MM-DD).
type DayGoal struct {
	ID        int64  `json:"id"`
	Day       string `json:"day"`
	Username  string `json:"username"`
	Goal      string `json:"goal"`
	UpdatedBy string `json:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt"`
	SentAt    *int64 `json:"sentAt,omitempty"`
}

func (s *Store) DayGoals(ctx context.Context, day string) ([]DayGoal, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, day, username, goal, updated_by, updated_at, sent_at
		  FROM day_goals WHERE day = ? ORDER BY username`, day)
	if err != nil {
		return nil, fmt.Errorf("list day goals: %w", err)
	}
	defer rows.Close()
	out := []DayGoal{}
	for rows.Next() {
		var g DayGoal
		if err := rows.Scan(&g.ID, &g.Day, &g.Username, &g.Goal, &g.UpdatedBy, &g.UpdatedAt, &g.SentAt); err != nil {
			return nil, fmt.Errorf("scan day goal: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) SetDayGoal(ctx context.Context, day, username, goal, updatedBy string, now time.Time) (DayGoal, error) {
	var g DayGoal
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO day_goals (day, username, goal, updated_by, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(day, username) DO UPDATE SET
			goal = excluded.goal, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		day, username, goal, updatedBy, millis(now)); err != nil {
		return g, fmt.Errorf("set day goal: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, day, username, goal, updated_by, updated_at, sent_at
		  FROM day_goals WHERE day = ? AND username = ?`, day, username).
		Scan(&g.ID, &g.Day, &g.Username, &g.Goal, &g.UpdatedBy, &g.UpdatedAt, &g.SentAt); err != nil {
		return g, fmt.Errorf("reload day goal: %w", err)
	}
	return g, nil
}

func (s *Store) MarkGoalSent(ctx context.Context, id int64, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE day_goals SET sent_at = ? WHERE id = ?`, millis(now), id); err != nil {
		return fmt.Errorf("mark goal sent: %w", err)
	}
	return nil
}

// QCUserStat aggregates QC results per order assignee for a time window.
type QCUserStat struct {
	Username string `json:"username"`
	Pass     int    `json:"pass"`
	Fail     int    `json:"fail"`
}

func (s *Store) QCStats(ctx context.Context, fromMs, toMs int64) ([]QCUserStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(q.tech, ''), 'unassigned') AS who,
		       SUM(q.status = 'PASS') AS pass,
		       SUM(q.status = 'FAIL') AS fail
		  FROM qc_checks q
		 WHERE q.created_at >= ? AND q.created_at < ?
		 GROUP BY who ORDER BY fail DESC, who`, fromMs, toMs)
	if err != nil {
		return nil, fmt.Errorf("qc stats: %w", err)
	}
	defer rows.Close()
	out := []QCUserStat{}
	for rows.Next() {
		var u QCUserStat
		if err := rows.Scan(&u.Username, &u.Pass, &u.Fail); err != nil {
			return nil, fmt.Errorf("scan qc stats: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// QCDashboard is the rich payload behind the Quality menu: per-tech stats with
// previous-month comparison, a daily trend, fail-reason breakdown and the most
// recent checks.
type QCDashboard struct {
	PerUser  []QCUserStatExt `json:"perUser"`
	Checks   int             `json:"checks"`
	Pass     int             `json:"pass"`
	Fail     int             `json:"fail"`
	Rate     float64         `json:"rate"`
	PrevRate *float64        `json:"prevRate"`
	Trend    []QCTrendPoint  `json:"trend"`
	Reasons  []QCReason      `json:"reasons"`
	Recent   []QCRecentCheck `json:"recent"`
	History  []QCMonthPoint  `json:"history"`
}

// QCMonthPoint is one month of the rolling six-month window behind the
// sparklines (rate is null when nothing was checked that month).
type QCMonthPoint struct {
	Month  string   `json:"month"`
	Checks int      `json:"checks"`
	Rate   *float64 `json:"rate"`
}

type QCUserStatExt struct {
	QCUserStat
	Rate     float64    `json:"rate"`
	PrevRate *float64   `json:"prevRate"`
	Spark    []*float64 `json:"spark"`
}

type QCTrendPoint struct {
	Day  string `json:"day"`
	Pass int    `json:"pass"`
	Fail int    `json:"fail"`
}

type QCReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type QCRecentCheck struct {
	ID          int64  `json:"id"`
	OrderID     int64  `json:"orderId"`
	OrderNumber string `json:"orderNumber"`
	Tech        string `json:"tech"`
	Inspector   string `json:"inspector"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	At          int64  `json:"at"`
}

func rate(pass, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(pass) / float64(total) * 100
}

// QCDashboard aggregates QC checks between fromMs/toMs and compares against
// prevFromMs/prevToMs (the previous month window).
func (s *Store) QCDashboard(ctx context.Context, fromMs, toMs, prevFromMs, prevToMs int64) (*QCDashboard, error) {
	out := &QCDashboard{PerUser: []QCUserStatExt{}, Trend: []QCTrendPoint{}, Reasons: []QCReason{}, Recent: []QCRecentCheck{}}

	cur, err := s.QCStats(ctx, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	prev, err := s.QCStats(ctx, prevFromMs, prevToMs)
	if err != nil {
		return nil, err
	}
	prevRate := map[string]float64{}
	var prevPass, prevTot int
	for _, u := range prev {
		t := u.Pass + u.Fail
		prevRate[u.Username] = rate(u.Pass, t)
		prevPass += u.Pass
		prevTot += t
	}
	if prevTot > 0 {
		r := rate(prevPass, prevTot)
		out.PrevRate = &r
	}
	for _, u := range cur {
		t := u.Pass + u.Fail
		e := QCUserStatExt{QCUserStat: u, Rate: rate(u.Pass, t)}
		if prevTot > 0 {
			if pr, ok := prevRate[u.Username]; ok {
				e.PrevRate = &pr
			}
		}
		out.PerUser = append(out.PerUser, e)
		out.Pass += u.Pass
		out.Fail += u.Fail
	}
	out.Checks = out.Pass + out.Fail
	out.Rate = rate(out.Pass, out.Checks)

	rows, err := s.db.QueryContext(ctx, `
		SELECT strftime('%Y-%m-%d', CAST(q.created_at / 1000 AS INTEGER), 'unixepoch') AS day,
		       SUM(q.status = 'PASS'), SUM(q.status = 'FAIL')
		  FROM qc_checks q
		 WHERE q.created_at >= ? AND q.created_at < ?
		 GROUP BY day ORDER BY day`, fromMs, toMs)
	if err != nil {
		return nil, fmt.Errorf("qc trend: %w", err)
	}
	for rows.Next() {
		var t QCTrendPoint
		if err := rows.Scan(&t.Day, &t.Pass, &t.Fail); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan qc trend: %w", err)
		}
		out.Trend = append(out.Trend, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// rolling six-month window: overall rate per month + per-tech fail rate
	fromT := time.UnixMilli(fromMs).UTC()
	startMs := time.Date(fromT.Year(), fromT.Month()-5, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	months := make([]string, 0, 6)
	for i := -5; i <= 0; i++ {
		months = append(months, time.Date(fromT.Year(), fromT.Month()+time.Month(i), 1, 0, 0, 0, 0, time.UTC).Format("2006-01"))
	}
	out.History = make([]QCMonthPoint, 0, 6)
	for _, m := range months {
		out.History = append(out.History, QCMonthPoint{Month: m})
	}
	monIdx := map[string]int{}
	for i, m := range months {
		monIdx[m] = i
	}
	overall := map[string][2]int{}
	perMon := map[string]map[string][2]int{}
	rows, err = s.db.QueryContext(ctx, `
		SELECT strftime('%Y-%m', CAST(q.created_at / 1000 AS INTEGER), 'unixepoch') AS mon,
		       COALESCE(NULLIF(q.tech, ''), 'unassigned') AS who,
		       SUM(q.status = 'PASS'), SUM(q.status = 'FAIL')
		  FROM qc_checks q
		 WHERE q.created_at >= ? AND q.created_at < ?
		 GROUP BY mon, who`, startMs, toMs)
	if err != nil {
		return nil, fmt.Errorf("qc history: %w", err)
	}
	for rows.Next() {
		var mon, who string
		var pr, fl int
		if err := rows.Scan(&mon, &who, &pr, &fl); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan qc history: %w", err)
		}
		if _, ok := monIdx[mon]; !ok {
			continue
		}
		o := overall[mon]
		o[0] += pr
		o[1] += fl
		overall[mon] = o
		pm := perMon[who]
		if pm == nil {
			pm = map[string][2]int{}
			perMon[who] = pm
		}
		pm[mon] = [2]int{pr, fl}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, m := range months {
		if v, ok := overall[m]; ok && v[0]+v[1] > 0 {
			r := rate(v[0], v[0]+v[1])
			out.History[i].Checks = v[0] + v[1]
			out.History[i].Rate = &r
		}
	}
	for i := range out.PerUser {
		u := &out.PerUser[i]
		pm := perMon[u.Username]
		sp := make([]*float64, len(months))
		for mi, m := range months {
			if v, ok := pm[m]; ok && v[0]+v[1] > 0 {
				fr := rate(v[1], v[0]+v[1])
				sp[mi] = &fr
			}
		}
		u.Spark = sp
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT fail_reason, COUNT(*) AS n FROM qc_checks
		 WHERE status = 'FAIL' AND fail_reason != '' AND created_at >= ? AND created_at < ?
		 GROUP BY fail_reason ORDER BY n DESC, fail_reason LIMIT 6`, fromMs, toMs)
	if err != nil {
		return nil, fmt.Errorf("qc reasons: %w", err)
	}
	for rows.Next() {
		var r QCReason
		if err := rows.Scan(&r.Reason, &r.Count); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan qc reasons: %w", err)
		}
		out.Reasons = append(out.Reasons, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT q.id, q.order_id, o.order_number,
		       COALESCE(NULLIF(q.tech, ''), 'unassigned'), q.inspector_id,
		       q.status, q.fail_reason, q.created_at
		  FROM qc_checks q
		  JOIN orders o ON o.id = q.order_id
		 WHERE q.created_at >= ? AND q.created_at < ?
		 ORDER BY q.created_at DESC, q.id DESC LIMIT 9`, fromMs, toMs)
	if err != nil {
		return nil, fmt.Errorf("qc recent: %w", err)
	}
	for rows.Next() {
		var r QCRecentCheck
		if err := rows.Scan(&r.ID, &r.OrderID, &r.OrderNumber, &r.Tech, &r.Inspector, &r.Status, &r.Reason, &r.At); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan qc recent: %w", err)
		}
		out.Recent = append(out.Recent, r)
	}
	rows.Close()
	return out, rows.Err()
}
