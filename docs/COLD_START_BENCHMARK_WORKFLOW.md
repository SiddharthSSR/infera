# Infera Cold-Start Benchmark Workflow

Use this runbook to measure cold-start behavior consistently across provider paths.

## Scope

Record all three scenarios separately:

1. `fresh_provision`: create a brand-new instance with `POST /api/instances/provision`
2. `stopped_instance_start`: start a known stopped instance with `POST /api/instances/{id}/start`
3. `stopped_instance_reuse`: call `POST /api/instances/provision` with the same workspace, model, and GPU shape and confirm the manager reuses a stopped instance instead of creating a new one

The third path matters because the provider manager already reuses matching stopped instances instead of always provisioning a new pod, and RunPod keeps model caches warm on the persistent `/workspace` volume across stop/start cycles.

## API Paths Used by This Workflow

The current gateway implements these routes:

- `POST /api/instances/provision`
- `GET /api/instances`
- `GET /api/instances/{id}`
- `POST /api/instances/{id}/stop`
- `POST /api/instances/{id}/start`
- `GET /api/workers`

Important behavior notes from the current code:

- `POST /api/instances/provision` returns `201 Created` with the new tracked instance in `instance`.
- `POST /api/instances/{id}/start` returns `200 OK` and flips the tracked instance status to `provisioning`.
- `POST /api/instances/{id}/stop` returns `200 OK` and flips the tracked instance status to `stopped`.
- `GET /api/instances` and `GET /api/instances/{id}` read the manager's tracked in-memory instance state.
- The gateway refreshes provider instance state in a background loop every `10s`, so `instance_running` from `/api/instances/*` is a coarse milestone, not the most precise startup signal.
- Worker `/health` is now the precise source for worker-side milestones such as `server_started`, `model_load_finished`, and `gateway_registered`.

## Stopped-Instance Reuse Match Criteria

`stopped_instance_reuse` only happens when all of these match the new provision request:

- provider
- workspace ID
- instance status is `stopped`
- `gpu_type`
- `gpu_count`
- model list contents

The current reuse matcher does not consider region, cost limit, docker image, or provider-specific options. Keep the request shape stable anyway during benchmarks so you do not accidentally benchmark a changed environment.

## Timestamp Definitions

Capture these timestamps for every run:

- `T0 request_sent`: the instant the provision or start action is triggered
- `T1 instance_running`: the first time `GET /api/instances` or `GET /api/instances/{id}` shows the instance as `running`
- `T2 server_started`: the first time the worker `/health` payload exposes `startup.stages.server_started`
- `T3 model_load_finished`: the first time the worker `/health` payload exposes `startup.stages.model_load_finished`
- `T4 worker_registered`: the first time the worker appears in `GET /api/workers` or `/health` shows `gateway_registered=true`
- `T5 first_successful_completion`: the first successful response from `scripts/benchmark-chat.py` or a direct `/v1/chat/completions` request

Derive these metrics:

- `provision_to_running_ms = T1 - T0`
- `running_to_server_started_ms = T2 - T1`
- `server_to_model_ready_ms = T3 - T2`
- `provision_to_registered_ms = T4 - T0`
- `provision_to_first_success_ms = T5 - T0`
- `registered_to_first_success_ms = T5 - T4`

## Preparation

Before every cold-start measurement:

1. Record branch, commit, provider, GPU type, model, image tag or digest, and hourly cost estimate.
2. Choose whether the run is expected to be cache-cold or cache-warm.
3. If you are measuring `stopped_instance_reuse`, confirm there is already one stopped instance with the same workspace, model, provider, GPU type, and GPU count.
4. Prepare the benchmark command you will use for `T3`.

Recommended instance provision payload template:

```json
{
  "name": "cold-start-bench",
  "provider": "runpod",
  "gpu_type": "A100_40GB",
  "gpu_count": 1,
  "models": ["Qwen/Qwen2.5-7B-Instruct"]
}
```

Example provision command:

```bash
curl -fsS \
  -X POST "https://your-gateway.example.com/api/instances/provision" \
  -H "Authorization: Bearer $INFERA_ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "cold-start-bench",
    "provider": "runpod",
    "gpu_type": "A100_40GB",
    "gpu_count": 1,
    "models": ["Qwen/Qwen2.5-7B-Instruct"]
  }'
```

Example start/stop commands:

```bash
curl -fsS \
  -X POST "https://your-gateway.example.com/api/instances/INSTANCE_ID/stop" \
  -H "Authorization: Bearer $INFERA_ADMIN_KEY"

curl -fsS \
  -X POST "https://your-gateway.example.com/api/instances/INSTANCE_ID/start" \
  -H "Authorization: Bearer $INFERA_ADMIN_KEY"
```

Example poll commands:

```bash
curl -fsS \
  -H "Authorization: Bearer $INFERA_ADMIN_KEY" \
  "https://your-gateway.example.com/api/instances/INSTANCE_ID"

curl -fsS \
  -H "Authorization: Bearer $INFERA_ADMIN_KEY" \
  "https://your-gateway.example.com/api/workers"
```

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
   Because the manager is refreshed on a `10s` loop, treat this as an infrastructure milestone, not a precise container-ready signal.
3. Poll the worker `/health` endpoint until `startup.stages.server_started` appears; record `T2 server_started`.
4. Continue polling `/health` until `startup.stages.model_load_finished` appears and `ready=true`; record `T3 model_load_finished`.
5. Poll `GET /api/workers` until the worker registers, or use `/health` and wait for `gateway_registered=true`; record `T4 worker_registered`.
6. Run the benchmark command once and record the first successful completion as `T5 first_successful_completion`.

### 2. Stopped Instance Start

1. Pick a stopped instance that already has the target model and image configuration.
2. Trigger `POST /api/instances/{id}/start` and record `T0 request_sent`.
3. Record `T1` through `T5` using the same polling and benchmark steps as above.

### 3. Stopped Instance Reuse

1. Stop a matching instance first with `POST /api/instances/{id}/stop`.
2. Trigger `POST /api/instances/provision` again with the same workload shape and record `T0 request_sent`.
3. Confirm the returned instance ID matches the stopped instance instead of a new provider-side provision.
   The reuse path should call provider `Start()` and should not create a second provider-side instance.
4. Record `T1` through `T5` using the same polling and benchmark steps as above.

## Recording Rules

- Use the same model, prompt preset, gateway build, and worker image for all three scenarios.
- Keep `provider`, `workspace`, `gpu_type`, `gpu_count`, and `models` identical between the stopped-instance run and the reuse run.
- Do not mix warmup groups into cold-start timing. Cold-start timing ends at the first successful completion.
- If a provider retry, image pull failure, or manual intervention happens, record it in the notes and rerun the sample.
- For repeated measurements, run at least three samples per scenario and keep median plus worst-case values.

## Output

Copy the results into [`docs/BENCHMARK_BASELINE_TEMPLATE.md`](/Users/siddharthsingh/codingtensor/infera/docs/BENCHMARK_BASELINE_TEMPLATE.md) with one row per scenario and note whether model artifacts came from remote download, persistent volume cache, or a previously loaded stopped instance.
