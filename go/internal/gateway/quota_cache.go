package gateway

import (
	"sync"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
)

const (
	defaultQuotaRecordCacheTTL = time.Minute
	defaultQuotaUsageCacheTTL  = time.Second
)

type quotaCache struct {
	mu       sync.RWMutex
	now      func() time.Time
	quotaTTL time.Duration
	usageTTL time.Duration
	quotas   map[string]cachedWorkspaceQuota
	usage    map[string]cachedWorkspaceUsage
}

type cachedWorkspaceQuota struct {
	value     *auth.WorkspaceQuotaRecord
	expiresAt time.Time
}

type cachedWorkspaceUsage struct {
	value     *audit.UsageSummary
	expiresAt time.Time
}

func newQuotaCache(quotaTTL, usageTTL time.Duration) *quotaCache {
	if quotaTTL <= 0 {
		quotaTTL = defaultQuotaRecordCacheTTL
	}
	if usageTTL <= 0 {
		usageTTL = defaultQuotaUsageCacheTTL
	}

	return &quotaCache{
		now:      time.Now,
		quotaTTL: quotaTTL,
		usageTTL: usageTTL,
		quotas:   make(map[string]cachedWorkspaceQuota),
		usage:    make(map[string]cachedWorkspaceUsage),
	}
}

func (c *quotaCache) getWorkspaceQuota(
	workspaceID string,
	loader func(string) (*auth.WorkspaceQuotaRecord, error),
) (*auth.WorkspaceQuotaRecord, error) {
	now := c.now()

	c.mu.RLock()
	entry, ok := c.quotas[workspaceID]
	c.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return cloneWorkspaceQuotaRecord(entry.value), nil
	}

	loaded, err := loader(workspaceID)
	if err != nil {
		return nil, err
	}

	cloned := cloneWorkspaceQuotaRecord(loaded)

	c.mu.Lock()
	c.quotas[workspaceID] = cachedWorkspaceQuota{
		value:     cloneWorkspaceQuotaRecord(cloned),
		expiresAt: now.Add(c.quotaTTL),
	}
	c.mu.Unlock()

	return cloned, nil
}

func (c *quotaCache) getWorkspaceUsageSummary(
	workspaceID string,
	periodStart time.Time,
	loader func(audit.UsageSummaryQuery) (*audit.UsageSummary, error),
) (*audit.UsageSummary, error) {
	now := c.now().UTC()
	cacheKey := workspaceUsageCacheKey(workspaceID, periodStart)

	c.mu.RLock()
	entry, ok := c.usage[cacheKey]
	c.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return cloneUsageSummary(entry.value), nil
	}

	loaded, err := loader(audit.UsageSummaryQuery{
		Start:       periodStart.UTC(),
		End:         now,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}

	cloned := cloneUsageSummary(loaded)

	c.mu.Lock()
	c.usage[cacheKey] = cachedWorkspaceUsage{
		value:     cloneUsageSummary(cloned),
		expiresAt: now.Add(c.usageTTL),
	}
	c.mu.Unlock()

	return cloned, nil
}

func workspaceUsageCacheKey(workspaceID string, periodStart time.Time) string {
	return workspaceID + ":" + periodStart.UTC().Format(time.RFC3339)
}

func cloneWorkspaceQuotaRecord(input *auth.WorkspaceQuotaRecord) *auth.WorkspaceQuotaRecord {
	if input == nil {
		return nil
	}

	out := *input
	if input.MonthlyRequestLimit != nil {
		limit := *input.MonthlyRequestLimit
		out.MonthlyRequestLimit = &limit
	}
	if input.MonthlyTokenLimit != nil {
		limit := *input.MonthlyTokenLimit
		out.MonthlyTokenLimit = &limit
	}
	return &out
}

func cloneUsageSummary(input *audit.UsageSummary) *audit.UsageSummary {
	if input == nil {
		return nil
	}

	out := *input
	return &out
}
