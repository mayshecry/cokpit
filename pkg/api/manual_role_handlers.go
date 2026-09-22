package api

import (
	"net/http"
	"strings"

	"cockpit/pkg/auth"
	"cockpit/pkg/order"
)

// productDeptAllowed reports whether the current user may use a product that
// belongs to a department. Empty department = usable by everyone; admins bypass.
func (s *Server) productDeptAllowed(r *http.Request, prod order.Product) bool {
	if prod.Department == "" {
		return true
	}
	me := s.currentUser(r)
	if me.Role == auth.RoleAdmin {
		return true
	}
	depts, err := s.store.UserDepartments(r.Context(), me.ID)
	if err != nil {
		return false
	}
	for _, d := range depts {
		if d.Name == prod.Department {
			return true
		}
	}
	return false
}

// autoApprove applies the approval rule: manuals authored by SC or admin are
// approved at birth; everything else (e.g. QC-authored) waits for SC approval.
func (s *Server) autoApprove(r *http.Request, prod order.Product) order.Product {
	me := s.currentUser(r)
	if me.Role == auth.RoleSC || me.Role == auth.RoleAdmin {
		if approved, err := s.store.ApproveProduct(r.Context(), prod.ID, me.Username, s.now().UTC()); err == nil {
			return approved
		}
	}
	return prod
}

func (s *Server) handleQuickManual(w http.ResponseWriter, r *http.Request) {
	var req order.QuickManualRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	code, name := cleanString(req.Code), cleanString(req.Name)
	if code == "" || name == "" {
		s.writeError(w, http.StatusBadRequest, "bad_request", "code and name are required")
		return
	}
	steps := []string{}
	for _, raw := range req.Steps {
		line := strings.TrimSpace(raw)
		if line != "" {
			steps = append(steps, line)
		}
	}
	now := s.now().UTC()
	me := s.currentUser(r)
	prod, err := s.store.CreateProduct(r.Context(), code, name, "", cleanString(req.Department), me.Username, now)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	for i, step := range steps {
		if _, err := s.store.AddManualBlock(r.Context(), prod.ID, step, "", "", now); err != nil {
			s.writeError(w, http.StatusInternalServerError, "internal_error", "block "+stepTitle(i)+" could not be added")
			return
		}
	}
	prod = s.autoApprove(r, prod)
	s.writeJSON(w, http.StatusCreated, map[string]order.Product{"product": prod})
}

func stepTitle(i int) string {
	return string(rune('1' + i))
}

func (s *Server) handleApproveProduct(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	me := s.currentUser(r)
	prod, err := s.store.ApproveProduct(r.Context(), id, me.Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]order.Product{"product": prod})
}

func (s *Server) handleSetProductDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	var req order.ProductDepartmentRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	prod, err := s.store.SetProductDepartment(r.Context(), id, cleanString(req.Department), s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]order.Product{"product": prod})
}

func (s *Server) handleSetCustomerCoordinator(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "customer id must be an integer")
		return
	}
	var req struct {
		Username string `json:"username"`
	}
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	username := cleanString(req.Username)
	if username != "" {
		u, err := s.store.UserByUsername(r.Context(), username)
		if err != nil || u.Role != auth.RoleSC {
			s.writeError(w, http.StatusBadRequest, "bad_request", "coordinator must be an existing user with the SC role")
			return
		}
	}
	cust, err := s.store.SetCustomerCoordinator(r.Context(), id, username)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"customer": cust})
}
