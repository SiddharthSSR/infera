"""Tests for the cold-start benchmark helper script."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import sys


REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT_PATH = REPO_ROOT / "scripts" / "cold-start-benchmark.py"


def load_module():
    spec = importlib.util.spec_from_file_location("cold_start_benchmark", SCRIPT_PATH)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def test_parse_args_defaults(monkeypatch):
    module = load_module()
    monkeypatch.setattr(
        "sys.argv",
        [
            "cold-start-benchmark.py",
            "--api-key",
            "test-key",
            "--gpu-type",
            "A100_80GB",
            "--model",
            "Qwen/Qwen2.5-7B-Instruct",
        ],
    )
    args = module.parse_args()
    assert args.base_url == "https://inferai.co.in"
    assert args.provider == "runpod"
    assert args.engine == "vllm"
    assert args.health_url_template == "https://{provider_id}-8081.proxy.runpod.net/health"
    assert args.gpu_count == 1


def test_format_health_url_uses_provider_id():
    module = load_module()
    url = module.format_health_url(
        "https://{provider_id}-8081.proxy.runpod.net/health",
        {"id": "inst-1", "provider_id": "pod-123", "http_port": 8081, "public_ip": ""},
    )
    assert url == "https://pod-123-8081.proxy.runpod.net/health"


def test_match_worker_prefers_provider_id_in_address():
    module = load_module()
    worker = module.match_worker(
        {
            "workers": [
                {
                    "worker_id": "w1",
                    "address": "pod-123-8081.proxy.runpod.net",
                    "last_heartbeat": "2026-03-23T03:31:56.807682Z",
                },
                {"worker_id": "w2", "address": "other.proxy.runpod.net"},
            ]
        },
        {"provider_id": "pod-123", "worker_id": ""},
        min_heartbeat_ms=1774236716800,
    )
    assert worker == {
        "worker_id": "w1",
        "address": "pod-123-8081.proxy.runpod.net",
        "last_heartbeat": "2026-03-23T03:31:56.807682Z",
    }


def test_match_worker_rejects_stale_or_forbidden_worker():
    module = load_module()
    worker = module.match_worker(
        {
            "workers": [
                {
                    "worker_id": "old-worker",
                    "address": "pod-123-8081.proxy.runpod.net",
                    "last_heartbeat": "2026-03-23T03:31:56.807682Z",
                },
                {
                    "worker_id": "new-worker",
                    "address": "pod-123-8081.proxy.runpod.net",
                    "last_heartbeat": "2026-03-23T03:35:56.807682Z",
                },
            ]
        },
        {"provider_id": "pod-123", "worker_id": ""},
        min_heartbeat_ms=1774236800000,
        forbidden_worker_id="old-worker",
    )
    assert worker == {
        "worker_id": "new-worker",
        "address": "pod-123-8081.proxy.runpod.net",
        "last_heartbeat": "2026-03-23T03:35:56.807682Z",
    }


def test_compute_durations_omits_missing_fields():
    module = load_module()
    times = module.ScenarioTimes(
        t0_request_sent=1000,
        t1_instance_running=2000,
        t4_worker_registered=3000,
        t5_first_successful_completion=4500,
    )
    durations = module.compute_durations(times)
    assert durations == {
        "request_to_running_ms": 1000,
        "request_to_registered_ms": 2000,
        "request_to_first_success_ms": 3500,
        "registered_to_first_success_ms": 1500,
    }


def test_compute_durations_omits_negative_running_to_server_started():
    module = load_module()
    times = module.ScenarioTimes(
        t0_request_sent=1000,
        t1_instance_running=5000,
        t2_server_started=3000,
        t3_model_load_finished=9000,
    )
    durations = module.compute_durations(times)
    assert durations == {
        "request_to_running_ms": 4000,
        "request_to_server_started_ms": 2000,
        "server_to_model_ready_ms": 6000,
    }


def test_build_report_serializes_probe():
    module = load_module()
    scenario = module.ScenarioResult(
        scenario="fresh_provision",
        instance_id="inst-1",
        provider_id="pod-1",
        worker_id="worker-1",
        worker_address="pod-1-8081.proxy.runpod.net",
        health_url="https://pod-1-8081.proxy.runpod.net/health",
        times=module.ScenarioTimes(t0_request_sent=1000),
        durations_ms={},
        probe=module.ProbeResult(
            total_ms=123.4,
            prompt_tokens=5,
            completion_tokens=1,
            total_tokens=6,
            content="OK",
        ),
        health_snapshot={"ready": True},
    )
    payload = module.build_report(
        type(
            "Args",
            (),
            {
                "base_url": "https://inferai.co.in",
                "provider": "runpod",
                "engine": "vllm",
                "gpu_type": "A100_80GB",
                "provider_gpu_type_id": "NVIDIA A100 80GB PCIe",
                "gpu_count": 1,
                "model": "Qwen/Qwen2.5-7B-Instruct",
                "instance_name": "cold-start-bench",
                "poll_interval_ms": 2000,
                "timeout_s": 900,
            },
        )(),
        [scenario],
    )
    assert payload["scenarios"][0]["probe"]["content"] == "OK"


def test_write_json_output_creates_parent_directories(tmp_path):
    module = load_module()
    payload = {"status": "ok"}
    output = tmp_path / "nested" / "cold-start" / "result.json"
    written_path = module.write_json_output(str(output), payload)
    assert written_path == output
    assert json.loads(output.read_text(encoding="utf-8")) == payload


def test_parse_stage_timestamp_ms_assumes_utc_for_naive_values():
    module = load_module()
    parsed = module.parse_stage_timestamp_ms("2026-03-23T03:31:56.807682")
    assert parsed is not None
    assert parsed == 1774236716807


def test_parse_stage_timestamp_ms_accepts_nanosecond_utc_values():
    module = load_module()
    parsed = module.parse_stage_timestamp_ms("2026-03-23T00:50:23.185785865Z")
    assert parsed is not None
    assert parsed == 1774227023185


def test_fetch_health_falls_back_to_curl_on_1010(monkeypatch):
    module = load_module()

    def fake_request_json(method, url, **kwargs):
        raise RuntimeError(f"{method} {url} failed with HTTP 403: error code: 1010")

    def fake_request_json_via_curl(method, url, **kwargs):
        assert method == "GET"
        assert url == "https://pod-123-8081.proxy.runpod.net/health"
        return {"ready": True}

    monkeypatch.setattr(module, "request_json", fake_request_json)
    monkeypatch.setattr(module, "request_json_via_curl", fake_request_json_via_curl)

    payload = module.fetch_health(
        "https://pod-123-8081.proxy.runpod.net/health",
        timeout=15,
        insecure=True,
    )
    assert payload == {"ready": True}


def test_run_ready_path_does_not_block_registration_or_probe_on_health(monkeypatch):
    module = load_module()
    call_order: list[str] = []

    def fake_wait_for_instance_status(*args, **kwargs):
        call_order.append("instance")
        return 2000, {"id": "inst-1", "provider_id": "pod-1", "worker_id": "", "http_port": 8081}

    def fake_wait_for_health_stages(*args, **kwargs):
        call_order.append("health")
        stop_event = kwargs["stop_event"]
        state = kwargs["state"]
        stop_event.wait(0.01)
        state.t2_server_started = 2100
        state.t3_model_load_finished = 2300
        state.latest_payload = {"ready": True}
        state.finished = True
        return state

    def fake_wait_for_worker_registration(*args, **kwargs):
        call_order.append("registration")
        assert kwargs["scenario_start_ms"] == 1000
        assert kwargs["previous_worker_id"] is None
        return 2400, {"total": 1, "workers": [{"worker_id": "worker-1", "address": "pod-1-8081.proxy.runpod.net"}]}

    def fake_run_first_success_probe(*args, **kwargs):
        call_order.append("probe")
        return module.ProbeResult(
            total_ms=123.0,
            prompt_tokens=1,
            completion_tokens=1,
            total_tokens=2,
            content="OK",
        )

    monkeypatch.setattr(module, "wait_for_instance_status", fake_wait_for_instance_status)
    monkeypatch.setattr(module, "wait_for_health_stages", fake_wait_for_health_stages)
    monkeypatch.setattr(module, "wait_for_worker_registration", fake_wait_for_worker_registration)
    monkeypatch.setattr(module, "run_first_success_probe", fake_run_first_success_probe)
    monkeypatch.setattr(module, "now_ms", lambda: 2500)

    args = type(
        "Args",
        (),
        {
            "health_url_template": "https://{provider_id}-8081.proxy.runpod.net/health",
            "timeout_s": 900,
            "poll_interval_ms": 2000,
            "probe_max_tokens": 32,
            "probe_temperature": 0.0,
            "quiet_progress": True,
            "health_insecure": True,
        },
    )()

    result = module.run_ready_path(
        scenario_name="fresh_provision",
        base_url="https://inferai.co.in",
        api_key="test-key",
        model="Qwen/Qwen2.5-7B-Instruct",
        instance={"id": "inst-1", "provider_id": "pod-1"},
        t0=1000,
        args=args,
    )

    assert call_order[0] == "instance"
    assert "registration" in call_order
    assert "probe" in call_order
    assert call_order.index("registration") < call_order.index("probe")
    assert result.times.t4_worker_registered == 2400
    assert result.times.t5_first_successful_completion == 2500
    assert result.times.t2_server_started == 2100
    assert result.times.t3_model_load_finished == 2300
