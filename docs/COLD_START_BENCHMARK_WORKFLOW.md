# Infera Cold-Start Benchmark Workflow

Use this runbook to measure cold-start behavior consistently across provider paths.

## Scope

Record all three scenarios separately:

1. `fresh_provision`: create a brand-new instance with `POST /api/instances/provision`
2. `stopped_instance_start`: start a known stopped instance with `POST /api/instances/{id}/start`
3. `stopped_instance_reuse`: call `POST /api/instances/provision` with the same workspace, model, and GPU shape and confirm the manager reuses a stopped instance instead of creating a new one

The third path matters because the provider manager already reuses matching stopped instances instead of always provisioning a new pod, and RunPod keeps model caches warm on the persistent `/workspace` volume across stop/start cycles.

## Timestamp Definitions

Capture these timestamps for every run:

- `T0 request_sent`: the instant the provision or start action is triggered
- `T1 instance_running`: the first time `GET /api/instances` or `GET /api/instances/{id}` shows the instance as `running`
- `T2 worker_registered`: the first time the worker appears in `GET /api/workers`
- `T3 first_successful_completion`: the first successful response from `scripts/benchmark-chat.py` or a direct `/v1/chat/completions` request

Derive these metrics:

- `provision_to_running_ms = T1 - T0`
- `provision_to_registered_ms = T2 - T0`
- `provision_to_first_success_ms = T3 - T0`
- `registered_to_first_success_ms = T3 - T2`

## Preparation

Before every cold-start measurement:

1. Record branch, commit, provider, GPU type, model, image tag or digest, and hourly cost estimate.
2. Choose whether the run is expected to be cache-cold or cache-warm.
3. If you are measuring `stopped_instance_reuse`, confirm there is already one stopped instance with the same workspace, model, provider, GPU type, and GPU count.
4. Prepare the benchmark command you will use for `T3`.

Recommended benchmark command:

```bash
python3 scripts/benchmark-chat.py \
  https://your-gateway.example.com \
  --api-key "$INFERA_SMOKE_API_KEY" \
  --model "your/model-id" \
  --preset conversation \
  --runs 1 \
  --warmup 0 \
  --concurrency 1 \
  --cache-reuse-mode none
```

## Procedure

### 1. Fresh Provision

1. Trigger `POST /api/instances/provision` and record `T0 request_sent`.
2. Poll `GET /api/instances/{id}` until the instance becomes `running`; record `T1 instance_running`.
3. Poll `GET /api/workers` until the worker registers; record `T2 worker_registered`.
4. Run the benchmark command once and record the first successful completion as `T3 first_successful_completion`.

### 2. Stopped Instance Start

1. Pick a stopped instance that already has the target model and image configuration.
2. Trigger `POST /api/instances/{id}/start` and record `T0 request_sent`.
3. Record `T1`, `T2`, and `T3` using the same polling and benchmark steps as above.

### 3. Stopped Instance Reuse

1. Stop a matching instance first with `POST /api/instances/{id}/stop`.
2. Trigger `POST /api/instances/provision` again with the same workload shape and record `T0 request_sent`.
3. Confirm the returned instance ID matches the stopped instance instead of a new provider-side provision.
4. Record `T1`, `T2`, and `T3` using the same polling and benchmark steps as above.

## Recording Rules

- Use the same model, prompt preset, gateway build, and worker image for all three scenarios.
- Do not mix warmup groups into cold-start timing. Cold-start timing ends at the first successful completion.
- If a provider retry, image pull failure, or manual intervention happens, record it in the notes and rerun the sample.
- For repeated measurements, run at least three samples per scenario and keep median plus worst-case values.

## Output

Copy the results into [`docs/BENCHMARK_BASELINE_TEMPLATE.md`](/Users/siddharthsingh/codingtensor/infera/docs/BENCHMARK_BASELINE_TEMPLATE.md) with one row per scenario and note whether model artifacts came from remote download, persistent volume cache, or a previously loaded stopped instance.
