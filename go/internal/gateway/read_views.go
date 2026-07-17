package gateway

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/vault"
	"github.com/infera/infera/go/pkg/types"
)

func (g *Gateway) listModelEntries() ([]map[string]interface{}, error) {
	workers := g.router.GetWorkers("", false)
	loadedSet := make(map[string]bool)
	for _, worker := range workers {
		for _, model := range worker.LoadedModels {
			loadedSet[model.ModelID] = true
		}
	}

	now := time.Now().Unix()
	if g.vaultHandler == nil {
		models := make([]map[string]interface{}, 0, len(loadedSet))
		for modelID := range loadedSet {
			models = append(models, map[string]interface{}{
				"id":       modelID,
				"object":   "model",
				"created":  now,
				"owned_by": "infera",
			})
		}
		sortEntriesByStringKey(models, "id")
		return models, nil
	}

	vaultModels, err := g.vaultHandler.Store().List(&vault.ModelFilter{})
	if err != nil {
		return nil, err
	}

	coveredByVault := make(map[string]bool)
	models := make([]map[string]interface{}, 0, len(vaultModels)+len(loadedSet))
	for _, vm := range vaultModels {
		loaded := loadedSet[vm.SourceURI]
		if loaded {
			coveredByVault[vm.SourceURI] = true
		}
		models = append(models, map[string]interface{}{
			"id":            vm.SourceURI,
			"object":        "model",
			"created":       now,
			"owned_by":      "infera",
			"loaded":        loaded,
			"family":        vm.Family,
			"parameters":    vm.Parameters,
			"quantization":  vm.Quantization,
			"vram_required": vm.VRAMRequired,
			"max_context":   vm.MaxContext,
			"tags":          vm.Tags,
			"vault_status":  vm.Status,
		})
	}

	for modelID := range loadedSet {
		if !coveredByVault[modelID] {
			models = append(models, map[string]interface{}{
				"id":       modelID,
				"object":   "model",
				"created":  now,
				"owned_by": "infera",
				"loaded":   true,
			})
		}
	}

	sortEntriesByStringKey(models, "id")
	return models, nil
}

func (g *Gateway) modelExists(modelID string) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false
	}
	models, err := g.listModelEntries()
	if err != nil {
		return false
	}
	for _, model := range models {
		if id, ok := model["id"].(string); ok && id == modelID {
			return true
		}
	}
	return false
}

func (g *Gateway) workersForWorkspace(workspaceID string) []*types.WorkerInfo {
	workspaceID = normalizeWorkspaceIDForGateway(workspaceID)
	workers := g.router.GetWorkers("", false)
	filtered := make([]*types.WorkerInfo, 0, len(workers))
	for _, worker := range workers {
		if worker.SharedPool || normalizeWorkspaceIDForGateway(worker.WorkspaceID) == workspaceID {
			filtered = append(filtered, worker)
		}
	}
	return filtered
}

func (g *Gateway) listWorkerEntries(workspaceID string) []map[string]interface{} {
	workers := g.workersForWorkspace(workspaceID)
	response := make([]map[string]interface{}, 0, len(workers))
	for _, worker := range workers {
		models := make([]string, 0, len(worker.LoadedModels))
		for _, model := range worker.LoadedModels {
			models = append(models, model.ModelID)
		}
		sort.Strings(models)

		response = append(response, map[string]interface{}{
			"worker_id":        worker.WorkerID,
			"address":          worker.Address,
			"status":           worker.Status,
			"models":           models,
			"gpu_utilization":  worker.Stats.GPUUtilization,
			"memory_used":      worker.Stats.MemoryUsedBytes,
			"memory_total":     worker.Stats.MemoryTotalBytes,
			"queue_depth":      worker.Stats.QueueDepth,
			"requests_per_sec": worker.Stats.RequestsPerSecond,
			"avg_latency_ms":   worker.Stats.AvgLatencyMS,
			"p50_latency_ms":   worker.Stats.P50LatencyMS,
			"p99_latency_ms":   worker.Stats.P99LatencyMS,
			"error_rate":       worker.Stats.ErrorRate,
			"last_heartbeat":   worker.LastHealthCheck,
		})
	}
	sortEntriesByStringKey(response, "worker_id")
	return response
}

func (g *Gateway) statsPayload(workspaceID string) map[string]interface{} {
	workers := g.workersForWorkspace(workspaceID)

	var totalRPS float64
	var totalMemoryUsed, totalMemoryTotal int64
	var totalGPUUtil float64
	healthyCount := 0
	totalQueueDepth := 0
	models := make(map[string]struct{})
	var weightedLatencySum, totalWeight float64

	for _, worker := range workers {
		totalRPS += worker.Stats.RequestsPerSecond
		totalMemoryUsed += worker.Stats.MemoryUsedBytes
		totalMemoryTotal += worker.Stats.MemoryTotalBytes
		totalGPUUtil += worker.Stats.GPUUtilization
		totalQueueDepth += worker.Stats.QueueDepth
		for _, model := range worker.LoadedModels {
			models[model.ModelID] = struct{}{}
		}
		if worker.IsHealthy() {
			healthyCount++
		}
		weight := worker.Stats.RequestsPerSecond
		if weight == 0 {
			weight = 1
		}
		weightedLatencySum += worker.Stats.AvgLatencyMS * weight
		totalWeight += weight
	}

	avgLatency := 0.0
	if totalWeight > 0 {
		avgLatency = weightedLatencySum / totalWeight
	}
	avgGPUUtil := 0.0
	if len(workers) > 0 {
		avgGPUUtil = totalGPUUtil / float64(len(workers))
	}

	return map[string]interface{}{
		"workers": map[string]interface{}{
			"total":   len(workers),
			"healthy": healthyCount,
		},
		"models": map[string]interface{}{
			"available": len(models),
		},
		"requests": map[string]interface{}{
			"per_second":  totalRPS,
			"queue_depth": totalQueueDepth,
		},
		"latency": map[string]interface{}{
			"avg_ms": avgLatency,
		},
		"gpu": map[string]interface{}{
			"avg_utilization": avgGPUUtil,
		},
		"memory": map[string]interface{}{
			"used_bytes":  totalMemoryUsed,
			"total_bytes": totalMemoryTotal,
		},
		"uptime_seconds": int64(time.Since(g.startedAt).Seconds()),
	}
}

func (g *Gateway) usageSummaryPayload(workspaceID string, now time.Time) (map[string]interface{}, error) {
	workspaceID = normalizeWorkspaceIDForGateway(workspaceID)
	if g.auditStore == nil {
		return nil, fmt.Errorf("audit store is not configured")
	}

	now = now.UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	summary, err := g.auditStore.UsageSummary(audit.UsageSummaryQuery{
		Start:       monthStart,
		End:         now,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}

	var totals audit.UsageSummary
	if summary != nil {
		totals = *summary
	}

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	trendStart := todayStart.AddDate(0, 0, -6)
	trendEnd := todayStart.AddDate(0, 0, 1)
	rows, err := g.auditStore.UsageByKey(audit.UsageQuery{
		Start:       trendStart,
		End:         trendEnd,
		Bucket:      "day",
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}

	type usageBucket struct {
		Attempts          int64
		Requests          int64
		Tokens            int64
		ExactRequests     int64
		EstimatedRequests int64
		ExactTokens       int64
		EstimatedTokens   int64
		Successes         int64
		Errors            int64
		CostNano          int64
		CostedTokens      int64
		ExactCosts        int64
		EstimatedCosts    int64
		UnavailableCosts  int64
	}
	aggregate := make(map[int64]usageBucket, len(rows))
	for _, row := range rows {
		current := aggregate[row.BucketStartMS]
		current.Attempts += row.AttemptCount
		current.Requests += row.RequestCount
		current.Tokens += row.TokenCount
		current.ExactRequests += row.ExactRequestCount
		current.EstimatedRequests += row.EstimatedRequestCount
		current.ExactTokens += row.ExactTokenCount
		current.EstimatedTokens += row.EstimatedTokenCount
		current.Successes += row.SuccessCount
		current.Errors += row.ErrorCount
		current.CostNano += row.CostNano
		current.CostedTokens += row.CostedTokenCount
		current.ExactCosts += row.ExactCostCount
		current.EstimatedCosts += row.EstimatedCostCount
		current.UnavailableCosts += row.UnavailableCostCount
		aggregate[row.BucketStartMS] = current
	}

	trend := make([]map[string]any, 0, 7)
	for bucket := trendStart; bucket.Before(trendEnd); bucket = bucket.Add(24 * time.Hour) {
		snapshot := aggregate[bucket.UnixMilli()]
		trend = append(trend, map[string]any{
			"bucket_start":       bucket.Format(time.RFC3339),
			"attempts":           snapshot.Attempts,
			"requests":           snapshot.Requests,
			"tokens":             snapshot.Tokens,
			"exact_requests":     snapshot.ExactRequests,
			"estimated_requests": snapshot.EstimatedRequests,
			"exact_tokens":       snapshot.ExactTokens,
			"estimated_tokens":   snapshot.EstimatedTokens,
			"successes":          snapshot.Successes,
			"errors":             snapshot.Errors,
			"cost":               buildCostMetrics(snapshot.CostNano, snapshot.CostedTokens, snapshot.ExactCosts, snapshot.EstimatedCosts, snapshot.UnavailableCosts),
		})
	}

	return map[string]any{
		"workspace_id":   workspaceID,
		"reconciliation": reconcileUsageSummary(totals),
		"period": map[string]any{
			"current_month_start": monthStart.Format(time.RFC3339),
			"current_period_end":  now.Format(time.RFC3339),
			"trend_start":         trendStart.Format(time.RFC3339),
			"trend_end":           trendEnd.Format(time.RFC3339),
			"trend_bucket":        "day",
		},
		"totals": map[string]any{
			"attempts":           totals.AttemptCount,
			"requests":           totals.RequestCount,
			"tokens":             totals.TokenCount,
			"exact_requests":     totals.ExactRequestCount,
			"estimated_requests": totals.EstimatedRequestCount,
			"exact_tokens":       totals.ExactTokenCount,
			"estimated_tokens":   totals.EstimatedTokenCount,
			"successes":          totals.SuccessCount,
			"errors":             totals.ErrorCount,
			"cost":               buildCostMetrics(totals.CostNano, totals.CostedTokenCount, totals.ExactCostCount, totals.EstimatedCostCount, totals.UnavailableCostCount),
		},
		"daily_trend": trend,
	}, nil
}

func (g *Gateway) quotaStatusPayload(workspaceID string, now time.Time) (map[string]interface{}, error) {
	workspaceID = normalizeWorkspaceIDForGateway(workspaceID)
	if g.authHandler == nil {
		return nil, fmt.Errorf("auth handler is not configured")
	}

	usageSummary, err := g.usageSummaryPayload(workspaceID, now)
	if err != nil {
		return nil, err
	}

	quota, err := g.authHandler.Store().GetWorkspaceQuota(workspaceID)
	if err != nil {
		return nil, err
	}

	totals, _ := usageSummary["totals"].(map[string]any)
	requestsUsed, _ := totals["requests"].(int64)
	tokensUsed, _ := totals["tokens"].(int64)
	requestPressure := computeQuotaPressure(quota.MonthlyRequestLimit, requestsUsed)
	tokenPressure := computeQuotaPressure(quota.MonthlyTokenLimit, tokensUsed)

	return map[string]any{
		"workspace_id": workspaceID,
		"period":       usageSummary["period"],
		"quota": map[string]any{
			"monthly_request_limit": quota.MonthlyRequestLimit,
			"monthly_token_limit":   quota.MonthlyTokenLimit,
			"enforce_hard_limits":   quota.EnforceHardLimits,
			"updated_at":            quota.UpdatedAt.UTC().Format(time.RFC3339),
		},
		"usage": map[string]any{
			"requests":  requestsUsed,
			"tokens":    tokensUsed,
			"successes": totals["successes"],
			"errors":    totals["errors"],
		},
		"pressure": map[string]any{
			"overall_status": deriveQuotaOverallStatus(requestPressure["status"], tokenPressure["status"]),
			"requests":       requestPressure,
			"tokens":         tokenPressure,
		},
	}, nil
}

func (h *InstanceHandlers) listInstanceEntriesForWorkspace(workspaceID string) []map[string]interface{} {
	instances := h.manager.ListInstances()
	if normalizeWorkspaceIDForGateway(workspaceID) != auth.DefaultWorkspaceID {
		instances = h.manager.ListInstancesByWorkspace(normalizeWorkspaceIDForGateway(workspaceID))
	}

	response := make([]map[string]interface{}, 0, len(instances))
	for _, instance := range instances {
		response = append(response, instanceToMap(instance))
	}
	sortEntriesByStringKey(response, "id")
	return response
}

func (h *InstanceHandlers) listDeploymentEntries(workspaceID string, limit int) ([]*deployments.AttemptRecord, error) {
	if h.deploymentStore == nil {
		return nil, nil
	}
	return h.deploymentStore.ListAttempts(normalizeWorkspaceIDForGateway(workspaceID), limit)
}

func (h *InstanceHandlers) listProviderEntries(ctx context.Context, workspaceID string) []map[string]interface{} {
	statuses := h.manager.GetProviderStatusForWorkspace(ctx, normalizeWorkspaceIDForGateway(workspaceID))
	response := make([]map[string]interface{}, 0, len(statuses))
	for _, status := range statuses {
		response = append(response, map[string]interface{}{
			"provider":         status.Provider,
			"connected":        status.Connected,
			"account_id":       status.AccountID,
			"balance":          status.Balance,
			"active_instances": status.ActiveCount,
			"quota_limit":      status.QuotaLimit,
			"error":            status.ErrorMessage,
			"error_code":       status.ErrorCode,
			"capabilities":     status.Capabilities,
		})
	}
	sortEntriesByStringKey(response, "provider")
	return response
}

func sortEntriesByStringKey(entries []map[string]interface{}, key string) {
	sort.Slice(entries, func(i, j int) bool {
		return entryString(entries[i], key) < entryString(entries[j], key)
	})
}

func entryString(entry map[string]interface{}, key string) string {
	value, _ := entry[key].(string)
	return value
}

func normalizeWorkspaceIDForGateway(workspaceID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return auth.DefaultWorkspaceID
	}
	return workspaceID
}

func computeQuotaPressure(limit *int64, used int64) map[string]any {
	if limit == nil || *limit <= 0 {
		return map[string]any{
			"used":   used,
			"limit":  limit,
			"ratio":  0.0,
			"status": "unlimited",
		}
	}

	ratio := float64(used) / float64(*limit)
	status := "healthy"
	switch {
	case used > *limit:
		status = "exceeded"
	case ratio >= 0.8:
		status = "near_limit"
	}

	return map[string]any{
		"used":   used,
		"limit":  *limit,
		"ratio":  ratio,
		"status": status,
	}
}

func deriveQuotaOverallStatus(statuses ...any) string {
	overall := "unlimited"
	for _, raw := range statuses {
		status, _ := raw.(string)
		switch status {
		case "exceeded":
			return "exceeded"
		case "near_limit":
			overall = "near_limit"
		case "healthy":
			if overall != "near_limit" {
				overall = "healthy"
			}
		}
	}
	return overall
}
