package api

import (
	"net/http"

	"cockpit/pkg/auth"
	"cockpit/pkg/db"
)

func (s *Server) handleListDepartments(w http.ResponseWriter, r *http.Request) {
	departments, err := s.store.ListDepartments(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"departments": departments})
}

type createDepartmentRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleCreateDepartment(w http.ResponseWriter, r *http.Request) {
	var req createDepartmentRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	department, err := s.store.CreateDepartment(r.Context(), cleanString(req.Name), req.Description, s.now().UTC())
	if err != nil {
		if err == db.ErrDuplicateDepartmentName {
			s.writeError(w, http.StatusConflict, "duplicate_name", "department name already exists")
			return
		}
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"department": department})
}

func (s *Server) handleDeleteDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "department id must be an integer")
		return
	}
	if err := s.store.DeleteDepartment(r.Context(), id); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type addUserToDepartmentRequest struct {
	UserID int64 `json:"userId"`
}

func (s *Server) handleAddUserToDepartment(w http.ResponseWriter, r *http.Request) {
	departmentID, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "department id must be an integer")
		return
	}
	var req addUserToDepartmentRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if err := s.store.AddUserToDepartment(r.Context(), req.UserID, departmentID); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func (s *Server) handleRemoveUserFromDepartment(w http.ResponseWriter, r *http.Request) {
	departmentID, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "department id must be an integer")
		return
	}
	userID, err := parseID(r, "userId")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "user id must be an integer")
		return
	}
	if err := s.store.RemoveUserFromDepartment(r.Context(), userID, departmentID); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleListDepartmentUsers(w http.ResponseWriter, r *http.Request) {
	departmentID, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "department id must be an integer")
		return
	}
	users, err := s.store.DepartmentUsers(r.Context(), departmentID)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"userIds": users})
}

func (s *Server) handleListUserDepartments(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "user id must be an integer")
		return
	}
	departments, err := s.store.UserDepartments(r.Context(), userID)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"departments": departments})
}

func init() {
	_ = auth.PermSICreate // ensure auth package is used
}
