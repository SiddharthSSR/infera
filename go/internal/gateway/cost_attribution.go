package gateway

import (
	"math"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/pkg/types"
)

const millisecondsPerHour int64 = 60 * 60 * 1000

// requestCostAttribution snapshots the selected instance price and estimates
// this execution's share of active instance time. WorkerActiveRequests is the
// router's pre-dispatch observation, so the current execution adds one.
func (g *Gateway) requestCostAttribution(workerID string, decision types.RoutingDecision, elapsed time.Duration) audit.CostAttribution {
	if g.instanceManager == nil || workerID == "" {
		return audit.UnavailableCostAttribution()
	}
	snapshot, ok := g.instanceManager.GetPriceSnapshotForWorker(workerID)
	if !ok {
		return audit.UnavailableCostAttribution()
	}
	concurrency := int64(1)
	if decision.WorkerActiveRequests != nil && *decision.WorkerActiveRequests > 0 {
		concurrency += int64(*decision.WorkerActiveRequests)
	}
	elapsedMS := elapsed.Milliseconds()
	if elapsedMS < 0 {
		elapsedMS = 0
	}
	costNano := int64(math.Round(float64(snapshot.AmountNano) * float64(elapsedMS) / float64(millisecondsPerHour) / float64(concurrency)))
	if costNano < 0 {
		costNano = 0
	}
	return audit.CostAttribution{
		Provider:                  string(snapshot.Provider),
		InstanceID:                snapshot.InstanceID,
		PriceSnapshotVersion:      snapshot.Version,
		PriceAmountNano:           snapshot.AmountNano,
		PriceCurrency:             snapshot.Currency,
		PriceTimeUnit:             snapshot.TimeUnit,
		PriceCapturedAt:           snapshot.CapturedAt,
		CostNano:                  costNano,
		CostAccuracy:              audit.CostAccuracyEstimated,
		CostAttributionMethod:     audit.CostMethodActiveInstanceTimeShareV1,
		ObservedActiveConcurrency: concurrency,
	}
}
