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
	}})
}
