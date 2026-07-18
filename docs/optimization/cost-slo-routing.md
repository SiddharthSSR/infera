# Cost Under Latency SLO Routing

`min_cost_under_latency_slo` is Infera's first evidence-aware cost routing
strategy. It chooses the lowest hourly-price candidate among healthy workers
whose fresh measured p99 latency is within a configured threshold.

This is intentionally narrower than cost-per-token optimization. Infera does
not guess per-request token counts, throughput, amortization, or marginal cost
to make this routing decision. The reliable comparable signal currently
available before dispatch is the provisioned instance's USD/hour price.

## Evidence contract

A candidate is cost/SLO eligible only when all of these are true:

- the worker is healthy, has capacity, and serves the requested model;
- p99 latency is positive, finite, no older than the configured evidence age,
  and not implausibly far in the future;
- p99 latency is less than or equal to the configured SLO;
- the gateway can resolve a positive price snapshot from the authoritative
  worker-to-instance binding;
- the snapshot uses `provider-instance-hourly-v1`, `USD`, and `hour`.

Worker tags never supply price evidence. Unknown snapshot versions, currencies,
or time units fail closed as unavailable evidence.

Among eligible candidates, the strategy chooses the lowest nano-USD/hour
amount. Equal prices are resolved deterministically by lower p99 latency, lower
load, then worker ID.

## Availability fallback

Missing, stale, or temporarily unavailable evidence does not take inference
offline. If no candidate has both trusted cost and qualifying latency evidence,
the strategy falls back to the existing least-loaded selector. The route
decision retains `min_cost_under_latency_slo` as the configured strategy and
records the bounded fallback reason
`no_candidate_with_trusted_cost_and_fresh_latency_under_slo`.

Affinity remains authoritative for an existing valid sticky binding. A route
served through affinity records `affinity`, matching the existing routing
contract.

## Configuration

```dotenv
INFERA_ROUTING_STRATEGY=min_cost_under_latency_slo
INFERA_ROUTING_LATENCY_SLO_MS=2000
INFERA_ROUTING_EVIDENCE_MAX_AGE=2m
```

The default strategy remains `least_loaded`. Invalid strategy names, non-finite
or non-positive SLOs, and invalid evidence ages prevent gateway startup.

## Decision evidence

Structured route logs and opt-in safe route metadata include:

- configured latency SLO;
- selected nano-USD/hour price when cost/SLO selection succeeds;
- number of cost/SLO-eligible candidates;
- fallback reason when the evidence-aware selection cannot run.

They do not expose provider credentials, instance secrets, prompts, API keys,
or the internal price-snapshot version. Durable request-level cost and
cost-per-token accuracy remain governed by `docs/COST_ATTRIBUTION.md`.
