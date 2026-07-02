# Infera Benchmarks

`infera-bench` is a small measurement CLI for OpenAI-compatible Infera chat completion endpoints. The MVP sends requests to `/v1/chat/completions` and writes JSON and Markdown reports with latency, throughput, success/error counts, and streaming TTFT/TPOT estimates.

The tool does not change routing behavior, does not provision workers, and does not modify production deployment files.

## Build

From the repository root:

```bash
cd go
go build ./cmd/infera-bench
```

Or run directly:

```bash
cd go
go run ./cmd/infera-bench --help
```

## Local Gateway Example

Use a small request count first:

```bash
cd go
go run ./cmd/infera-bench \
  --base-url http://127.0.0.1:8080 \
  --api-key-file ../.secrets/local-api-key.txt \
  --model Qwen/Qwen2.5-7B-Instruct \
  --workload ../bench/workloads/short_chat.yaml \
  --concurrency 1 \
  --requests 3 \
  --warmup 1 \
  --timeout 60s \
  --out-json ../bench/results/local-short.json \
  --out-md ../bench/results/local-short.md
```

## Production Example

Production benchmarks use live GPU capacity and can incur provider cost. Start with `--concurrency 1`, a small `--requests` value, and a short workload. Increase concurrency only after checking worker health and cost expectations.

```bash
cd go
go run ./cmd/infera-bench \
  --base-url https://inferai.co.in \
  --api-key-file ../.secrets/prod-smoke-key.txt \
  --model Qwen/Qwen2.5-7B-Instruct \
  --workload ../bench/workloads/streaming_chat.yaml \
  --concurrency 1 \
  --requests 3 \
  --warmup 1 \
  --stream \
  --timeout 120s \
  --out-json ../bench/results/prod-streaming.json \
  --out-md ../bench/results/prod-streaming.md
```

Avoid production runs such as `--concurrency 16,32` or high request counts until there is an explicit load-test window and cost approval. The current production worker is a live RunPod GPU instance, so benchmark traffic consumes paid capacity.

## API Key Handling

Prefer `--api-key-file` over `--api-key` so the key does not appear in shell history:

```bash
go run ./cmd/infera-bench \
  --api-key-file ../.secrets/prod-smoke-key.txt \
  --base-url https://inferai.co.in \
  --model Qwen/Qwen2.5-7B-Instruct \
  --workload ../bench/workloads/short_chat.yaml
```

The CLI does not write API keys into JSON or Markdown reports. Do not commit key files or generated reports that contain copied secrets in error messages from external systems.

## Workloads

MVP workload files are YAML:

- `bench/workloads/short_chat.yaml`
- `bench/workloads/streaming_chat.yaml`

Each workload contains prompt IDs, chat messages, max output tokens, temperature, and tags. The CLI cycles through prompts when the request count is larger than the number of prompts.

## Reliable MVP Metrics

The MVP reports:

- request success and error counts;
- representative errors;
- end-to-end latency p50/p95/p99;
- requests/sec;
- token/sec when response usage is available;
- streaming TTFT p50/p95/p99 when a non-empty content delta is observed;
- approximate streaming TPOT p50/p95/p99 from inter-delta timing.

## Intentional MVP Gaps

The MVP does not implement:

- cost per request;
- cost per 1M tokens;
- selected worker/provider extraction;
- route decision metrics;
- p95/p99 routing decisions;
- SLO-aware routing;
- cost-aware routing;
- automatic GPU right-sizing.

Non-streaming TTFT and TPOT are marked unavailable unless a future gateway response exposes enough timing detail to measure them honestly.
