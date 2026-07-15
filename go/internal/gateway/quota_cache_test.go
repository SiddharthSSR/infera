package gateway

import (
	"testing"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
)

func TestQuotaCacheCachesWorkspaceQuotaWithinTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	cache := newQuotaCache(time.Minute, time.Second)
	cache.now = func() time.Time { return now }

	calls := 0
	limit := int64(120)
	quota, err := cache.getWorkspaceQuota("ws_123", func(workspaceID string) (*auth.WorkspaceQuotaRecord, error) {
		calls++
		return &auth.WorkspaceQuotaRecord{
			WorkspaceID:         workspaceID,
			MonthlyRequestLimit: &limit,
			EnforceHardLimits:   true,
			UpdatedAt:           now,
		}, nil
	})
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if quota == nil || quota.MonthlyRequestLimit == nil || *quota.MonthlyRequestLimit != 120 {
		t.Fatalf("unexpected quota: %+v", quota)
	}

	quota.MonthlyRequestLimit = nil

	second, err := cache.getWorkspaceQuota("ws_123", func(workspaceID string) (*auth.WorkspaceQuotaRecord, error) {
		calls++
		return nil, nil
	})
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one loader call, got %d", calls)
	}
	if second == nil || second.MonthlyRequestLimit == nil || *second.MonthlyRequestLimit != 120 {
		t.Fatalf("expected cached quota copy, got %+v", second)
	}
}

func TestQuotaCacheRefreshesUsageAfterTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	cache := newQuotaCache(time.Minute, time.Second)
	cache.now = func() time.Time { return now }

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	calls := 0

	first, err := cache.getWorkspaceUsageSummary("ws_123", monthStart, func(q audit.UsageSummaryQuery) (*audit.UsageSummary, error) {
		calls++
		return &audit.UsageSummary{RequestCount: 5, TokenCount: 100}, nil
	})
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if first == nil || first.RequestCount != 5 {
		t.Fatalf("unexpected first usage: %+v", first)
	}

	second, err := cache.getWorkspaceUsageSummary("ws_123", monthStart, func(q audit.UsageSummaryQuery) (*audit.UsageSummary, error) {
		calls++
		return &audit.UsageSummary{RequestCount: 999, TokenCount: 999}, nil
	})
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected cached usage for second read, got %d loader calls", calls)
	}
	if second == nil || second.RequestCount != 5 {
		t.Fatalf("expected cached usage summary, got %+v", second)
	}

	now = now.Add(2 * time.Second)

	third, err := cache.getWorkspaceUsageSummary("ws_123", monthStart, func(q audit.UsageSummaryQuery) (*audit.UsageSummary, error) {
		calls++
		return &audit.UsageSummary{RequestCount: 6, TokenCount: 120}, nil
	})
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected refreshed usage after TTL, got %d loader calls", calls)
	}
	if third == nil || third.RequestCount != 6 {
		t.Fatalf("expected refreshed usage summary, got %+v", third)
	}
}
