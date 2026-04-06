package gateway

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "stream":
		g.handleAgentRunStream(w, r, runID)
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

// handleAgentWebhooks dispatches GET (list) and POST (create) on
// /api/agents/webhooks.
func (g *Gateway) handleAgentWebhooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		g.handleListAgentWebhooks(w, r)
	case http.MethodPost:
		g.handleCreateAgentWebhook(w, r)
	default:
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are allowed")
	}
}

func (g *Gateway) handleListAgentWebhooks(w http.ResponseWriter, r *http.Request) {
	webhooks, err := g.agentRuntime.ListWebhookConfigs(currentWorkspaceID(r))
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
		return
	}
	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"webhooks": webhooks,
		"total":    len(webhooks),
	})
}

func (g *Gateway) handleCreateAgentWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "url is required")
		return
	}

	wh, err := g.agentRuntime.CreateWebhookConfig(
		currentWorkspaceID(r),
		req.URL,
		req.Secret,
		req.Events,
	)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	g.writeJSON(w, http.StatusCreated, map[string]interface{}{"webhook": wh})
}

// handleAgentWebhookByID handles DELETE /api/agents/webhooks/:id.
func (g *Gateway) handleAgentWebhookByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only DELETE is allowed")
		return
	}

	// Extract the webhook ID from the URL path suffix.
	webhookID := strings.TrimPrefix(r.URL.Path, "/api/agents/webhooks/")
	webhookID = strings.Trim(webhookID, "/")
	if strings.TrimSpace(webhookID) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Webhook ID is required")
		return
	}

	err := g.agentRuntime.DeleteWebhookConfig(currentWorkspaceID(r), webhookID)
	if err == sql.ErrNoRows {
		g.writeError(w, http.StatusNotFound, "not_found", "Webhook not found")
		return
	}
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
		return
	}
	g.writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

func (g *Gateway) handleAgentRunStream(w http.ResponseWriter, r *http.Request, runID string) {
	workspaceID := currentWorkspaceID(r)

	// Verify the run exists and belongs to this workspace before opening the stream.
	run, err := g.agentRuntime.GetRun(workspaceID, runID)
	if err == sql.ErrNoRows {
		g.writeError(w, http.StatusNotFound, "not_found", "Run not found")
		return
	}
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		g.writeError(w, http.StatusInternalServerError, "streaming_not_supported", "Streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx) so bytes reach the client immediately.
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	lastIndex := -1

	// Send an initial status event with the full current run state and all
	// steps that already exist.  This lets the client hydrate immediately
	// without a separate REST call.
	detail, _ := g.agentRuntime.GetRunDetail(workspaceID, runID)
	if detail != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"run":   detail.Run,
			"steps": detail.Steps,
		})
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
		flusher.Flush()
		if len(detail.Steps) > 0 {
			lastIndex = detail.Steps[len(detail.Steps)-1].Index
		}
		run = detail.Run
	}

	// If the run is already terminal we can send "done" immediately and close.
	if run.Status == agents.StatusSucceeded || run.Status == agents.StatusFailed || run.Status == agents.StatusCanceled {
		data, _ := json.Marshal(map[string]interface{}{"run": run})
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Poll the store every 500 ms for new steps and status changes.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected — exit cleanly.
			return
		case <-ticker.C:
			// Emit any steps that were appended since the last poll.
			newSteps, _ := g.agentRuntime.ListStepsAfter(workspaceID, runID, lastIndex)
			for _, step := range newSteps {
				data, _ := json.Marshal(map[string]interface{}{"step": step})
				fmt.Fprintf(w, "event: step\ndata: %s\n\n", data)
				if step.Index > lastIndex {
					lastIndex = step.Index
				}
			}
			if len(newSteps) > 0 {
				flusher.Flush()
			}

			// Check whether the run has reached a terminal state.
			currentRun, err := g.agentRuntime.GetRun(workspaceID, runID)
			if err != nil {
				// Transient DB error — keep polling.
				continue
			}
			if currentRun.Status == agents.StatusSucceeded || currentRun.Status == agents.StatusFailed || currentRun.Status == agents.StatusCanceled {
				data, _ := json.Marshal(map[string]interface{}{"run": currentRun})
				fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
				flusher.Flush()
				return
			}
		}
	}
}

// handleAgentDefinitions handles GET/POST /api/agents/definitions.
//
// GET  — returns the list of custom definitions for the workspace combined with
// the built-in (registered) agent descriptors.
//
// POST — creates a new custom agent definition; caller must have the
// manage_vault permission (owners and admins).
func (g *Gateway) handleAgentDefinitions(w http.ResponseWriter, r *http.Request) {
	if g.agentRuntime == nil {
		g.writeError(w, http.StatusServiceUnavailable, "agents_unavailable", "Agent runtime is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		g.handleListAgentDefinitions(w, r)
	case http.MethodPost:
		g.handleCreateAgentDefinition(w, r)
	default:
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are allowed")
	}
}

func (g *Gateway) handleListAgentDefinitions(w http.ResponseWriter, r *http.Request) {
	actor := auth.KeyFromContext(r.Context())
	workspaceID := currentWorkspaceID(r)

	builtIn := g.agentRuntime.ListDefinitions(actor)

	custom, err := g.agentRuntime.ListCustomDefinitions(workspaceID)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
		return
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"built_in": builtIn,
		"custom":   custom,
	})
}

func (g *Gateway) handleCreateAgentDefinition(w http.ResponseWriter, r *http.Request) {
	actor := auth.KeyFromContext(r.Context())
	if !auth.HasPermission(actor, auth.PermissionManageVault) {
		g.writeError(w, http.StatusForbidden, "forbidden", "Managing agent definitions requires owner or admin role")
		return
	}

	var req struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		SystemPrompt   string   `json:"system_prompt"`
		Tools          []string `json:"tools"`
		MaxSteps       int      `json:"max_steps"`
		TimeoutSeconds int      `json:"timeout_seconds"`
		Model          string   `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if strings.TrimSpace(req.SystemPrompt) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "system_prompt is required")
		return
	}

	def, err := g.agentRuntime.CreateCustomDefinition(currentWorkspaceID(r), agents.CreateCustomDefinitionRequest{
		Name:           req.Name,
		Description:    req.Description,
		SystemPrompt:   req.SystemPrompt,
		Tools:          req.Tools,
		MaxSteps:       req.MaxSteps,
		TimeoutSeconds: req.TimeoutSeconds,
		Model:          req.Model,
	})
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	g.writeJSON(w, http.StatusCreated, map[string]interface{}{"definition": def})
}

// handleAgentDefinitionByID handles DELETE /api/agents/definitions/{id}.
func (g *Gateway) handleAgentDefinitionByID(w http.ResponseWriter, r *http.Request) {
	if g.agentRuntime == nil {
		g.writeError(w, http.StatusServiceUnavailable, "agents_unavailable", "Agent runtime is not configured")
		return
	}
	if r.Method != http.MethodDelete {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only DELETE is allowed")
		return
	}

	actor := auth.KeyFromContext(r.Context())
	if !auth.HasPermission(actor, auth.PermissionManageVault) {
		g.writeError(w, http.StatusForbidden, "forbidden", "Managing agent definitions requires owner or admin role")
		return
	}

	defID := strings.TrimPrefix(r.URL.Path, "/api/agents/definitions/")
	defID = strings.TrimSpace(strings.Trim(defID, "/"))
	if defID == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Definition ID is required")
		return
	}

	if err := g.agentRuntime.DeleteCustomDefinition(currentWorkspaceID(r), defID); err == sql.ErrNoRows {
		g.writeError(w, http.StatusNotFound, "not_found", "Agent definition not found")
		return
	} else if err != nil {
		g.writeError(w, http.StatusInternalServerError, "agents_unavailable", err.Error())
		return
	}
	g.writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// handleExternalAgentRun handles POST /v1/agents/runs.
//
// This endpoint is API-key authenticated (same as /v1/chat/completions).  It
// accepts the same body as POST /api/agents/runs and additionally supports a
// `wait` query parameter:
//
//   - wait=true  — blocks until the run reaches a terminal state (up to the
//     agent timeout) and returns the full RunDetail.
//   - wait=false (default) — returns immediately with the newly created Run
//     and its run ID so the caller can poll /api/agents/runs/{id}.
func (g *Gateway) handleExternalAgentRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}
	if g.agentRuntime == nil {
		g.writeError(w, http.StatusServiceUnavailable, "agents_unavailable", "Agent runtime is not configured")
		return
	}

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

	// Fast path: caller does not want to wait.
	waitParam := strings.TrimSpace(r.URL.Query().Get("wait"))
	if waitParam != "true" {
		g.writeJSON(w, http.StatusCreated, map[string]interface{}{"run": run})
		return
	}

	// Blocking path: poll until the run reaches a terminal state.
	// We cap the overall wait at a reasonable ceiling to prevent holding
	// the connection open indefinitely regardless of the agent's timeout.
	const maxWait = 120 * time.Second
	const pollInterval = 500 * time.Millisecond

	deadline := time.Now().Add(maxWait)
	workspaceID := currentWorkspaceID(r)

	for {
		if time.Now().After(deadline) {
			// Return what we have even though it is not yet terminal.
			detail, _ := g.agentRuntime.GetRunDetail(workspaceID, run.ID)
			if detail != nil {
				g.writeJSON(w, http.StatusOK, map[string]interface{}{
					"run":         detail.Run,
					"steps":       detail.Steps,
					"attachments": detail.Attachments,
					"sources":     detail.Sources,
					"timed_out":   true,
				})
			} else {
				g.writeJSON(w, http.StatusOK, map[string]interface{}{"run": run, "timed_out": true})
			}
			return
		}

		select {
		case <-r.Context().Done():
			// Client disconnected.
			return
		case <-time.After(pollInterval):
		}

		current, err := g.agentRuntime.GetRun(workspaceID, run.ID)
		if err != nil {
			// Transient DB error — keep waiting.
			continue
		}

		if current.Status == agents.StatusSucceeded || current.Status == agents.StatusFailed || current.Status == agents.StatusCanceled {
			detail, err := g.agentRuntime.GetRunDetail(workspaceID, run.ID)
			if err != nil {
				g.writeJSON(w, http.StatusOK, map[string]interface{}{"run": current})
				return
			}
			g.writeJSON(w, http.StatusOK, map[string]interface{}{
				"run":         detail.Run,
				"steps":       detail.Steps,
				"attachments": detail.Attachments,
				"sources":     detail.Sources,
			})
			return
		}
	}
}
