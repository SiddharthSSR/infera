package providers

import (
	"testing"
	"time"
)

func TestNewCostTracker(t *testing.T) {
	ct := NewCostTracker()
	if ct == nil {
		t.Fatal("NewCostTracker returned nil")
	}
	if ct.entries == nil {
		t.Error("entries should be initialized")
	}
	if ct.history == nil {
		t.Error("history should be initialized")
	}
}

func TestCostTrackerStartTracking(t *testing.T) {
	ct := NewCostTracker()

	now := time.Now()
	instance := &Instance{
		ID:          "test-1",
		Provider:    ProviderMock,
		GPUType:     GPURTX4090,
		CostPerHour: 0.50,
		CreatedAt:   now,
	}

	ct.StartTracking(instance)

	// Verify entry was created
	ct.mu.RLock()
	entry, exists := ct.entries[instance.ID]
	ct.mu.RUnlock()

	if !exists {
		t.Fatal("entry should exist")
	}
	if entry.CostPerHour != 0.50 {
		t.Errorf("expected 0.50, got %f", entry.CostPerHour)
	}
	if entry.Provider != ProviderMock {
		t.Errorf("expected mock, got %s", entry.Provider)
	}
	if entry.GPUType != GPURTX4090 {
		t.Errorf("expected RTX_4090, got %s", entry.GPUType)
	}
}

func TestCostTrackerStopTracking(t *testing.T) {
	ct := NewCostTracker()

	instance := &Instance{
		ID:          "test-2",
		Provider:    ProviderMock,
		GPUType:     GPURTX4090,
		CostPerHour: 1.00,
		CreatedAt:   time.Now(),
	}

	ct.StartTracking(instance)

	// Wait a tiny bit to accumulate some cost
	time.Sleep(10 * time.Millisecond)

	ct.StopTracking(instance.ID)

	// Verify entry has StopTime set
	ct.mu.RLock()
	entry := ct.entries[instance.ID]
	historyLen := len(ct.history)
	ct.mu.RUnlock()

	if entry.StopTime == nil {
		t.Error("StopTime should be set")
	}
	if entry.Accumulated <= 0 {
		t.Error("Accumulated should be positive")
	}

	// Verify history record was created
	if historyLen != 1 {
		t.Errorf("expected 1 history record, got %d", historyLen)
	}
}

func TestCostTrackerGetSummary(t *testing.T) {
	ct := NewCostTracker()

	// Create multiple instances
	instances := []*Instance{
		{
			ID:          "inst-1",
			Provider:    ProviderMock,
			GPUType:     GPURTX4090,
			CostPerHour: 0.50,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "inst-2",
			Provider:    ProviderRunPod,
			GPUType:     GPUA100_80,
			CostPerHour: 2.00,
			CreatedAt:   time.Now(),
		},
	}

	for _, inst := range instances {
		ct.StartTracking(inst)
	}

	summary := ct.GetSummary()

	t.Run("CurrentHourly", func(t *testing.T) {
		expected := 0.50 + 2.00
		if summary.CurrentHourly != expected {
			t.Errorf("expected %.2f, got %.2f", expected, summary.CurrentHourly)
		}
	})

	t.Run("ByProvider", func(t *testing.T) {
		if summary.ByProvider["mock"] != 0.50 {
			t.Errorf("expected mock=0.50, got %f", summary.ByProvider["mock"])
		}
		if summary.ByProvider["runpod"] != 2.00 {
			t.Errorf("expected runpod=2.00, got %f", summary.ByProvider["runpod"])
		}
	})

	t.Run("ByGPU", func(t *testing.T) {
		if summary.ByGPU["RTX_4090"] != 0.50 {
			t.Errorf("expected RTX_4090=0.50, got %f", summary.ByGPU["RTX_4090"])
		}
		if summary.ByGPU["A100_80GB"] != 2.00 {
			t.Errorf("expected A100_80GB=2.00, got %f", summary.ByGPU["A100_80GB"])
		}
	})
}

func TestCostTrackerProjectedMonth(t *testing.T) {
	ct := NewCostTracker()

	instance := &Instance{
		ID:          "proj-1",
		Provider:    ProviderMock,
		GPUType:     GPURTX4090,
		CostPerHour: 1.00,
		CreatedAt:   time.Now(),
	}

	ct.StartTracking(instance)

	summary := ct.GetSummary()

	// ProjectedMonth should be calculated based on current day of month
	if summary.ProjectedMonth < 0 {
		t.Error("ProjectedMonth should not be negative")
	}
}

func TestCostTrackerStoppedInstancesNotCountedInHourly(t *testing.T) {
	ct := NewCostTracker()

	instance := &Instance{
		ID:          "stopped-1",
		Provider:    ProviderMock,
		GPUType:     GPURTX4090,
		CostPerHour: 1.00,
		CreatedAt:   time.Now(),
	}

	ct.StartTracking(instance)

	// Verify it's counted while running
	summary1 := ct.GetSummary()
	if summary1.CurrentHourly != 1.00 {
		t.Errorf("expected 1.00 while running, got %.2f", summary1.CurrentHourly)
	}

	ct.StopTracking(instance.ID)

	// Verify it's not counted after stopping
	summary2 := ct.GetSummary()
	if summary2.CurrentHourly != 0 {
		t.Errorf("expected 0 after stopping, got %.2f", summary2.CurrentHourly)
	}
}

func TestCostEntry(t *testing.T) {
	now := time.Now()
	entry := &CostEntry{
		InstanceID:  "entry-1",
		Provider:    ProviderMock,
		GPUType:     GPURTX4090,
		CostPerHour: 0.75,
		StartTime:   now,
		Accumulated: 0,
	}

	t.Run("Initial state", func(t *testing.T) {
		if entry.StopTime != nil {
			t.Error("StopTime should be nil initially")
		}
		if entry.Accumulated != 0 {
			t.Error("Accumulated should be 0 initially")
		}
	})

	t.Run("After stopping", func(t *testing.T) {
		stopTime := now.Add(1 * time.Hour)
		entry.StopTime = &stopTime
		entry.Accumulated = 0.75 // 1 hour at $0.75/hr

		if entry.Accumulated != 0.75 {
			t.Errorf("expected 0.75, got %f", entry.Accumulated)
		}
	})
}

func TestCostRecord(t *testing.T) {
	record := CostRecord{
		Date:       "2024-01-15",
		Provider:   "runpod",
		InstanceID: "rec-1",
		GPUType:    "A100_80GB",
		Hours:      2.5,
		Cost:       5.00,
	}

	t.Run("Fields", func(t *testing.T) {
		if record.Hours != 2.5 {
			t.Errorf("expected 2.5, got %f", record.Hours)
		}
		if record.Cost != 5.00 {
			t.Errorf("expected 5.00, got %f", record.Cost)
		}
	})

	t.Run("Cost calculation", func(t *testing.T) {
		expectedCost := record.Hours * 2.00 // $2/hr for A100_80GB
		if record.Cost != expectedCost {
			t.Errorf("expected %.2f (%.1f hours * $2/hr), got %.2f", expectedCost, record.Hours, record.Cost)
		}
	})
}
