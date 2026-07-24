# INF-49 production cost/SLO routing proof

Date: 2026-07-18 UTC

## Result

`min_cost_under_latency_slo` passed a bounded production proof on release
`main-2d2a021-inf49`. With two healthy workers serving the same model and fresh
p99 telemetry, the router selected the lowest trusted hourly cost under a
60 ms SLO. With a 56.637 ms SLO, it excluded the cheaper worker whose known p99
was above the SLO and selected the remaining eligible worker.

Routing was restored to `least_loaded` after the proof. All proof workers were
terminated, the gateway worker registry returned to zero, and RunPod reported
zero active pods and zero active hourly spend.

## Release and invariants

- Source commit: `2d2a0217467449fd93afa13022e70b23ebc7f915`
- Source tree: `7bad1b024bd415c67ceef4512830df8fdd7178b4`
- Image: `codingtensor/infera-gateway:main-2d2a021`
- Registry digest: `sha256:13701dcc055dfbae40242a4811c374f48bc06a1ec5df9c79fe5d3c2ecabbad0b`
- Target: Linux/amd64, non-root `gateway` user
- Build bases:
  - `golang:1.22-alpine3.19@sha256:6f73a1b8b608dad4866b9f746ac6888ffdb112f75ef59ed97c43b5f734368718`
  - `alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1`
- Two gateway replicas remained on protocol `1` with the same release ID,
  shared PostgreSQL control state, PostgreSQL audit ledger, provider credential
  encryption key, and worker authentication contract.
- No secret, provider credential, DSN, worker address, prompt content, or model
  output is included in this report.

## Bounded proof environment

Both workers used RunPod, vLLM, and `Qwen/Qwen2.5-0.5B-Instruct`:

| Worker shape | Gateway price snapshot | Fresh p99 before proof |
| --- | ---: | ---: |
| 1x A100 PCIe 80 GB | $1.19/hour | 56.648 ms |
| 2x A100 PCIe 80 GB | $2.38/hour | 56.626 ms |

The environment handled five successful gateway requests, each capped at one
completion token. The final ledger window contained 160 total tokens, no
errors, and $0.002733695 attributed cost.

## Routing evidence

### Known over-SLO candidate excluded from fallback

At a 50 ms SLO, the 1x worker had known p99 of 56.648 ms and the new 2x worker
did not yet have latency evidence. The request succeeded on the 2x worker with:

- strategy: `min_cost_under_latency_slo`
- candidates evaluated: `2`
- cost/SLO eligible candidates: `0`
- selected worker shape: 2x A100 PCIe
- fallback reason: `no_candidate_with_trusted_cost_and_fresh_latency_under_slo`

The known over-SLO 1x worker did not enter the least-loaded fallback.

### Cheapest eligible worker selected

At a 60 ms SLO, both workers had fresh p99 telemetry and trusted positive USD/hour
snapshots. The request succeeded with:

- strategy: `min_cost_under_latency_slo`
- candidates evaluated: `2`
- cost/SLO eligible candidates: `2`
- selected p99: `56.648 ms`
- selected cost: `1,190,000,000` nano-USD/hour
- result: the 1x worker at $1.19/hour was selected over the 2x worker at
  $2.38/hour

### Known over-SLO candidate excluded from normal cost selection

At a 56.637 ms midpoint SLO, the cheaper 1x worker's 56.648 ms p99 was over the
SLO while the 2x worker's 56.626 ms p99 remained within it. The request
succeeded with one cost/SLO-eligible candidate and selected the 2x worker at
`2,380,000,000` nano-USD/hour. No fallback was used.

The opt-in `X-Infera-Route-Decision` metadata contained only bounded routing
fields: request/model identifiers, strategy, selected worker/provider, queue
and latency measurements, SLO, selected hourly cost, eligible count, reason,
and timestamp. It contained no credentials, instance IDs, prompts, model
outputs, or internal price-snapshot version.

## Audit and cost reconciliation

For the 60 ms cheapest-worker request, the PostgreSQL audit window changed as
follows:

| Metric | Before | After | Delta |
| --- | ---: | ---: | ---: |
| Attempts | 3 | 4 | 1 |
| Requests | 3 | 4 | 1 |
| Successes | 3 | 4 | 1 |
| Tokens | 96 | 128 | 32 |
| Attributed cost | $0.001725831 | $0.002056056 | $0.000330225 |

Reconciliation status was `ok` with no discrepancies and no unavailable-cost
rows. All five proof requests were classified as estimated-cost requests,
which matches the current instance-time attribution method.

## Provider findings

RunPod capacity and pricing metadata were not stable enough to treat every
offering response as a placement or spend guarantee:

- RTX 4090, RTX 4080, RTX A5000, and A100 SXM placement attempts failed before
  creating a pod despite positive advertised capacity.
- L40S, H100 NVL, and H100 PCIe pods were terminated when they did not receive
  usable runtime capacity within the bounded proof workflow.
- `max_cost_hour` is not enforced by the RunPod adapter.
- Offering price, stored gateway snapshot, and RunPod pod price diverged on
  observed shapes. For example, the 1x A100 offering and gateway snapshot were
  $1.19/hour while the provider pod status reported $1.39/hour. H100 NVL showed
  $2.59/hour in offerings, $1.99/hour in the gateway record, and $3.19/hour on
  the provider pod.

The routing algorithm followed its stored v1 USD/hour evidence correctly, but
the provider adapter must reconcile live placement price before that evidence
can be considered economically authoritative.

## Cleanup evidence

- Routing on both gateway replicas: `least_loaded`
- Restored defaults: 2000 ms SLO, 2 minute evidence age
- Gateway replicas: 2 healthy
- Registered workers: 0
- Active gateway instances: 0
- Active RunPod pods: 0
- Active RunPod hourly spend: $0
- Proof pod names remaining at provider: none
- Existing production DigitalOcean host: unchanged
