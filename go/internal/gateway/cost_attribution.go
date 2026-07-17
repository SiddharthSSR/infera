package gateway

import (
	"math"
	"math/big"
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
	snapshot, ok, err := g.instanceManager.GetPriceSnapshotForWorkerWithError(workerID)
	if err != nil || !ok {
		return audit.UnavailableCostAttribution()
	}
	concurrency := int64(1)
	if decision.WorkerActiveRequests != nil && *decision.WorkerActiveRequests > 0 {
		if uint64(*decision.WorkerActiveRequests) >= uint64(math.MaxInt64) {
			return audit.UnavailableCostAttribution()
		}
		concurrency += int64(*decision.WorkerActiveRequests)
	}
	elapsedMS := elapsed.Milliseconds()
	if elapsedMS < 0 {
		elapsedMS = 0
	}
	costNano, ok := attributedCostNano(snapshot.AmountNano, elapsedMS, concurrency)
	if !ok {
		return audit.UnavailableCostAttribution()
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

// attributedCostNano computes price*elapsed/(milliseconds/hour*concurrency)
// with integer half-up rounding. big.Int keeps malformed extremes from
// overflowing before the final checked int64 conversion.
func attributedCostNano(priceNano, elapsedMS, concurrency int64) (int64, bool) {
	if priceNano <= 0 || elapsedMS < 0 || concurrency <= 0 {
		return 0, false
	}
	numerator := new(big.Int).Mul(big.NewInt(priceNano), big.NewInt(elapsedMS))
	denominator := new(big.Int).Mul(big.NewInt(millisecondsPerHour), big.NewInt(concurrency))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}
