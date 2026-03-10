package vault

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// Handler provides HTTP endpoints for the vault model registry.
type Handler struct {
	store *Store
}

// NewHandler creates a new vault HTTP handler.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// Store returns the underlying model store for direct queries.
func (h *Handler) Store() *Store {
	return h.store
}

// RegisterRoutes mounts vault endpoints on the given mux.
// corsHandler wraps each endpoint with CORS support, matching the gateway pattern.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, corsHandler func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/vault/models", corsHandler(h.handleModels))
	mux.HandleFunc("/api/vault/models/", corsHandler(h.handleModelByID))
	mux.HandleFunc("/api/vault/models/families", corsHandler(h.handleFamilies))
	mux.HandleFunc("/api/vault/stats", corsHandler(h.handleStats))
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listModels(w, r)
	case http.MethodPost:
		h.createModel(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are allowed")
	}
}

func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := &ModelFilter{
		Family:       q.Get("family"),
		Status:       q.Get("status"),
		Quantization: q.Get("quantization"),
		Tag:          q.Get("tag"),
		Search:       q.Get("search"),
	}

	if v := q.Get("min_vram"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.MinVRAM = n
		}
	}
	if v := q.Get("max_vram"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.MaxVRAM = n
		}
	}

	models, err := h.store.List(filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": models,
		"count":  len(models),
	})
}

func (h *Handler) createModel(w http.ResponseWriter, r *http.Request) {
	var m Model
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON: "+err.Error())
		return
	}

	if m.Name == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if m.SourceURI == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "source_uri is required")
		return
	}

	if err := h.store.Create(&m); err != nil {
		h.writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	slog.Info("vault model registered", slog.String("name", m.Name), slog.String("id", m.ID))
	h.writeJSON(w, http.StatusCreated, m)
}

func (h *Handler) handleModelByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/vault/models/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/vault/models/")
	if path == "" || path == "families" {
		// /api/vault/models/families is handled by a separate handler
		// This shouldn't be reached due to ServeMux ordering, but guard anyway
		if path == "families" {
			h.handleFamilies(w, r)
			return
		}
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Model ID required")
		return
	}
	id := path

	switch r.Method {
	case http.MethodGet:
		m, err := h.store.Get(id)
		if err != nil {
			h.writeError(w, http.StatusNotFound, "not_found", "Model not found")
			return
		}
		h.writeJSON(w, http.StatusOK, m)

	case http.MethodPut:
		var m Model
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON: "+err.Error())
			return
		}
		m.ID = id

		if err := h.store.Update(&m); err != nil {
			if strings.Contains(err.Error(), "not found") {
				h.writeError(w, http.StatusNotFound, "not_found", "Model not found")
				return
			}
			h.writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}

		slog.Info("vault model updated", slog.String("name", m.Name), slog.String("id", m.ID))

		// Return the updated model
		updated, err := h.store.Get(id)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		h.writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := h.store.Delete(id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				h.writeError(w, http.StatusNotFound, "not_found", "Model not found")
				return
			}
			h.writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}

		slog.Info("vault model deleted", slog.String("id", id))
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"deleted": id,
		})

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET, PUT, and DELETE are allowed")
	}
}

func (h *Handler) handleFamilies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}

	families, err := h.store.ListFamilies()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"families": families,
	})
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}

	stats, err := h.store.Stats()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, errType, message string) {
	h.writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	})
}
