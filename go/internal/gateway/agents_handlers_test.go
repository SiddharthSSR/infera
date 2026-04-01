package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/agents"
	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
)

func newGatewayWithAgentsRuntime(t *testing.T, modelID string, transport http.RoundTripper) (*Gateway, *agents.Store) {
	t.Helper()
	g := newGatewayWithTestWorker(t, modelID, transport)

	store, err := agents.NewStore(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("agents.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	runtime, err := g.NewAgentsRuntime(store)
	if err != nil {
		t.Fatalf("NewAgentsRuntime: %v", err)
	}
	g.SetAgentRuntime(runtime)
	return g, store
}

func waitForAgentRun(t *testing.T, runtime *agents.Runtime, workspaceID, runID string, allowed ...agents.Status) *agents.RunDetail {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		detail, err := runtime.GetRunDetail(workspaceID, runID)
		if err == nil {
			for _, status := range allowed {
				if detail.Run.Status == status {
					return detail
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	detail, err := runtime.GetRunDetail(workspaceID, runID)
	if err != nil {
		t.Fatalf("GetRunDetail: %v", err)
	}
	t.Fatalf("timed out waiting for run %s to reach %v, got %s", runID, allowed, detail.Run.Status)
	return nil
}

func TestHandleAgentsListsHermes(t *testing.T) {
	g, _ := newGatewayWithAgentsRuntime(t, "meta-llama/Meta-Llama-3.1-8B-Instruct", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"meta-llama/Meta-Llama-3.1-8B-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":"{\"type\":\"final\",\"message\":\"ok\"}"},"finish_reason":"stop"}]}`), nil
	}))

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/agents", nil), auth.RoleUser)
	w := httptest.NewRecorder()
	g.handleAgents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Agents         []agents.AgentDescriptor `json:"agents"`
		DefaultAgentID string                   `json:"default_agent_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.DefaultAgentID != "hermes" {
		t.Fatalf("expected default_agent_id=hermes, got %q", resp.DefaultAgentID)
	}
	if len(resp.Agents) != 1 || resp.Agents[0].ID != "hermes" {
		t.Fatalf("expected Hermes in response, got %+v", resp.Agents)
	}
}

func TestHandleAgentsFiltersHermesToolsByRole(t *testing.T) {
	g, _ := newGatewayWithAgentsRuntime(t, "meta-llama/Meta-Llama-3.1-8B-Instruct", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"meta-llama/Meta-Llama-3.1-8B-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":"{\"type\":\"final\",\"message\":\"ok\"}"},"finish_reason":"stop"}]}`), nil
	}))

	cases := []struct {
		name string
		role string
		want []string
	}{
		{
			name: "owner sees infra and usage tools",
			role: auth.RoleOwner,
			want: []string{"get_gateway_stats", "get_provider_status", "get_quota_status", "get_usage_summary", "list_deployments", "list_instances", "list_models", "list_workers"},
		},
		{
			name: "operator sees infra but not usage",
			role: auth.RoleOperator,
			want: []string{"get_gateway_stats", "get_provider_status", "list_deployments", "list_instances", "list_models", "list_workers"},
		},
		{
			name: "billing sees usage but not infra",
			role: auth.RoleBilling,
			want: []string{"get_quota_status", "get_usage_summary", "list_models"},
		},
		{
			name: "read only sees both read bundles",
			role: auth.RoleReadOnly,
			want: []string{"get_gateway_stats", "get_provider_status", "get_quota_status", "get_usage_summary", "list_deployments", "list_instances", "list_models", "list_workers"},
		},
		{
			name: "user only sees model listing",
			role: auth.RoleUser,
			want: []string{"list_models"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/agents", nil), tc.role)
			w := httptest.NewRecorder()
			g.handleAgents(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp struct {
				Agents []agents.AgentDescriptor `json:"agents"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if len(resp.Agents) != 1 {
				t.Fatalf("expected one agent, got %d", len(resp.Agents))
			}

			got := make([]string, 0, len(resp.Agents[0].Tools))
			for _, tool := range resp.Agents[0].Tools {
				got = append(got, tool.Name)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("expected tools %v, got %v", tc.want, got)
			}
		})
	}
}

func TestHandleAgentRunsWorkspaceScoped(t *testing.T) {
	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"
	g, _ := newGatewayWithAgentsRuntime(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"`+modelID+`","choices":[{"index":0,"message":{"role":"assistant","content":"{\"type\":\"final\",\"message\":\"done\"}"},"finish_reason":"stop"}]}`), nil
	}))

	createReq := authedWorkspaceRequest(
		httptest.NewRequest(http.MethodPost, "/api/agents/runs", strings.NewReader(`{"model":"`+modelID+`","input":"inspect workers"}`)),
		auth.RoleOwner,
		"ws_alpha",
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRes := httptest.NewRecorder()
	g.handleCreateAgentRun(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRes.Code, createRes.Body.String())
	}

	var created struct {
		Run *agents.Run `json:"run"`
	}
	if err := json.Unmarshal(createRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if created.Run == nil {
		t.Fatal("expected run payload")
	}

	waitForAgentRun(t, g.agentRuntime, "ws_alpha", created.Run.ID, agents.StatusSucceeded)

	listAlphaReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/agents/runs", nil), auth.RoleOwner, "ws_alpha")
	listAlphaRes := httptest.NewRecorder()
	g.handleListAgentRuns(listAlphaRes, listAlphaReq)
	if listAlphaRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listAlphaRes.Code)
	}

	listBetaReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/agents/runs", nil), auth.RoleOwner, "ws_beta")
	listBetaRes := httptest.NewRecorder()
	g.handleListAgentRuns(listBetaRes, listBetaReq)
	if listBetaRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listBetaRes.Code)
	}

	var listBeta struct {
		Runs []agents.Run `json:"runs"`
	}
	if err := json.Unmarshal(listBetaRes.Body.Bytes(), &listBeta); err != nil {
		t.Fatalf("json.Unmarshal beta: %v", err)
	}
	if len(listBeta.Runs) != 0 {
		t.Fatalf("expected ws_beta to see 0 runs, got %d", len(listBeta.Runs))
	}

	detailOtherReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/agents/runs/"+created.Run.ID, nil), auth.RoleOwner, "ws_beta")
	detailOtherRes := httptest.NewRecorder()
	g.handleAgentRunByID(detailOtherRes, detailOtherReq)
	if detailOtherRes.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-workspace run detail, got %d", detailOtherRes.Code)
	}
}

func TestHermesRunRecordsAuditUsage(t *testing.T) {
	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"
	g, _ := newGatewayWithAgentsRuntime(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"`+modelID+`","choices":[{"index":0,"message":{"role":"assistant","content":"{\"type\":\"final\",\"message\":\"audited\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}}`), nil
	}))

	authStore, err := auth.NewStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("auth.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = authStore.Close() })
	g.SetAuthHandler(auth.NewHandler(authStore))

	auditStore, err := audit.NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("audit.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.Close() })
	g.SetAuditStore(auditStore)

	run, err := g.agentRuntime.CreateRun(context.Background(), &auth.KeyRecord{
		ID:            "key-1",
		KeyPrefix:     "key_abc",
		WorkspaceID:   auth.DefaultWorkspaceID,
		WorkspaceName: auth.DefaultWorkspaceName,
		Role:          auth.RoleOwner,
		PrincipalType: auth.PrincipalHuman,
		Status:        "active",
	}, nil, agents.CreateRunRequest{
		Model: modelID,
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	waitForAgentRun(t, g.agentRuntime, auth.DefaultWorkspaceID, run.ID, agents.StatusSucceeded)

	summary, err := auditStore.UsageSummary(audit.UsageSummaryQuery{
		Start:       time.Now().UTC().Add(-time.Hour),
		End:         time.Now().UTC().Add(time.Hour),
		WorkspaceID: auth.DefaultWorkspaceID,
	})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary == nil || summary.RequestCount != 1 {
		t.Fatalf("expected one audited Hermes request, got %+v", summary)
	}
}

func TestHermesRunRespectsWorkspaceQuota(t *testing.T) {
	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"
	g, _ := newGatewayWithAgentsRuntime(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"`+modelID+`","choices":[{"index":0,"message":{"role":"assistant","content":"{\"type\":\"final\",\"message\":\"should not run\"}"},"finish_reason":"stop"}]}`), nil
	}))

	authStore, err := auth.NewStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("auth.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = authStore.Close() })
	g.SetAuthHandler(auth.NewHandler(authStore))

	zero := int64(0)
	if _, err := authStore.UpsertWorkspaceQuota(auth.DefaultWorkspaceID, &zero, nil, true); err != nil {
		t.Fatalf("UpsertWorkspaceQuota: %v", err)
	}

	auditStore, err := audit.NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("audit.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.Close() })
	g.SetAuditStore(auditStore)

	run, err := g.agentRuntime.CreateRun(context.Background(), &auth.KeyRecord{
		ID:            "key-1",
		KeyPrefix:     "key_abc",
		WorkspaceID:   auth.DefaultWorkspaceID,
		WorkspaceName: auth.DefaultWorkspaceName,
		Role:          auth.RoleOwner,
		PrincipalType: auth.PrincipalHuman,
		Status:        "active",
	}, nil, agents.CreateRunRequest{
		Model: modelID,
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForAgentRun(t, g.agentRuntime, auth.DefaultWorkspaceID, run.ID, agents.StatusFailed)
	if !strings.Contains(detail.Run.FailureReason, "quota exceeded") {
		t.Fatalf("expected quota failure, got %q", detail.Run.FailureReason)
	}
}

func TestWorkspaceHealthHelpersAreWorkspaceScoped(t *testing.T) {
	g, _ := newGatewayWithAgentsRuntime(t, "meta-llama/Meta-Llama-3.1-8B-Instruct", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"meta-llama/Meta-Llama-3.1-8B-Instruct","choices":[{"index":0,"message":{"role":"assistant","content":"{\"type\":\"final\",\"message\":\"ok\"}"},"finish_reason":"stop"}]}`), nil
	}))

	authStore, err := auth.NewStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("auth.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = authStore.Close() })
	g.SetAuthHandler(auth.NewHandler(authStore))

	auditStore, err := audit.NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("audit.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.Close() })
	g.SetAuditStore(auditStore)

	alpha, err := authStore.CreateWorkspace("Alpha Team")
	if err != nil {
		t.Fatalf("CreateWorkspace alpha: %v", err)
	}
	beta, err := authStore.CreateWorkspace("Beta Team")
	if err != nil {
		t.Fatalf("CreateWorkspace beta: %v", err)
	}

	alphaReqLimit := int64(10)
	alphaTokenLimit := int64(500)
	if _, err := authStore.UpsertWorkspaceQuota(alpha.ID, &alphaReqLimit, &alphaTokenLimit, true); err != nil {
		t.Fatalf("UpsertWorkspaceQuota alpha: %v", err)
	}
	betaReqLimit := int64(200)
	betaTokenLimit := int64(9000)
	if _, err := authStore.UpsertWorkspaceQuota(beta.ID, &betaReqLimit, &betaTokenLimit, false); err != nil {
		t.Fatalf("UpsertWorkspaceQuota beta: %v", err)
	}

	now := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	records := []audit.InferenceAuditRecord{
		{
			Timestamp:   now.Add(-2 * time.Hour),
			RequestID:   "alpha-1",
			KeyID:       "key-alpha",
			WorkspaceID: alpha.ID,
			Model:       "m1",
			Status:      "success",
			TokenCount:  120,
		},
		{
			Timestamp:   now.Add(-26 * time.Hour),
			RequestID:   "alpha-2",
			KeyID:       "key-alpha",
			WorkspaceID: alpha.ID,
			Model:       "m1",
			Status:      "inference_error",
			TokenCount:  30,
		},
		{
			Timestamp:   now.Add(-3 * time.Hour),
			RequestID:   "beta-1",
			KeyID:       "key-beta",
			WorkspaceID: beta.ID,
			Model:       "m2",
			Status:      "success",
			TokenCount:  999,
		},
	}
	for _, record := range records {
		if err := auditStore.AppendInference(record); err != nil {
			t.Fatalf("AppendInference %s: %v", record.RequestID, err)
		}
	}

	usagePayload, err := g.usageSummaryPayload(alpha.ID, now)
	if err != nil {
		t.Fatalf("usageSummaryPayload: %v", err)
	}
	totals, _ := usagePayload["totals"].(map[string]any)
	if got, _ := totals["requests"].(int64); got != 2 {
		t.Fatalf("expected alpha request count 2, got %v", totals["requests"])
	}
	if got, _ := totals["tokens"].(int64); got != 150 {
		t.Fatalf("expected alpha token count 150, got %v", totals["tokens"])
	}
	if got, _ := totals["errors"].(int64); got != 1 {
		t.Fatalf("expected alpha error count 1, got %v", totals["errors"])
	}
	trend, _ := usagePayload["daily_trend"].([]map[string]any)
	if len(trend) != 7 {
		t.Fatalf("expected 7 trend buckets, got %d", len(trend))
	}

	quotaPayload, err := g.quotaStatusPayload(alpha.ID, now)
	if err != nil {
		t.Fatalf("quotaStatusPayload: %v", err)
	}
	quotaBlock, _ := quotaPayload["quota"].(map[string]any)
	if got, _ := quotaBlock["monthly_request_limit"].(*int64); got == nil || *got != alphaReqLimit {
		t.Fatalf("expected alpha request limit %d, got %#v", alphaReqLimit, quotaBlock["monthly_request_limit"])
	}
	pressure, _ := quotaPayload["pressure"].(map[string]any)
	requestPressure, _ := pressure["requests"].(map[string]any)
	if got, _ := requestPressure["used"].(int64); got != 2 {
		t.Fatalf("expected alpha pressure to use 2 requests, got %v", requestPressure["used"])
	}
	if got, _ := requestPressure["limit"].(int64); got != alphaReqLimit {
		t.Fatalf("expected alpha pressure limit %d, got %v", alphaReqLimit, requestPressure["limit"])
	}
	if got, _ := pressure["overall_status"].(string); got != "healthy" {
		t.Fatalf("expected healthy quota status, got %q", got)
	}
}
