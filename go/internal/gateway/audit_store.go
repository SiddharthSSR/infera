package gateway

import (
	"time"

	"github.com/infera/infera/go/internal/audit"
)

// auditUsageStore isolates the subset of audit-store behavior the gateway uses
// for async event writes and usage queries.
type auditUsageStore interface {
	AppendInference(rec audit.InferenceAuditRecord) error
	UsageSummary(q audit.UsageSummaryQuery) (*audit.UsageSummary, error)
	UsageByKey(q audit.UsageQuery) ([]audit.UsageRow, error)
}

type auditWriteRequest struct {
	record audit.InferenceAuditRecord
	done   chan error
}

func (g *Gateway) enqueueAuditRecord(record audit.InferenceAuditRecord) error {
	if g.auditCh == nil || g.auditStore == nil {
		return nil
	}
	done := make(chan error, 1)
	g.auditCh <- auditWriteRequest{record: record, done: done}
	return <-done
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
