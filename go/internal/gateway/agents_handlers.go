package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/infera/infera/go/internal/agents"
	"github.com/infera/infera/go/internal/auth"
)

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
		AgentID  string `json:"agent_id"`
		Model    string `json:"model"`
		Input    string `json:"input"`
		MaxSteps int    `json:"max_steps"`
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
			AgentID:  req.AgentID,
			Model:    req.Model,
			Input:    req.Input,
			MaxSteps: req.MaxSteps,
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
			"run":   detail.Run,
			"steps": detail.Steps,
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
