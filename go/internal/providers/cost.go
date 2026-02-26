package providers

import (
	"sync"
	"time"
)

// CostTracker tracks costs across all instances.
type CostTracker struct {
	entries   map[string]*CostEntry
	history   []CostRecord
	mu        sync.RWMutex
	startTime time.Time
}

// CostEntry tracks cost for a single instance.
type CostEntry struct {
	InstanceID  string
	Provider    ProviderType
	GPUType     GPUType
	CostPerHour float64
	StartTime   time.Time
	StopTime    *time.Time
	Accumulated float64
}

// CostRecord is a historical cost record.
type CostRecord struct {
	Date       string  `json:"date"`
	Provider   string  `json:"provider"`
	InstanceID string  `json:"instance_id"`
	GPUType    string  `json:"gpu_type"`
	Hours      float64 `json:"hours"`
	Cost       float64 `json:"cost"`
}

// NewCostTracker creates a new cost tracker.
func NewCostTracker() *CostTracker {
	return &CostTracker{
		entries:   make(map[string]*CostEntry),
		history:   make([]CostRecord, 0),
		startTime: time.Now(),
	}
}

// StartTracking begins tracking costs for an instance.
func (ct *CostTracker) StartTracking(instance *Instance) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.entries[instance.ID] = &CostEntry{
		InstanceID:  instance.ID,
		Provider:    instance.Provider,
		GPUType:     instance.GPUType,
		CostPerHour: instance.CostPerHour,
		StartTime:   time.Now(),
	}
}

// StopTracking stops tracking costs for an instance.
func (ct *CostTracker) StopTracking(instanceID string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	entry, exists := ct.entries[instanceID]
	if !exists {
		return
	}

	now := time.Now()
	entry.StopTime = &now

	hours := now.Sub(entry.StartTime).Hours()
	entry.Accumulated = hours * entry.CostPerHour

	ct.history = append(ct.history, CostRecord{
		Date:       now.Format("2006-01-02"),
		Provider:   string(entry.Provider),
		InstanceID: entry.InstanceID,
		GPUType:    string(entry.GPUType),
		Hours:      hours,
		Cost:       entry.Accumulated,
	})
}

// GetSummary returns current cost summary.
func (ct *CostTracker) GetSummary() *CostSummary {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	now := time.Now()
	today := now.Format("2006-01-02")
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	summary := &CostSummary{
		ByProvider: make(map[string]float64),
		ByGPU:      make(map[string]float64),
	}

	// Calculate from active instances
	for _, entry := range ct.entries {
		if entry.StopTime == nil {
			summary.CurrentHourly += entry.CostPerHour
			summary.ByProvider[string(entry.Provider)] += entry.CostPerHour
			summary.ByGPU[string(entry.GPUType)] += entry.CostPerHour

			hours := now.Sub(entry.StartTime).Hours()
			cost := hours * entry.CostPerHour

			if entry.StartTime.Format("2006-01-02") == today {
				summary.TodayTotal += cost
			}
			if entry.StartTime.After(monthStart) {
				summary.MonthTotal += cost
			}
		}
	}

	// Add historical records
	for _, record := range ct.history {
		if record.Date == today {
			summary.TodayTotal += record.Cost
		}
		recordDate, _ := time.Parse("2006-01-02", record.Date)
		if recordDate.After(monthStart) {
			summary.MonthTotal += record.Cost
		}
	}

	// Project monthly cost
	dayOfMonth := float64(now.Day())
	daysInMonth := float64(time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day())
	if dayOfMonth > 0 {
		summary.ProjectedMonth = (summary.MonthTotal / dayOfMonth) * daysInMonth
	}

	return summary
}
