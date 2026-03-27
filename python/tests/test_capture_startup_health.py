"""Tests for the startup health capture helper script."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import sys
import pytest


REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT_PATH = REPO_ROOT / "scripts" / "capture-startup-health.py"


def load_module():
    spec = importlib.util.spec_from_file_location("capture_startup_health", SCRIPT_PATH)
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
            "capture-startup-health.py",
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
    assert args.engine == "vllm"
    assert args.instance_name == "cache-probe-bench"
    assert args.include_restart is False
    assert args.allowed_cuda_version == []


def test_build_provision_payload_defaults_allowed_cuda_versions_for_tensorrt_runpod():
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "instance_name": "cache-probe-bench",
            "provider": "runpod",
            "engine": "tensorrt_llm",
            "gpu_type": "A100_80GB",
            "gpu_count": 1,
            "model": "Qwen/Qwen2.5-7B-Instruct",
            "provider_gpu_type_id": "NVIDIA A100 80GB PCIe",
            "allowed_cuda_version": [],
            "runtime_option": [],
        },
    )()

    payload = module.build_provision_payload(args)

    assert payload["allowed_cuda_versions"] == ["12.6", "12.7", "12.8"]


def test_build_provision_payload_prefers_explicit_allowed_cuda_versions():
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "instance_name": "cache-probe-bench",
            "provider": "runpod",
            "engine": "tensorrt_llm",
            "gpu_type": "A100_80GB",
            "gpu_count": 1,
            "model": "Qwen/Qwen2.5-7B-Instruct",
            "provider_gpu_type_id": "",
            "allowed_cuda_version": ["12.7", "12.8", "12.8"],
            "runtime_option": [],
        },
    )()

    payload = module.build_provision_payload(args)

    assert payload["allowed_cuda_versions"] == ["12.7", "12.8"]


def test_build_provision_payload_includes_runtime_options():
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "instance_name": "cache-probe-bench",
            "provider": "runpod",
            "engine": "sglang",
            "gpu_type": "A100_80GB",
            "gpu_count": 1,
            "model": "Qwen/Qwen2.5-7B-Instruct",
            "provider_gpu_type_id": "",
            "allowed_cuda_version": [],
            "runtime_option": [
                "INFERA_ENGINE=sglang",
                "INFERA_SGLANG_MAX_RUNNING_REQUESTS=32",
            ],
        },
    )()

    payload = module.build_provision_payload(args)

    assert payload["options"] == {
        "INFERA_ENGINE": "sglang",
        "INFERA_SGLANG_MAX_RUNNING_REQUESTS": "32",
    }


def test_build_report_serializes_capture():
    module = load_module()
    capture = module.HealthCapture(
        label="fresh_provision",
        instance_id="inst-1",
        provider_id="pod-1",
        health_url="https://pod-1-8081.proxy.runpod.net/health",
        t0_request_sent=1000,
        t1_instance_running=2000,
        t2_server_started=3000,
        t3_model_load_finished=4000,
        health_snapshot={"ready": True, "startup": {"metadata": {"model_loads": {}}}},
        notes=[],
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
                "instance_name": "cache-probe-bench",
                "poll_interval_ms": 2000,
                "timeout_s": 900,
            },
        )(),
        [capture],
    )
    assert payload["captures"][0]["label"] == "fresh_provision"
    assert payload["captures"][0]["health_snapshot"]["ready"] is True


def test_wait_for_health_ready_returns_ready_payload(monkeypatch):
    module = load_module()
    payload = {
        "ready": True,
        "startup": {
            "stages": {
                "server_started": "2026-03-23T12:38:22.680046",
                "model_load_finished": "2026-03-23T12:41:19.475298",
            }
        },
    }

    monkeypatch.setattr(module, "fetch_health", lambda *args, **kwargs: payload)

    args = type("Args", (), {"health_insecure": True, "quiet_progress": True})()
    t2, t3, snapshot, notes = module.wait_for_health_ready(
        "https://pod-1-8081.proxy.runpod.net/health",
        timeout_s=1,
        poll_interval_ms=1,
        args=args,
    )

    assert t2 == 1774269502680
    assert t3 == 1774269679475
    assert snapshot == payload
    assert notes == []


def test_fatal_health_payload_reason_reports_gpu_preflight_failure():
    module = load_module()
    payload = {
        "state": "error",
        "live": False,
        "startup": {
            "metadata": {
                "gpu_preflight": {
                    "status": "failed",
                    "error_type": "RuntimeError",
                    "error": "CUDA unknown error",
                }
            }
        },
    }

    reason = module.fatal_health_payload_reason(payload)

    assert reason == (
        "worker entered error state during startup: gpu_preflight failed: "
        "RuntimeError: CUDA unknown error"
    )


def test_wait_for_health_ready_raises_on_fatal_startup_payload(monkeypatch):
    module = load_module()
    payload = {
        "ready": False,
        "state": "error",
        "live": False,
        "startup": {
            "metadata": {
                "gpu_preflight": {
                    "status": "failed",
                    "error_type": "RuntimeError",
                    "error": "CUDA unknown error",
                }
            }
        },
    }

    monkeypatch.setattr(module, "fetch_health", lambda *args, **kwargs: payload)

    args = type("Args", (), {"health_insecure": True, "quiet_progress": True})()
    with pytest.raises(RuntimeError, match="gpu_preflight failed"):
        module.wait_for_health_ready(
            "https://pod-1-8081.proxy.runpod.net/health",
            timeout_s=1,
            poll_interval_ms=1,
            args=args,
        )


def test_summarize_health_poll_error_classifies_bootstrap_failures():
    module = load_module()

    assert module.summarize_health_poll_error(RuntimeError("GET /health failed with HTTP 404:")) == (
        "bootstrap in progress: worker health route not published yet (HTTP 404)"
    )
    assert module.summarize_health_poll_error(RuntimeError("GET /health failed with HTTP 502: bad gateway")) == (
        "bootstrap in progress: proxy upstream not ready yet (HTTP 502)"
    )
    assert module.summarize_health_poll_error(RuntimeError("The read operation timed out")) == (
        "bootstrap in progress: worker health endpoint not responding yet (timeout)"
    )


def test_write_json_output_creates_parent_directories(tmp_path):
    module = load_module()
    payload = {"status": "ok"}
    output = tmp_path / "nested" / "capture" / "result.json"
    written_path = module.write_json_output(str(output), payload)
    assert written_path == output
    assert json.loads(output.read_text(encoding="utf-8")) == payload
