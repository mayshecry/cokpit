package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cockpit/pkg/auth"
	"cockpit/pkg/automation"
	"cockpit/pkg/capa"
	"cockpit/pkg/db"
	"cockpit/pkg/order"
	"cockpit/pkg/si"
)

const maxBodyBytes = 1 << 20

type Server struct {
	loginLimiter *RateLimiter
	qr           *qrTicketMgr
	clientGuard  *ClientGuard
	store        *db.Store
	now          func() time.Time
	slaHrs       time.Duration
	sessionHours time.Duration
	logger       *log.Logger
	hub          *eventHub
	automationMu sync.Mutex
	config       ServerConfig
	build        string
}

type ServerConfig struct {
	ClientGuard *ClientGuard

	TrustProxy bool

	PublicURL string
	Now       func() time.Time

	SLATargetDefaultHours int

	SessionHours int

	Logger *log.Logger

	Build string

	OmnitrackerBaseURL string
}

func New(store *db.Store, cfg ServerConfig) *Server {
	trustProxy = cfg.TrustProxy
	publicBaseURL = strings.TrimRight(cfg.PublicURL, "/")
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SLATargetDefaultHours <= 0 {
		cfg.SLATargetDefaultHours = 24
	}
	if cfg.SessionHours <= 0 {
		cfg.SessionHours = 24
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	return &Server{
		store:        store,
		now:          cfg.Now,
		slaHrs:       time.Duration(cfg.SLATargetDefaultHours) * time.Hour,
		sessionHours: time.Duration(cfg.SessionHours) * time.Hour,
		loginLimiter: NewRateLimiter(),
		qr:           newQrTicketMgr(),
		clientGuard:  cfg.ClientGuard,
		logger:       cfg.Logger,
		hub:          newEventHub(),
		config:       cfg,
		build:        cfg.Build,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.handleLogout))

	mux.HandleFunc("POST /api/v1/pair", s.handleClientPair)

	mux.HandleFunc("POST /api/v1/launch", s.handleClientLaunch)

	mux.HandleFunc("GET /api/v1/qr-login", s.handleQRLoginCreate)

	mux.HandleFunc("GET /api/v1/qr-login/status", s.handleQRLoginStatus)

	mux.HandleFunc("POST /api/v1/qr-login/approve", s.requireAuth(s.handleQRLoginApprove))

	mux.HandleFunc("POST /api/v1/prep-logs", s.handlePostPrepLog)

	mux.HandleFunc("GET /api/v1/admin/prep-devices", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleListPrepDevices)))

	mux.HandleFunc("GET /api/v1/admin/prep-devices/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleGetPrepDevice)))

	mux.HandleFunc("DELETE /api/v1/admin/prep-devices/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleDeletePrepDevice)))

	mux.HandleFunc("GET /api/v1/admin/pair-qr", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleAdminPairQR)))

	mux.HandleFunc("POST /api/v1/admin/pair-code", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleAdminPairCode)))

	mux.HandleFunc("GET /api/v1/admin/client-keys", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleListPairedClients)))

	mux.HandleFunc("POST /api/v1/admin/client-keys/{id}/revoke", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleRevokePairedClient)))

	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))

	mux.HandleFunc("GET /api/v1/auth/permissions", s.requireAuth(s.handlePermissionCatalogue))

	mux.HandleFunc("GET /api/v1/users", s.requireAuth(s.requirePerm(auth.PermUsersList, s.handleListUsers)))

	mux.HandleFunc("POST /api/v1/users", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleCreateUser)))

	mux.HandleFunc("POST /api/v1/users/{id}/role", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleSetUserRole)))

	mux.HandleFunc("POST /api/v1/users/{id}/password", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleResetPassword)))

	mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleDeleteUser)))

	mux.HandleFunc("GET /api/v1/orders", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListOrders)))

	mux.HandleFunc("POST /api/v1/orders", s.requireAuth(s.requirePerm(auth.PermOrderCreate, s.handleCreateOrder)))

	mux.HandleFunc("GET /api/v1/orders/{id}", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetOrder)))

	mux.HandleFunc("POST /api/v1/orders/{id}/transition", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleTransition)))

	mux.HandleFunc("POST /api/v1/orders/{id}/assign", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleAssign)))

	mux.HandleFunc("POST /api/v1/qcbench/step-result", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleRecordWorkflowStepResult)))

	mux.HandleFunc("GET /api/v1/qcbench/steps/{id}", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleGetWorkflowStepResults)))

	mux.HandleFunc("POST /api/v1/qcbench/rerun/{id}", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleRerunWorkflow)))

	mux.HandleFunc("POST /api/v1/orders/{id}/holds", s.requireAuth(s.requirePerm(auth.PermHoldCreate, s.handlePlaceHold)))

	mux.HandleFunc("POST /api/v1/holds/{id}/resolve", s.requireAuth(s.requirePerm(auth.PermHoldResolve, s.handleResolveHold)))

	mux.HandleFunc("POST /api/v1/orders/{id}/qc", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleSubmitQC)))

	mux.HandleFunc("GET /api/v1/orders/{id}/audit", s.requireAuth(s.requirePerm(auth.PermAuditView, s.handleAudit)))

	mux.HandleFunc("POST /api/v1/orders/bulk-transition", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleBulkTransition)))

	mux.HandleFunc("POST /api/v1/holds/bulk-resolve", s.requireAuth(s.requirePerm(auth.PermHoldResolve, s.handleBulkResolve)))

	mux.HandleFunc("GET /api/v1/orders/{id}/checklist", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetChecklist)))

	mux.HandleFunc("GET /api/v1/orders/{id}/worked-by", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetOrderWorkedBy)))

	mux.HandleFunc("POST /api/v1/orders/{id}/share", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleMarkOrderShared)))

	mux.HandleFunc("POST /api/v1/orders/{id}/share-with", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleShareOrderWithUser)))

	mux.HandleFunc("GET /api/v1/orders/{id}/shared-info", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetOrderSharedInfo)))

	mux.HandleFunc("GET /api/v1/stats/qc", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleQCStats)))

	mux.HandleFunc("GET /api/v1/qc/queue", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleQCQueue)))

	mux.HandleFunc("GET /api/v1/qc/reasons", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleQCReasons)))

	mux.HandleFunc("GET /api/v1/qc/context/{id}", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleQCContext)))

	mux.HandleFunc("GET /api/v1/capa", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListCAPA)))

	mux.HandleFunc("POST /api/v1/capa", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleCreateCAPA)))

	mux.HandleFunc("GET /api/v1/capa/{id}", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleGetCAPA)))

	mux.HandleFunc("POST /api/v1/capa/{id}", s.requireAuth(s.handleUpdateCAPA))

	mux.HandleFunc("POST /api/v1/capa/{id}/verify", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleVerifyCAPA)))

	mux.HandleFunc("GET /api/v1/automations", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleListAutomationRules)))

	mux.HandleFunc("POST /api/v1/automations", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleCreateAutomationRule)))

	mux.HandleFunc("POST /api/v1/automations/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleUpdateAutomationRule)))

	mux.HandleFunc("DELETE /api/v1/automations/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleDeleteAutomationRule)))

	mux.HandleFunc("GET /api/v1/automation-runs", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleListAutomationRuns)))

	mux.HandleFunc("POST /api/v1/automations/evaluate", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleEvaluateAutomations)))

	mux.HandleFunc("POST /api/v1/automations/preview", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handlePreviewAutomationRule)))

	mux.HandleFunc("GET /api/v1/dayplan", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleGetDayplan)))

	mux.HandleFunc("PUT /api/v1/dayplan/goal", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handlePutDayGoal)))

	mux.HandleFunc("POST /api/v1/dayplan/send", s.requireAuth(s.requirePerm(auth.PermQCSubmit, s.handleSendDayGoal)))

	mux.HandleFunc("GET /api/v1/stats/ops", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleOpsStats)))

	mux.HandleFunc("GET /api/v1/workflow", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleGetWorkflow)))

	mux.HandleFunc("PUT /api/v1/workflow", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handlePutWorkflow)))

	mux.HandleFunc("GET /api/v1/workflow-profiles", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListWorkflowProfiles)))

	mux.HandleFunc("GET /api/v1/workflow-profiles/{id}", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleGetWorkflowProfile)))

	mux.HandleFunc("POST /api/v1/workflow-profiles", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleCreateWorkflowProfile)))

	mux.HandleFunc("PUT /api/v1/workflow-profiles/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleUpdateWorkflowProfile)))

	mux.HandleFunc("GET /api/v1/work/sessions", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleListAllWork)))

	mux.HandleFunc("GET /api/v1/work/mine", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleListMyWork)))

	mux.HandleFunc("GET /api/v1/orders/{id}/work", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetWork)))

	mux.HandleFunc("POST /api/v1/orders/{id}/work/start", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleStartWork)))

	mux.HandleFunc("POST /api/v1/orders/{id}/work/update", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleUpdateWork)))

	mux.HandleFunc("POST /api/v1/orders/{id}/work/end", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleEndWork)))

	mux.HandleFunc("POST /api/v1/orders/{id}/checklist", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleSetChecklist)))

	mux.HandleFunc("POST /api/v1/checklist/{id}/tick", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleTickChecklistItem)))

	mux.HandleFunc("POST /api/v1/checklist/{id}/untick", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleUntickChecklistItem)))

	mux.HandleFunc("GET /api/v1/checklists", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleListChecklists)))

	mux.HandleFunc("POST /api/v1/checklists/demo", s.requireAuth(s.requirePerm(auth.PermOrderCreate, s.handleSeedDemoChecklists)))

	mux.HandleFunc("GET /api/v1/products", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListProducts)))

	mux.HandleFunc("POST /api/v1/products", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleCreateProduct)))

	mux.HandleFunc("GET /api/v1/products/{id}", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetProduct)))

	mux.HandleFunc("POST /api/v1/products/{id}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleUpdateProduct)))

	mux.HandleFunc("DELETE /api/v1/products/{id}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleDeleteProduct)))

	mux.HandleFunc("POST /api/v1/products/quick", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleQuickManual)))

	mux.HandleFunc("POST /api/v1/products/{id}/approve", s.requireAuth(s.requirePerm(auth.PermManualsApprove, s.handleApproveProduct)))

	mux.HandleFunc("POST /api/v1/products/{id}/department", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleSetProductDepartment)))

	mux.HandleFunc("POST /api/v1/si/customers/{id}/coordinator", s.requireAuth(s.requirePerm(auth.PermCustomersManage, s.handleSetCustomerCoordinator)))

	mux.HandleFunc("GET /api/v1/customers", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListCustomersDirectory)))

	mux.HandleFunc("POST /api/v1/customers", s.requireAuth(s.requirePerm(auth.PermCustomersManage, s.handleCreateCustomerManaged)))

	mux.HandleFunc("GET /api/v1/customers/flows", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListCustomerFlows)))

	mux.HandleFunc("POST /api/v1/products/{id}/blocks", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleAddManualBlock)))

	mux.HandleFunc("POST /api/v1/products/{pid}/blocks/{bid}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleUpdateManualBlock)))

	mux.HandleFunc("POST /api/v1/products/{pid}/blocks/{bid}/move", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleMoveManualBlock)))

	mux.HandleFunc("DELETE /api/v1/products/{pid}/blocks/{bid}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleDeleteManualBlock)))

	mux.HandleFunc("POST /api/v1/orders/{id}/manuals", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleInstantiateManual)))

	mux.HandleFunc("GET /api/v1/orders/{id}/manuals", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleListOrderManuals)))

	mux.HandleFunc("POST /api/v1/manuals/{mid}/blocks/{bid}/answer", s.requireAuth(s.requirePerm(auth.PermPickUse, s.handleAnswerManualBlock)))

	mux.HandleFunc("POST /api/v1/manuals/{mid}/blocks/{bid}/flag", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleFlagManualBlock)))

	mux.HandleFunc("DELETE /api/v1/manuals/{mid}", s.requireAuth(s.requirePerm(auth.PermManualManage, s.handleDeleteManual)))

	mux.HandleFunc("GET /api/v1/manuals", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleListManuals)))

	mux.HandleFunc("GET /api/v1/attention", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleAttention)))

	mux.HandleFunc("POST /api/v1/scan", s.requireAuth(s.requirePerm(auth.PermScanUse, s.handleScan)))

	mux.HandleFunc("GET /api/v1/orders/{id}/scans", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListScans)))

	mux.HandleFunc("GET /api/v1/orders/{id}/comments", s.requireAuth(s.requirePerm(auth.PermCommentRead, s.handleListComments)))

	mux.HandleFunc("POST /api/v1/orders/{id}/comments", s.requireAuth(s.requirePerm(auth.PermCommentPost, s.handleAddComment)))

	mux.HandleFunc("GET /api/v1/orders/{id}/barcode", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleGetBarcode)))

	mux.HandleFunc("POST /api/v1/orders/{id}/barcode", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleGenerateBarcode)))

	mux.HandleFunc("POST /api/v1/orders/{id}/omnitracker", s.requireAuth(s.requirePerm(auth.PermOrderTransition, s.handleUpdateOmnitrackerInfo)))

	mux.HandleFunc("GET /api/v1/orders/{id}/barcode/history", s.requireAuth(s.requirePerm(auth.PermOrderView, s.handleBarcodeScanHistory)))

	mux.HandleFunc("GET /api/v1/scan/barcode", s.requireAuth(s.requirePerm(auth.PermScanUse, s.handleScanBarcode)))

	mux.HandleFunc("POST /api/v1/process/orders/import", s.requireAuth(s.requirePerm(auth.PermProcessAdvance, s.handleImportOrder)))

	mux.HandleFunc("GET /api/v1/process/board", s.requireAuth(s.requirePerm(auth.PermProcessView, s.handleProcessBoard)))

	mux.HandleFunc("GET /api/v1/process/metrics", s.requireAuth(s.requirePerm(auth.PermProcessView, s.handleProcessMetrics)))

	mux.HandleFunc("GET /api/v1/process/escalations", s.requireAuth(s.requirePerm(auth.PermProcessView, s.handleListEscalations)))

	mux.HandleFunc("POST /api/v1/process/escalations/auto", s.requireAuth(s.requirePerm(auth.PermProcessManage, s.handleAutoEscalate)))

	mux.HandleFunc("POST /api/v1/process/escalations/{id}/resolve", s.requireAuth(s.requirePerm(auth.PermProcessEscalate, s.handleResolveEscalation)))

	mux.HandleFunc("GET /api/v1/process/sync-jobs", s.requireAuth(s.requirePerm(auth.PermProcessView, s.handleListSyncJobs)))

	mux.HandleFunc("POST /api/v1/process/sync-jobs/claim", s.requireAuth(s.requirePerm(auth.PermIntegrationsManage, s.handleClaimSyncJobs)))

	mux.HandleFunc("POST /api/v1/process/sync-jobs/{id}/complete", s.requireAuth(s.requirePerm(auth.PermIntegrationsManage, s.handleCompleteSyncJob)))

	mux.HandleFunc("POST /api/v1/process/sync-jobs/{id}/retry", s.requireAuth(s.requirePerm(auth.PermIntegrationsManage, s.handleRetrySyncJob)))

	mux.HandleFunc("GET /api/v1/orders/{id}/process", s.requireAuth(s.requirePerm(auth.PermProcessView, s.handleGetProcess)))

	mux.HandleFunc("POST /api/v1/orders/{id}/process/advance", s.requireAuth(s.requirePerm(auth.PermProcessAdvance, s.handleAdvanceProcess)))

	mux.HandleFunc("POST /api/v1/orders/{id}/process/escalate", s.requireAuth(s.requirePerm(auth.PermProcessEscalate, s.handleEscalateProcess)))

	mux.HandleFunc("GET /api/v1/orders/{id}/process/tasks", s.requireAuth(s.requirePerm(auth.PermProcessView, s.handleProcessTasks)))

	mux.HandleFunc("POST /api/v1/process/tasks/{id}/tick", s.requireAuth(s.requirePerm(auth.PermProcessAdvance, s.handleTickProcessTask)))

	mux.HandleFunc("POST /api/v1/process/tasks/{id}/untick", s.requireAuth(s.requirePerm(auth.PermProcessAdvance, s.handleUntickProcessTask)))

	mux.HandleFunc("GET /api/v1/orders/{id}/process/events", s.requireAuth(s.requirePerm(auth.PermProcessView, s.handleProcessEvents)))

	mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.handleListNotifications))

	mux.HandleFunc("POST /api/v1/notifications/read", s.requireAuth(s.handleMarkNotificationsRead))

	mux.HandleFunc("GET /api/v1/users/brief", s.requireAuth(s.requirePerm(auth.PermOrderList, s.handleListUsersBrief)))

	mux.HandleFunc("GET /api/v1/si/customers", s.requireAuth(s.requirePerm(auth.PermSIList, s.handleListCustomers)))

	mux.HandleFunc("POST /api/v1/si/customers", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleCreateCustomer)))

	mux.HandleFunc("GET /api/v1/si/customers/{number}/projects", s.requireAuth(s.requirePerm(auth.PermSIList, s.handleListProjects)))

	mux.HandleFunc("POST /api/v1/si/projects", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleCreateProject)))

	mux.HandleFunc("GET /api/v1/si/sis", s.requireAuth(s.requirePerm(auth.PermSIList, s.handleListSIs)))

	mux.HandleFunc("POST /api/v1/si/sis", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleCreateSI)))

	mux.HandleFunc("GET /api/v1/si/sis/{id}", s.requireAuth(s.requirePerm(auth.PermSIView, s.handleGetSI)))

	mux.HandleFunc("POST /api/v1/si/sis/{id}", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleUpdateSI)))

	mux.HandleFunc("POST /api/v1/si/sis/{id}/transition", s.requireAuth(s.requirePerm(auth.PermSITransition, s.handleTransitionSI)))

	mux.HandleFunc("POST /api/v1/si/sis/{id}/projects", s.requireAuth(s.requirePerm(auth.PermSIProjects, s.handleSetSIProjects)))

	mux.HandleFunc("GET /api/v1/si/sis/{id}/events", s.requireAuth(s.requirePerm(auth.PermSIView, s.handleSIEvents)))

	mux.HandleFunc("POST /api/v1/si/seed", s.requireAuth(s.requirePerm(auth.PermSICreate, s.handleSeedDemoSI)))

	mux.HandleFunc("GET /api/v1/si/projects/{projectId}/checklist", s.requireAuth(s.requirePerm(auth.PermSIView, s.handleListChecklist)))

	mux.HandleFunc("POST /api/v1/si/projects/{projectId}/checklist", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleAddChecklistItem)))

	mux.HandleFunc("POST /api/v1/si/projects/checklist/{itemId}/tick", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleSITickChecklistItem)))

	mux.HandleFunc("POST /api/v1/si/projects/checklist/{itemId}/untick", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleSIUntickChecklistItem)))

	mux.HandleFunc("DELETE /api/v1/si/projects/checklist/{itemId}", s.requireAuth(s.requirePerm(auth.PermSIUpdate, s.handleDeleteChecklistItem)))

	mux.HandleFunc("GET /api/v1/departments", s.requireAuth(s.requirePerm(auth.PermUsersList, s.handleListDepartments)))

	mux.HandleFunc("POST /api/v1/departments", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleCreateDepartment)))

	mux.HandleFunc("DELETE /api/v1/departments/{id}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleDeleteDepartment)))

	mux.HandleFunc("POST /api/v1/departments/{id}/users", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleAddUserToDepartment)))

	mux.HandleFunc("DELETE /api/v1/departments/{id}/users/{userId}", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleRemoveUserFromDepartment)))

	mux.HandleFunc("GET /api/v1/departments/{id}/users", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleListDepartmentUsers)))

	mux.HandleFunc("POST /api/v1/departments/{id}/permissions", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleSetDepartmentPermissions)))

	mux.HandleFunc("GET /api/v1/users/{id}/departments", s.requireAuth(s.requirePerm(auth.PermUsersList, s.handleListUserDepartments)))

	mux.HandleFunc("GET /api/v1/admin/backup", s.requireAuth(s.requirePerm(auth.PermUsersManage, s.handleAdminBackup)))

	mux.HandleFunc("GET /api/v1/events", s.requireAuthSSE(s.requirePerm(auth.PermOrderList, s.handleEvents)))

	mux.HandleFunc("/api/", s.handleAPINotFound)

	static := http.FileServer(http.Dir("frontend"))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		static.ServeHTTP(w, r)
	}))

	var top http.Handler = originGuard(mux)
	if s.clientGuard != nil && s.clientGuard.Enabled() {
		top = s.clientGuard.Wrap(top)
	}
	return s.recoverMiddleware(s.logMiddleware(securityHeaders(top)))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			s.logger.Printf("json encode: %v", err)
		}
	}
}

type apiError struct {
	Error apiErrorBody `json:"error"`
}

type apiErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Extra   map[string]any `json:"extra,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, apiError{Error: apiErrorBody{Code: code, Message: message}})
}

func (s *Server) isCanceled(r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if r != nil && r.Context().Err() == context.Canceled {
		return true
	}
	return errors.Is(err, context.Canceled)
}

func (s *Server) writeCanceled(w http.ResponseWriter, r *http.Request) {
	if r != nil && r.Context().Err() == context.Canceled {
		s.logf("client canceled: %s %s", r.Method, r.URL.Path)
		return
	}
	s.writeError(w, http.StatusRequestTimeout, "request_canceled", "the request was canceled before it finished")
}

func (s *Server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

func (s *Server) writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	if s.isCanceled(r, err) {
		s.writeCanceled(w, r)
		return
	}
	if r != nil {
		s.logf("internal error: %s %s: %v", r.Method, r.URL.Path, err)
	} else {
		s.logf("internal error: %v", err)
	}
	s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("request body contains trailing data")
	}
	return nil
}

func parseID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func (s *Server) classifyError(err error) (int, string, string) {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, db.ErrDuplicateOrderNumber):
		return http.StatusConflict, "duplicate_order_number", err.Error()
	case errors.Is(err, db.ErrDuplicateUsername):
		return http.StatusConflict, "duplicate_username", err.Error()
	case errors.Is(err, db.ErrLastAdmin):
		return http.StatusConflict, "last_admin", err.Error()
	case errors.Is(err, db.ErrDuplicateProductCode):
		return http.StatusConflict, "duplicate_product_code", err.Error()
	case errors.Is(err, db.ErrDuplicateManual):
		return http.StatusConflict, "duplicate_manual", err.Error()
	case errors.Is(err, db.ErrDuplicateCustomerNumber):
		return http.StatusConflict, "duplicate_customer_number", err.Error()
	case errors.Is(err, db.ErrDuplicateProjectCode):
		return http.StatusConflict, "duplicate_project_code", err.Error()
	case errors.Is(err, db.ErrDuplicateSICode):
		return http.StatusConflict, "duplicate_si_code", err.Error()
	case errors.Is(err, db.ErrDuplicateDepartmentName):
		return http.StatusConflict, "duplicate_department_name", err.Error()
	case errors.Is(err, db.ErrDuplicateAutomationName):
		return http.StatusConflict, "duplicate_automation_name", err.Error()
	case errors.Is(err, db.ErrScanIdempotencyConflict):
		return http.StatusConflict, "scan_idempotency_conflict", err.Error()
	case errors.Is(err, automation.ErrValidation):
		return http.StatusBadRequest, "invalid_automation_rule", err.Error()
	case errors.Is(err, capa.ErrValidation):
		return http.StatusBadRequest, "invalid_capa", err.Error()
	case errors.Is(err, capa.ErrInvalidTransition):
		return http.StatusConflict, "invalid_capa_transition", err.Error()
	case errors.Is(err, db.ErrUnknownAssignee):
		return http.StatusBadRequest, "unknown_assignee", err.Error()
	case errors.Is(err, db.ErrUnknownDepartment):
		return http.StatusBadRequest, "unknown_department", err.Error()
	case errors.Is(err, db.ErrBadAnswer):
		return http.StatusBadRequest, "invalid_answer", err.Error()
	case errors.Is(err, db.ErrBadFlagReason):
		return http.StatusBadRequest, "flag_reason_required", err.Error()
	case errors.Is(err, db.ErrFlagUnanswered):
		return http.StatusConflict, "flag_unanswered", err.Error()
	case errors.Is(err, db.ErrManualEdge):
		return http.StatusConflict, "manual_edge", err.Error()
	case errors.Is(err, db.ErrHoldActive):
		return http.StatusConflict, "hold_active", err.Error()
	case errors.Is(err, db.ErrHoldResolved):
		return http.StatusConflict, "hold_already_resolved", err.Error()
	case errors.Is(err, db.ErrNoQCPass):
		return http.StatusConflict, "qc_not_passed", err.Error()
	case errors.Is(err, order.ErrInvalidStatus):
		return http.StatusBadRequest, "invalid_status", err.Error()
	case errors.Is(err, order.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition", err.Error()
	case errors.Is(err, si.ErrInvalidStatus):
		return http.StatusBadRequest, "invalid_status", err.Error()
	case errors.Is(err, si.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition", err.Error()
	case errors.Is(err, si.ErrValidation):
		return http.StatusBadRequest, "validation_error", err.Error()
	case errors.Is(err, db.ErrStaleWrite):
		return http.StatusConflict, "stale_write", err.Error()
	case errors.Is(err, order.ErrActiveHold):
		return http.StatusConflict, "active_hold", err.Error()
	case errors.Is(err, order.ErrCompletedTransition):
		return http.StatusConflict, "order_completed", err.Error()
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error"
	}
}

func validCommandStatus(status order.Status) bool {
	switch status {
	case order.StatusReceived, order.StatusProcessing, order.StatusQCReview, order.StatusCompleted:
		return true
	default:
		return false
	}
}

func cleanString(v string) string { return strings.TrimSpace(v) }
