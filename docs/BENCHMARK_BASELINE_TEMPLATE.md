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
- Warm or cold run:

## Command

```bash
python3 scripts/benchmark-chat.py \
  https://your-gateway.example.com \
  --api-key "$INFERA_SMOKE_API_KEY" \
  --model "your/model-id" \
  --runs 3 \
  --cost-per-hour 0.34 \
  --json-output /tmp/infera-benchmark.json
```

## Manual Cold-Start Measurement

Record this separately for a fresh pod:

1. Trigger provision or warm restart.
2. Note the timestamp when the request is sent.
3. Note the timestamp when the first successful chat completion returns.
4. Record `cold start to ready`.

## Results Table

| Scenario | TTFT p50 | TTFT p95 | Stream total p50 | Non-stream total p50 | Decode tok/s p50 | Cost/query | Notes |
|---|---:|---:|---:|---:|---:|---:|---|
| Short prompt |  |  |  |  |  |  |  |
| Medium prompt |  |  |  |  |  |  |  |
| Long prompt |  |  |  |  |  |  |  |

## Required Notes

- Was the pod freshly provisioned, resumed, or already warm?
- Was the model loaded from remote download or cache?
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
