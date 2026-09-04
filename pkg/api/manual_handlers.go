package api

import (
	"net/http"

	"cockpit/pkg/order"
)

func (s *Server) handleListProducts(w http.ResponseWriter, r *http.Request) {
	products, err := s.store.Products(r.Context())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.Product{"products": products})
}

func (s *Server) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	var req order.CreateProductRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	product, err := s.store.CreateProduct(r.Context(), req.Code, req.Name, req.Description, s.currentUser(r).Username, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]order.Product{"product": product})
}

func (s *Server) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	product, blocks, err := s.store.ProductByID(r.Context(), id)
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"product": product,
		"blocks":  blocks,
	})
}

func (s *Server) handleUpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	var req order.UpdateProductRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	product, err := s.store.UpdateProduct(r.Context(), id, req.Code, req.Name, req.Description, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]order.Product{"product": product})
}

func (s *Server) handleDeleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	if err := s.store.DeleteProduct(r.Context(), id); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAddManualBlock(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	var req order.ManualBlockRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	block, err := s.store.AddManualBlock(r.Context(), id, req.Title, req.Body, req.Assignee, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]order.ManualBlock{"block": block})
}

func (s *Server) handleUpdateManualBlock(w http.ResponseWriter, r *http.Request) {
	productID, err := parseID(r, "pid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	blockID, err := parseID(r, "bid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "block id must be an integer")
		return
	}
	var req order.ManualBlockPatchRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	block, err := s.store.UpdateManualBlock(r.Context(), productID, blockID, req.Title, req.Body, req.Assignee, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]order.ManualBlock{"block": block})
}

func (s *Server) handleMoveManualBlock(w http.ResponseWriter, r *http.Request) {
	productID, err := parseID(r, "pid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	blockID, err := parseID(r, "bid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "block id must be an integer")
		return
	}
	var req order.ManualBlockMoveRequest
	if err := s.decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	blocks, err := s.store.MoveManualBlock(r.Context(), productID, blockID, req.Direction, s.now().UTC())
	if err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string][]order.ManualBlock{"blocks": blocks})
}

func (s *Server) handleDeleteManualBlock(w http.ResponseWriter, r *http.Request) {
	productID, err := parseID(r, "pid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "product id must be an integer")
		return
	}
	blockID, err := parseID(r, "bid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "bad_request", "block id must be an integer")
		return
	}
	if err := s.store.DeleteManualBlock(r.Context(), productID, blockID); err != nil {
		status, code, msg := s.classifyError(err)
		s.writeError(w, status, code, msg)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
