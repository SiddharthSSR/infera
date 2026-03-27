## Inference Performance Lab

The performance lab is a configuration-driven framework for evaluating and tuning inference serving across engines, hardware classes, models, precisions, quantization variants, and workloads.

### Catalog Root

The repo-tracked source of truth lives under:

- [configs/benchmark_lab/engines.json](/Users/siddharthsingh/codingtensor/infera/configs/benchmark_lab/engines.json)
- [configs/benchmark_lab/hardware.json](/Users/siddharthsingh/codingtensor/infera/configs/benchmark_lab/hardware.json)
- [configs/benchmark_lab/models.json](/Users/siddharthsingh/codingtensor/infera/configs/benchmark_lab/models.json)
- [configs/benchmark_lab/workloads.json](/Users/siddharthsingh/codingtensor/infera/configs/benchmark_lab/workloads.json)
- [configs/benchmark_lab/benchmark_profiles.json](/Users/siddharthsingh/codingtensor/infera/configs/benchmark_lab/benchmark_profiles.json)
- [configs/benchmark_lab/suites/cross_engine_baseline.json](/Users/siddharthsingh/codingtensor/infera/configs/benchmark_lab/suites/cross_engine_baseline.json)

These catalogs define:

- engines and engine-specific tunables
- hardware IDs and provider selectors
- model variants with precision and quantization metadata
- workload profiles and traffic shapes
- benchmark execution profiles
- experiment suites and cross-product sweeps

### Python Framework

The generic orchestration library lives in:

- [python/src/infera_bench/schema.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/schema.py)
- [python/src/infera_bench/catalog.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/catalog.py)
- [python/src/infera_bench/adapters.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/adapters.py)
- [python/src/infera_bench/matrix.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/matrix.py)
- [python/src/infera_bench/execution.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/execution.py)
- [python/src/infera_bench/results.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/results.py)
- [python/src/infera_bench/orchestration.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/orchestration.py)
- [python/src/infera_bench/lab.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/lab.py)

Key behavior:

- experiment suites expand into explicit resolved run specs
- engine adapters translate generic tunables into engine-specific runtime env vars
- compatibility is classified as `ready`, `unverified`, `blocked`, `unsupported`, or `invalid`
- warm runs can sample worker `/health` to capture memory usage and stability
- result artifacts are emitted as JSON, CSV, and Markdown summaries

### Isolation Boundary

The performance lab is intentionally structured as an isolated internal module that can be extracted later if needed without assuming a separate repo today.

The preferred Python integration surface is:

- [python/src/infera_bench/lab.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_bench/lab.py)

This facade owns:

- catalog loading
- suite validation
- suite execution
- result index loading
- result comparison

CLI wrappers should depend on this facade instead of importing multiple internal submodules directly.

On the Go side, benchmark API handlers depend on:

- [go/internal/gateway/benchmark_service.go](/Users/siddharthsingh/codingtensor/infera/go/internal/gateway/benchmark_service.go)

That service boundary keeps HTTP delivery separate from benchmark-spec parsing and comparison logic, so the benchmark module can move independently later if required.

### CLI

Generic suite execution:

```bash
python3 scripts/run-benchmark-suite.py \
  https://inferai.co.in \
  --api-key "$INFERA_ADMIN_KEY" \
  --suite-file configs/benchmark_lab/suites/cross_engine_baseline.json
```

Validate a suite without executing:

```bash
python3 scripts/validate-benchmark-suite.py \
  --suite-file configs/benchmark_lab/suites/cross_engine_baseline.json
```

Compare result indexes:

```bash
python3 scripts/compare-benchmark-results.py \
  /tmp/infera-benchmark-lab/cross-engine-baseline/cross-engine-baseline-result-index.json \
  --objective balanced
```

### API

The gateway now exposes:

- `GET /api/benchmarks/catalog`
- `POST /api/benchmarks/validate`
- `POST /api/benchmarks/compare`

These endpoints use the same embedded benchmark catalog surface as the CLI-facing framework.

### Current Scope

What is generic now:

- cross-engine experiment expansion
- hardware selection through catalog IDs and provider selectors
- model variants with precision and quantization metadata
- workload-driven benchmark execution
- result indexing and objective-based comparison

What is still engine-specific by design:

- worker runtime option names
- adapter-level tunable translation
- engine-specific compatibility hints

What still remains incremental:

- the older Phase 1 and Phase 2 scripts remain available for compatibility and legacy artifact formats
- provider runtime defaults in the Go provision path still need full migration onto the new heuristic catalogs
- richer workload arrival models and multi-profile mixed-request distributions can be added without changing orchestration code
