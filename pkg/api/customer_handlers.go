package api

import (
	"net/http"
	"strings"
	"time"

	"cockpit/pkg/db"
)

func (s *Server) handleListCustomersDirectory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	managed, err := s.store.ListCustomers(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	flows, err := s.store.ListCustomerWorkflows(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	flowSet := map[string]db.CustomerFlowMeta{}
	for _, f := range flows {
		flowSet[f.Customer] = f
	}
	onOrders := map[string]bool{}
	names := map[string]bool{}
	for _, c := range managed {
		names[c.Name] = true
	}
	custom, err := s.store.DistinctOrderCustomers(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	for _, n := range custom {
		onOrders[n] = true
		names[n] = true
	}
	type customerOut struct {
		Name       string    `json:"name"`
		Number     string    `json:"number,omitempty"`
		Managed    bool      `json:"managed"`
		Custom     bool      `json:"custom"`
		HasFlow    bool      `json:"hasFlow"`
		FlowBy     string    `json:"flowBy,omitempty"`
		FlowAt     time.Time `json:"flowAt,omitempty"`
		AssignedTo string    `json:"assignedTo,omitempty"`
	}
	out := []customerOut{}
	for name := range names {
		c := customerOut{Name: name, Custom: onOrders[name]}
		for _, m := range managed {
			if m.Name == name {
				c.Managed = true
				c.Number = m.Number
				c.AssignedTo = m.AssignedTo
				break
			}
		}
		if f, ok := flowSet[name]; ok {
			c.HasFlow = true
			c.FlowBy = f.UpdatedBy
			c.FlowAt = f.UpdatedAt
		}
		out = append(out, c)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"customers": out})
}

func (s *Server) handleCreateCustomerManaged(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Number string `json:"number"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Number = strings.TrimSpace(req.Number)
	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "customer name is required")
		return
	}
	if len(req.Name) > 80 {
		s.writeError(w, http.StatusBadRequest, "bad_request", "customer name is too long (max 80)")
		return
	}
	ctx := r.Context()
	managed, err := s.store.ListCustomers(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	byName := map[string]string{}
	byNumber := map[string]string{}
	for _, c := range managed {
		byName[strings.ToLower(c.Name)] = c.Name
		if c.Number != "" {
			byNumber[c.Number] = c.Name
		}
	}
	if existing, ok := byName[strings.ToLower(req.Name)]; ok {
		s.writeError(w, http.StatusConflict, "conflict", "a customer named \""+existing+"\" already exists")
		return
	}
	if req.Number != "" {
		if existing, ok := byNumber[req.Number]; ok {
			s.writeError(w, http.StatusConflict, "conflict", "number "+req.Number+" already belongs to \""+existing+"\"")
			return
		}
	}
	number := req.Number
	if number == "" {
		for i := 1000 + len(managed) + 1; ; i++ {
			number = "CUST-" + itoa(i)
			if _, taken := byNumber[number]; !taken {
				break
			}
		}
	}
	cust, err := s.store.CreateCustomer(ctx, number, req.Name, s.now().UTC())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"customer": cust})
}

func (s *Server) handleListCustomerFlows(w http.ResponseWriter, r *http.Request) {
	flows, err := s.store.ListCustomerWorkflows(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"overrides": flows})
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
