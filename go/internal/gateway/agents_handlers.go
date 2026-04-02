package gateway

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/agents"
	"github.com/infera/infera/go/internal/auth"
)

const maxAgentAttachmentBytes = 8 << 20

func (g *Gateway) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	actor := auth.KeyFromContext(r.Context())
	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents":           g.agentRuntime.ListDefinitions(actor),
		"default_agent_id": "hermes",
	})
}

func (g *Gateway) handleAgentAttachments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}
	if g.agentRuntime == nil {
		g.writeError(w, http.StatusServiceUnavailable, "agents_unavailable", "Agent runtime is not configured")
		return
	}
	if err := r.ParseMultipartForm(maxAgentAttachmentBytes); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Expected multipart form upload")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "file is required")
		return
	}
	defer file.Close()

	limited := io.LimitReader(file, maxAgentAttachmentBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Failed to read uploaded file")
		return
	}
	if int64(len(raw)) > maxAgentAttachmentBytes {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Screenshot exceeds the 8 MiB upload limit")
		return
	}

	mimeType, ok := detectAgentImageMIME(raw)
	if !ok {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Only PNG, JPEG, and WEBP screenshots are supported")
		return
	}

	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil {
		width = cfg.Width
		height = cfg.Height
	}

	sum := sha256.Sum256(raw)
	storageFile := uuid.New().String() + agentAttachmentExtension(mimeType)
	storagePath := filepath.Join(g.agentRuntime.AttachmentRoot(), storageFile)
	if err := os.WriteFile(storagePath, raw, 0o644); err != nil {
		g.writeError(w, http.StatusInternalServerError, "attachment_write_failed", err.Error())
		return
	}

	attachment, err := g.agentRuntime.CreateAttachment(
		auth.KeyFromContext(r.Context()),
		header.Filename,
		mimeType,
		int64(len(raw)),
		width,
		height,
		hex.EncodeToString(sum[:]),
		storagePath,
	)
	if err != nil {
		_ = os.Remove(storagePath)
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	g.writeJSON(w, http.StatusCreated, map[string]interface{}{"attachment": attachment})
}

func detectAgentImageMIME(raw []byte) (string, bool) {
	if len(raw) < 12 {
		return "", false
	}
	contentType := http.DetectContentType(raw)
	switch contentType {
	case "image/png", "image/jpeg":
		return contentType, true
	}
	if string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		return "image/webp", true
	}
	return "", false
}

func agentAttachmentExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}

func (g *Gateway) handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		g.handleListAgentRuns(w, r)
	case http.MethodPost:
		g.handleCreateAgentRun(w, r)
	default:
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are allowed")
	}
}

func (g *Gateway) handleListAgentRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := g.agentRuntime.ListRuns(currentWorkspaceID(r), 25)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
		return
	}
	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":  runs,
		"total": len(runs),
	})
}

func (g *Gateway) handleCreateAgentRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID       string   `json:"agent_id"`
		Mode          string   `json:"mode"`
		AnalysisDepth string   `json:"analysis_depth"`
		Model         string   `json:"model"`
		Input         string   `json:"input"`
		MaxSteps      int      `json:"max_steps"`
		Attachments   []string `json:"attachments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	if strings.TrimSpace(req.Input) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "input is required")
		return
	}
	if !g.modelExists(req.Model) {
		g.writeError(w, http.StatusNotFound, "model_not_found", "Model is not registered in Infera")
		return
	}

	run, err := g.agentRuntime.CreateRun(
		r.Context(),
		auth.KeyFromContext(r.Context()),
		auth.SessionFromContext(r.Context()),
		agents.CreateRunRequest{
			AgentID:       req.AgentID,
			Mode:          agents.RunMode(strings.TrimSpace(req.Mode)),
			AnalysisDepth: agents.AnalysisDepth(strings.TrimSpace(req.AnalysisDepth)),
			Model:         req.Model,
			Input:         req.Input,
			MaxSteps:      req.MaxSteps,
			AttachmentIDs: req.Attachments,
		},
	)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	g.writeJSON(w, http.StatusCreated, map[string]interface{}{"run": run})
}

func (g *Gateway) handleAgentRunByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/runs/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Run ID is required")
		return
	}
	runID := strings.TrimSpace(parts[0])

	switch {
	case r.Method == http.MethodGet && len(parts) == 1:
		detail, err := g.agentRuntime.GetRunDetail(currentWorkspaceID(r), runID)
		if err == sql.ErrNoRows {
			g.writeError(w, http.StatusNotFound, "not_found", "Run not found")
			return
		}
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
			return
		}
		g.writeJSON(w, http.StatusOK, map[string]interface{}{
			"run":         detail.Run,
			"steps":       detail.Steps,
			"attachments": detail.Attachments,
			"sources":     detail.Sources,
		})
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "cancel":
		run, err := g.agentRuntime.CancelRun(currentWorkspaceID(r), runID)
		if err == sql.ErrNoRows {
			g.writeError(w, http.StatusNotFound, "not_found", "Run not found")
			return
		}
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
			return
		}
		g.writeJSON(w, http.StatusOK, map[string]interface{}{"run": run})
	default:
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Unsupported run action")
	}
}
