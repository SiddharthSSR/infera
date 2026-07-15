package gateway

import "github.com/infera/infera/go/internal/audit"

// auditUsageStore isolates the subset of audit-store behavior the gateway uses
// for async event writes and usage queries.
type auditUsageStore interface {
	AppendInference(rec audit.InferenceAuditRecord) error
	UsageSummary(q audit.UsageSummaryQuery) (*audit.UsageSummary, error)
	UsageByKey(q audit.UsageQuery) ([]audit.UsageRow, error)
}
