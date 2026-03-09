package types

import (
	"testing"
	"time"
)

func TestWorkerStatsCurrentLoad(t *testing.T) {
	tests := []struct {
		name     string
		stats    WorkerStats
		wantMin  float64
		wantMax  float64
	}{
		{
			name:    "idle worker",
			stats:   WorkerStats{},
			wantMin: 0.0,
			wantMax: 0.01,
		},
		{
			name:    "GPU heavy",
			stats:   WorkerStats{GPUUtilization: 0.9},
			wantMin: 0.4,
			wantMax: 0.5,
		},
		{
			name:    "queue heavy",
			stats:   WorkerStats{QueueDepth: 100},
			wantMin: 0.29,
			wantMax: 0.31,
		},
		{
			name:    "memory heavy",
			stats:   WorkerStats{MemoryUsedBytes: 15_000_000_000, MemoryTotalBytes: 16_000_000_000},
			wantMin: 0.18,
			wantMax: 0.20,
		},
		{
			name: "fully loaded",
			stats: WorkerStats{
				GPUUtilization:   1.0,
				QueueDepth:       200,
				MemoryUsedBytes:  16_000_000_000,
				MemoryTotalBytes: 16_000_000_000,
			},
			wantMin: 0.99,
			wantMax: 1.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			load := tt.stats.CurrentLoad()
			if load < tt.wantMin || load > tt.wantMax {
				t.Errorf("CurrentLoad() = %f, want [%f, %f]", load, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestWorkerStatsIsOverloaded(t *testing.T) {
	t.Run("not overloaded", func(t *testing.T) {
		s := &WorkerStats{GPUUtilization: 0.5}
		if s.IsOverloaded() {
			t.Error("should not be overloaded at 50% GPU")
		}
	})

	t.Run("overloaded by load", func(t *testing.T) {
		s := &WorkerStats{GPUUtilization: 1.0, QueueDepth: 200, MemoryUsedBytes: 16e9, MemoryTotalBytes: 16e9}
		if !s.IsOverloaded() {
			t.Error("should be overloaded")
		}
	})

	t.Run("overloaded by error rate", func(t *testing.T) {
		s := &WorkerStats{ErrorRate: 0.15}
		if !s.IsOverloaded() {
			t.Error("should be overloaded with high error rate")
		}
	})
}

func TestWorkerInfoHasModel(t *testing.T) {
	w := &WorkerInfo{
		LoadedModels: []LoadedModel{
			{ModelID: "llama-8b"},
			{ModelID: "mistral-7b"},
		},
	}

	if !w.HasModel("llama-8b") {
		t.Error("should have llama-8b")
	}
	if w.HasModel("nonexistent") {
		t.Error("should not have nonexistent")
	}
}

func TestWorkerInfoHasCapacity(t *testing.T) {
	t.Run("healthy with capacity", func(t *testing.T) {
		w := &WorkerInfo{Status: WorkerStatusHealthy, Stats: WorkerStats{GPUUtilization: 0.5}}
		if !w.HasCapacity() {
			t.Error("should have capacity")
		}
	})

	t.Run("unhealthy has no capacity", func(t *testing.T) {
		w := &WorkerInfo{Status: WorkerStatusUnhealthy, Stats: WorkerStats{GPUUtilization: 0.1}}
		if w.HasCapacity() {
			t.Error("unhealthy should not have capacity")
		}
	})

	t.Run("overloaded has no capacity", func(t *testing.T) {
		w := &WorkerInfo{Status: WorkerStatusHealthy, Stats: WorkerStats{ErrorRate: 0.2}}
		if w.HasCapacity() {
			t.Error("overloaded should not have capacity")
		}
	})
}

func TestWorkerInfoIsHealthy(t *testing.T) {
	tests := []struct {
		status WorkerStatus
		want   bool
	}{
		{WorkerStatusHealthy, true},
		{WorkerStatusDegraded, true},
		{WorkerStatusUnhealthy, false},
		{WorkerStatusDraining, false},
		{WorkerStatusOffline, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			w := &WorkerInfo{Status: tt.status}
			if got := w.IsHealthy(); got != tt.want {
				t.Errorf("IsHealthy() for %s = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestWorkerInfoUpdateStats(t *testing.T) {
	w := &WorkerInfo{Status: WorkerStatusHealthy}
	before := time.Now()
	w.UpdateStats(WorkerStats{GPUUtilization: 0.75})

	if w.Stats.GPUUtilization != 0.75 {
		t.Errorf("expected 0.75, got %f", w.Stats.GPUUtilization)
	}
	if w.LastHealthCheck.Before(before) {
		t.Error("LastHealthCheck should be updated")
	}
}

func TestWorkerInfoUpdateStatus(t *testing.T) {
	w := &WorkerInfo{Status: WorkerStatusHealthy}
	w.UpdateStatus(WorkerStatusDraining)
	if w.Status != WorkerStatusDraining {
		t.Errorf("expected draining, got %s", w.Status)
	}
}

func TestWorkerInfoClone(t *testing.T) {
	w := &WorkerInfo{
		WorkerID:     "w1",
		Address:      "localhost:8001",
		Status:       WorkerStatusHealthy,
		LoadedModels: []LoadedModel{{ModelID: "llama"}},
		Tags:         map[string]string{"gpu": "A100"},
	}

	clone := w.Clone()

	if clone.WorkerID != "w1" {
		t.Error("clone should have same worker ID")
	}

	// Verify deep copy
	clone.LoadedModels[0].ModelID = "modified"
	if w.LoadedModels[0].ModelID == "modified" {
		t.Error("modifying clone models should not affect original")
	}

	clone.Tags["gpu"] = "H100"
	if w.Tags["gpu"] == "H100" {
		t.Error("modifying clone tags should not affect original")
	}
}
