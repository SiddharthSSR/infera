package gateway

import (
	"context"
	"time"

	"github.com/infera/infera/go/internal/audit"
)

const auditWriteTimeout = 2 * time.Second

// auditUsageStore isolates the subset of audit-store behavior the gateway uses
// for async event writes and usage queries.
type auditUsageStore interface {
	AppendInference(rec audit.InferenceAuditRecord) error
	ReserveQuota(res audit.QuotaReservation) error
	UsageSummary(q audit.UsageSummaryQuery) (*audit.UsageSummary, error)
	UsageByKey(q audit.UsageQuery) ([]audit.UsageRow, error)
}

type auditWriteRequest struct {
	record audit.InferenceAuditRecord
	done   chan error
}

func (g *Gateway) enqueueAuditRecord(record audit.InferenceAuditRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	return g.enqueueAuditRecordWithContext(ctx, record)
}

func (g *Gateway) enqueueAuditRecordWithContext(ctx context.Context, record audit.InferenceAuditRecord) error {
	if g.auditCh == nil || g.auditStore == nil {
		return nil
	}
	done := make(chan error, 1)
	select {
	case g.auditCh <- auditWriteRequest{record: record, done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Gateway) appendAuditRecordWithRetry(record audit.InferenceAuditRecord) error {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = g.auditStore.AppendInference(record)
		if err == nil {
			return nil
		}
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt*25) * time.Millisecond)
		}
	}
	return err
}

type usageReconciliation struct {
	Status        string   `json:"status"`
	Discrepancies []string `json:"discrepancies"`
}

func reconcileUsageRows(rows []audit.UsageRow) usageReconciliation {
	var attempts, requests, tokens int64
	var successes, errors int64
	var exactRequests, estimatedRequests int64
	var exactTokens, estimatedTokens int64
	var exactCosts, estimatedCosts, unavailableCosts int64
	for _, row := range rows {
		attempts += row.AttemptCount
		requests += row.RequestCount
		tokens += row.TokenCount
		successes += row.SuccessCount
		errors += row.ErrorCount
		exactRequests += row.ExactRequestCount
		estimatedRequests += row.EstimatedRequestCount
		exactTokens += row.ExactTokenCount
		estimatedTokens += row.EstimatedTokenCount
		exactCosts += row.ExactCostCount
		estimatedCosts += row.EstimatedCostCount
		unavailableCosts += row.UnavailableCostCount
	}

	discrepancies := make([]string, 0, 3)
	if attempts != successes+errors {
		discrepancies = append(discrepancies, "attempt_status_mismatch")
	}
	if requests != exactRequests+estimatedRequests {
		discrepancies = append(discrepancies, "request_accuracy_mismatch")
	}
	if tokens != exactTokens+estimatedTokens {
		discrepancies = append(discrepancies, "token_accuracy_mismatch")
	}
	// Zero means an older/stub summary did not populate cost metadata. Durable
	// ledger queries always classify every row, including unavailable prices.
	classifiedCosts := exactCosts + estimatedCosts + unavailableCosts
	if classifiedCosts != 0 && attempts != classifiedCosts {
		discrepancies = append(discrepancies, "cost_accuracy_mismatch")
	}
	status := "ok"
	if len(discrepancies) > 0 {
		status = "mismatch"
	}
	return usageReconciliation{Status: status, Discrepancies: discrepancies}
}

func reconcileUsageSummary(summary audit.UsageSummary) usageReconciliation {
	return reconcileUsageRows([]audit.UsageRow{{
		AttemptCount:          summary.AttemptCount,
		RequestCount:          summary.RequestCount,
		TokenCount:            summary.TokenCount,
		ExactRequestCount:     summary.ExactRequestCount,
		EstimatedRequestCount: summary.EstimatedRequestCount,
		ExactTokenCount:       summary.ExactTokenCount,
		EstimatedTokenCount:   summary.EstimatedTokenCount,
		SuccessCount:          summary.SuccessCount,
		ErrorCount:            summary.ErrorCount,
		ExactCostCount:        summary.ExactCostCount,
		EstimatedCostCount:    summary.EstimatedCostCount,
		UnavailableCostCount:  summary.UnavailableCostCount,
	}})
}

type costMetrics struct {
	Currency             string  `json:"currency"`
	CostUSD              float64 `json:"cost_usd"`
	CostPerRequestUSD    float64 `json:"cost_per_request_usd"`
	CostPerTokenUSD      float64 `json:"cost_per_token_usd"`
	CostPerMillionTokens float64 `json:"cost_per_1m_tokens_usd"`
	CostedRequests       int64   `json:"costed_requests"`
	CostedTokens         int64   `json:"costed_tokens"`
	ExactRequests        int64   `json:"exact_requests"`
	EstimatedRequests    int64   `json:"estimated_requests"`
	UnavailableRequests  int64   `json:"unavailable_requests"`
}

func buildCostMetrics(costNano, costedTokens, exact, estimated, unavailable int64) costMetrics {
	costedRequests := exact + estimated
	costUSD := float64(costNano) / 1_000_000_000
	metrics := costMetrics{
		Currency: "USD", CostUSD: costUSD, CostedRequests: costedRequests,
		CostedTokens: costedTokens, ExactRequests: exact,
		EstimatedRequests: estimated, UnavailableRequests: unavailable,
	}
	if costedRequests > 0 {
		metrics.CostPerRequestUSD = costUSD / float64(costedRequests)
	}
	if costedTokens > 0 {
		metrics.CostPerTokenUSD = costUSD / float64(costedTokens)
		metrics.CostPerMillionTokens = metrics.CostPerTokenUSD * 1_000_000
	}
	return metrics
}
