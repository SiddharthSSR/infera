# Infera Benchmark Baseline Template

Use this worksheet before and after each performance slice.

## Setup

- Branch:
- Commit:
- Base URL:
- Model:
- GPU / provider:
- Hourly cost used for estimates:
- Worker image:
- Run condition: cold / warm / resumed
- Warmup groups:
- Measured concurrency:
- Cache reuse mode: none / affinity
- Affinity key prefix:

## Command

```bash
python3 scripts/benchmark-chat.py \
  https://your-gateway.example.com \
  --api-key "$INFERA_SMOKE_API_KEY" \
  --model "your/model-id" \
  --preset conversation \
  --runs 3 \
  --warmup 2 \
  --concurrency 4 \
  --cache-reuse-mode affinity \
  --cache-key-prefix baseline \
  --cost-per-hour 0.34 \
  --json-output /tmp/infera-benchmark.json
```

## Manual Cold-Start Measurement

Follow [`docs/COLD_START_BENCHMARK_WORKFLOW.md`](/Users/siddharthsingh/codingtensor/infera/docs/COLD_START_BENCHMARK_WORKFLOW.md) and record this separately for each scenario:

1. `fresh_provision`
2. `stopped_instance_start`
3. `stopped_instance_reuse`

Capture:

- `T0 request_sent`
- `T1 instance_running`
- `T2 worker_registered`
- `T3 first_successful_completion`

## Results Table

| Scenario | TTFT p50 | TTFT p95 | TTFT p99 | Stream total p50 | Non-stream total p50 | Decode tok/s p50 | Aggregate decode tok/s p50 | Contention ratio p50 | Cost/query | Notes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Short prompt, cold |  |  |  |  |  |  |  |  |  |  |
| Short prompt, warmed |  |  |  |  |  |  |  |  |  |  |
| Conversation, no reuse |  |  |  |  |  |  |  |  |  |  |
| Conversation, affinity reuse |  |  |  |  |  |  |  |  |  |  |
| Long prompt, affinity reuse |  |  |  |  |  |  |  |  |  |  |

## Required Notes

- Was the pod freshly provisioned, resumed, or already warm?
- Was the model loaded from remote download or cache?
- Did you discard warmup groups before recording the measured runs?
- Was `--cache-reuse-mode` set to `none` or `affinity`?
- If affinity reuse was enabled, was the same affinity key prefix reused across all measured groups?
- Were there any retries or provider-side delays?
- Which image tag or digest was used?

## Alert Tuning

After writing benchmark JSON, generate suggested alert thresholds:

```bash
python3 scripts/suggest-alert-thresholds.py /tmp/infera-benchmark.json
```

Use that output to tune:

- `InferaInferenceTTFTHigh`
- `InferaInferenceTPOTHigh`
- `InferaBatchWaitHigh`

The helper also prints a model-specific Prometheus rule block that you can copy into the alert rules file when you want tighter thresholds for one model instead of a shared fleet-wide default.

Treat the script output as a starting point, not an automatic truth. If the workload is bursty or your warm-pool behavior is still changing, use a little more headroom before paging on-call.
