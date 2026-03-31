package gateway

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/vault"
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

func (g *Gateway) listWorkerEntries() []map[string]interface{} {
	workers := g.router.GetWorkers("", false)
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

func (g *Gateway) statsPayload() map[string]interface{} {
	stats := g.router.GetStats()
	workers := g.router.GetWorkers("", false)

	var totalRPS float64
	var totalMemoryUsed, totalMemoryTotal int64
	var totalGPUUtil float64
	healthyCount := 0
	var weightedLatencySum, totalWeight float64

	for _, worker := range workers {
		totalRPS += worker.Stats.RequestsPerSecond
		totalMemoryUsed += worker.Stats.MemoryUsedBytes
		totalMemoryTotal += worker.Stats.MemoryTotalBytes
		totalGPUUtil += worker.Stats.GPUUtilization
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
			"total":   stats.TotalWorkers,
			"healthy": healthyCount,
		},
		"models": map[string]interface{}{
			"available": stats.ModelsAvailable,
		},
		"requests": map[string]interface{}{
			"per_second":  totalRPS,
			"queue_depth": stats.TotalQueueDepth,
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
