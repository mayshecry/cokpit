package api

import (
	"net/http"
	"strconv"

	"cockpit/pkg/auth"
	"cockpit/pkg/swi"
)

type processResponse struct {
	Process  swi.Process   `json:"process"`
	OrderID  int64         `json:"orderId"`
	SyncJobs []swi.SyncJob `json:"syncJobs,omitempty"`
}

func (s *Server) handleGetProcess(w http.ResponseWriter, r *http.Request) {
	orderID, ok := s.orderIDParam(w, r)
	if !ok {
		return
	}
	proc, err := s.store.GetProcess(r.Context(), orderID)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, processResponse{Process: proc, OrderID: orderID})
}

func (s *Server) handleAdvanceProcess(w http.ResponseWriter, r *http.Request) {
	orderID, ok := s.orderIDParam(w, r)
	if !ok {
		return
	}

	var req swi.AdvanceRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if err := swi.ValidateAdvance(req); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	user := s.currentUser(r)
	if req.Force && !s.userHasPerm(r, user, auth.PermProcessManage) {
		s.writeError(w, http.StatusForbidden, "forbidden", "forcing a stage change requires process:manage")
		return
	}

	proc, jobs, err := s.store.AdvanceProcess(r.Context(), orderID, req.Stage, cleanString(req.Note), user.Username, req.Force, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(orderID, user.Username)
	s.writeJSON(w, http.StatusOK, processResponse{Process: proc, OrderID: orderID, SyncJobs: jobs})
}

func (s *Server) handleEscalateProcess(w http.ResponseWriter, r *http.Request) {
	orderID, ok := s.orderIDParam(w, r)
	if !ok {
		return
	}

	var req swi.EscalateRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if err := swi.ValidateEscalation(req); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}

	esc, proc, jobs, err := s.store.EscalateProcess(r.Context(), orderID, req.Level, req.Reason,
		cleanString(req.Note), cleanString(req.EscalatedTo), s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(orderID, s.currentUser(r).Username)
	if esc.EscalatedTo != "" {
		s.publishNotification(esc.EscalatedTo)
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"escalation": esc,
		"process":    proc,
		"syncJobs":   jobs,
	})
}

func (s *Server) handleResolveEscalation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "escalation id must be an integer")
		return
	}

	var req swi.ResolveEscalationRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if cleanString(req.Resolution) == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "resolution is required")
		return
	}

	esc, proc, jobs, err := s.store.ResolveEscalation(r.Context(), id, cleanString(req.Resolution), s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(esc.OrderID, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"escalation": esc,
		"process":    proc,
		"syncJobs":   jobs,
	})
}

func (s *Server) handleListEscalations(w http.ResponseWriter, r *http.Request) {
	openOnly := r.URL.Query().Get("open") != "0"
	limit := queryInt(r, "limit", 200, 1, 1000)

	escalations, err := s.store.ListEscalations(r.Context(), openOnly, limit)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"escalations": escalations,
		"total":       len(escalations),
	})
}

func (s *Server) handleProcessTasks(w http.ResponseWriter, r *http.Request) {
	orderID, ok := s.orderIDParam(w, r)
	if !ok {
		return
	}
	var stage *swi.Stage
	if raw := r.URL.Query().Get("stage"); raw != "" {
		st := swi.Stage(raw)
		if !swi.IsValidStage(st) {
			s.writeError(w, http.StatusBadRequest, "invalid_stage", "unknown process stage "+raw)
			return
		}
		stage = &st
	}

	tasks, err := s.store.ProcessTasks(r.Context(), orderID, stage)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "total": len(tasks)})
}

func (s *Server) handleTickProcessTask(w http.ResponseWriter, r *http.Request) {
	s.setProcessTaskDone(w, r, true)
}

func (s *Server) handleUntickProcessTask(w http.ResponseWriter, r *http.Request) {
	s.setProcessTaskDone(w, r, false)
}

func (s *Server) setProcessTaskDone(w http.ResponseWriter, r *http.Request, done bool) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "task id must be an integer")
		return
	}
	var req swi.TaskRequest
	if r.ContentLength != 0 {
		if err := s.decodeJSON(w, r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
			return
		}
	}

	task, orderID, err := s.store.SetProcessTaskDone(r.Context(), id, done, cleanString(req.Note), s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(orderID, s.currentUser(r).Username)
	s.writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) handleProcessEvents(w http.ResponseWriter, r *http.Request) {
	orderID, ok := s.orderIDParam(w, r)
	if !ok {
		return
	}
	events, err := s.store.ProcessEvents(r.Context(), orderID, queryInt(r, "limit", 200, 1, 1000))
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"events": events, "total": len(events)})
}

func (s *Server) handleProcessBoard(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 500, 1, 5000)
	var stage *swi.Stage
	if raw := r.URL.Query().Get("stage"); raw != "" {
		st := swi.Stage(raw)
		if !swi.IsValidStage(st) {
			s.writeError(w, http.StatusBadRequest, "invalid_stage", "unknown process stage "+raw)
			return
		}
		stage = &st
	}

	now := s.now().UTC()
	procs, err := s.store.ListProcesses(r.Context(), stage, limit)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	metrics, err := s.store.ProcessMetrics(r.Context(), now)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"board":   swi.BuildBoard(now, procs, metrics),
		"stages":  swi.AllStageInfo(),
		"systems": systemInfo(),
	})
}

func (s *Server) handleProcessMetrics(w http.ResponseWriter, r *http.Request) {
	now := s.now().UTC()
	metrics, err := s.store.ProcessMetrics(r.Context(), now)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	overdue, err := s.store.OverdueProcesses(r.Context(), now)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"metrics": metrics,
		"overdue": overdue,
		"stages":  swi.AllStageInfo(),
		"systems": systemInfo(),
	})
}

func (s *Server) handleImportOrder(w http.ResponseWriter, r *http.Request) {
	var req swi.ImportRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if err := swi.ValidateImport(req); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}

	o, created, err := s.store.ImportOrder(r.Context(), req, s.slaHrs, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	proc, err := s.store.GetProcess(r.Context(), o.ID)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	jobs, err := s.store.ListSyncJobs(r.Context(), swi.SyncJobFilter{OrderID: o.ID, Limit: 50})
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.publishOrder(o.ID, s.currentUser(r).Username)

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.writeJSON(w, status, map[string]any{
		"order":    o,
		"process":  proc,
		"syncJobs": jobs,
		"created":  created,
	})
}

func (s *Server) handleAutoEscalate(w http.ResponseWriter, r *http.Request) {
	raised, err := s.store.AutoEscalate(r.Context(), s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	for _, esc := range raised {
		s.publishOrder(esc.OrderID, "system")
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"escalated": raised, "total": len(raised)})
}

func (s *Server) handleListSyncJobs(w http.ResponseWriter, r *http.Request) {
	f := swi.SyncJobFilter{Limit: queryInt(r, "limit", 200, 1, 1000)}
	if raw := r.URL.Query().Get("system"); raw != "" {
		system := swi.System(raw)
		if !swi.IsValidSystem(system) {
			s.writeError(w, http.StatusBadRequest, "invalid_system", "unknown external system "+raw)
			return
		}
		f.System = system
	}
	if raw := r.URL.Query().Get("status"); raw != "" {
		status := swi.SyncStatus(raw)
		if !swi.IsValidSyncStatus(status) {
			s.writeError(w, http.StatusBadRequest, "invalid_status", "unknown sync status "+raw)
			return
		}
		f.Status = status
	}
	if raw := r.URL.Query().Get("orderId"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "bad_request", "orderId must be an integer")
			return
		}
		f.OrderID = id
	}

	jobs, err := s.store.ListSyncJobs(r.Context(), f)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"syncJobs": jobs, "total": len(jobs)})
}

func (s *Server) handleClaimSyncJobs(w http.ResponseWriter, r *http.Request) {
	system := swi.System(r.URL.Query().Get("system"))
	if !swi.IsValidSystem(system) {
		s.writeError(w, http.StatusBadRequest, "invalid_system",
			"system query parameter must be one of AFAS, Omnitracker, Intune, Knox, AppleBusinessManager")
		return
	}
	jobs, err := s.store.ClaimSyncJobs(r.Context(), system, queryInt(r, "limit", 20, 1, 200), s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"syncJobs": jobs, "total": len(jobs)})
}

func (s *Server) handleCompleteSyncJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "sync job id must be an integer")
		return
	}
	var req swi.CompleteSyncRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if err := swi.ValidateComplete(req); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}

	job, err := s.store.CompleteSyncJob(r.Context(), id, req.Status, cleanString(req.Response), cleanString(req.Error), s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"syncJob": job})
}

func (s *Server) handleRetrySyncJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "sync job id must be an integer")
		return
	}
	job, err := s.store.RetrySyncJob(r.Context(), id, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"syncJob": job})
}

func (s *Server) orderIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "order id must be an integer")
		return 0, false
	}
	return id, true
}

func queryInt(r *http.Request, name string, def, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func systemInfo() []map[string]string {
	out := make([]map[string]string, 0, len(swi.AllSystems))
	for _, sys := range swi.AllSystems {
		out = append(out, map[string]string{"code": string(sys), "label": swi.SystemLabel(sys)})
	}
	return out
}
