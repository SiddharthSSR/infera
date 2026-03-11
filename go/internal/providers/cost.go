package providers

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/infera/infera/go/internal/migrate"
)

// costMigrations defines the schema for the cost database.
var costMigrations = []migrate.Migration{
	{
		Version:     1,
		Description: "create cost_records table",
		SQL: `
		CREATE TABLE IF NOT EXISTS cost_records (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			date        TEXT NOT NULL,
			provider    TEXT NOT NULL,
			instance_id TEXT NOT NULL,
			gpu_type    TEXT NOT NULL DEFAULT '',
			hours       REAL NOT NULL DEFAULT 0,
			cost        REAL NOT NULL DEFAULT 0,
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_cost_records_date ON cost_records(date);`,
	},
}

// CostTracker tracks costs across all instances.
type CostTracker struct {
	entries   map[string]*CostEntry
	history   []CostRecord
	mu        sync.RWMutex
	startTime time.Time
	db        *sql.DB // optional: persists history across restarts
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

// NewCostTracker creates a new in-memory-only cost tracker.
func NewCostTracker() *CostTracker {
	return &CostTracker{
		entries:   make(map[string]*CostEntry),
		history:   make([]CostRecord, 0),
		startTime: time.Now(),
	}
}

// NewPersistentCostTracker creates a cost tracker backed by SQLite.
// It runs migrations and loads existing history from the database.
func NewPersistentCostTracker(dbPath string) (*CostTracker, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate.Run(db, costMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}

	ct := &CostTracker{
		entries:   make(map[string]*CostEntry),
		history:   make([]CostRecord, 0),
		startTime: time.Now(),
		db:        db,
	}

	if err := ct.loadHistory(); err != nil {
		slog.Warn("cost_tracker: failed to load history", slog.String("error", err.Error()))
	}

	return ct, nil
}

// Close closes the underlying database if persistent.
func (ct *CostTracker) Close() error {
	if ct.db != nil {
		return ct.db.Close()
	}
	return nil
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

	record := CostRecord{
		Date:       now.Format("2006-01-02"),
		Provider:   string(entry.Provider),
		InstanceID: entry.InstanceID,
		GPUType:    string(entry.GPUType),
		Hours:      hours,
		Cost:       entry.Accumulated,
	}

	ct.history = append(ct.history, record)

	// Persist to database if available
	if ct.db != nil {
		if err := ct.persistRecord(record); err != nil {
			slog.Warn("cost_tracker: failed to persist record",
				slog.String("instance_id", record.InstanceID),
				slog.String("error", err.Error()),
			)
		}
	}
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

func (ct *CostTracker) persistRecord(record CostRecord) error {
	_, err := ct.db.Exec(
		"INSERT INTO cost_records (date, provider, instance_id, gpu_type, hours, cost) VALUES (?, ?, ?, ?, ?, ?)",
		record.Date, record.Provider, record.InstanceID, record.GPUType, record.Hours, record.Cost,
	)
	return err
}

func (ct *CostTracker) loadHistory() error {
	// Load records from the current month only (older records are not needed for summaries)
	monthStart := time.Now().Format("2006-01") + "-01"

	rows, err := ct.db.Query(
		"SELECT date, provider, instance_id, gpu_type, hours, cost FROM cost_records WHERE date >= ? ORDER BY id ASC",
		monthStart,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var r CostRecord
		if err := rows.Scan(&r.Date, &r.Provider, &r.InstanceID, &r.GPUType, &r.Hours, &r.Cost); err != nil {
			return err
		}
		ct.history = append(ct.history, r)
	}
	return rows.Err()
}
