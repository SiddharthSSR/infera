"""Tests for the Phase 1 engine benchmark orchestration script."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import sys


REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT_PATH = REPO_ROOT / "scripts" / "run-engine-benchmark-phase1.py"


def load_module():
    spec = importlib.util.spec_from_file_location("run_engine_benchmark_phase1", SCRIPT_PATH)
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
            "run-engine-benchmark-phase1.py",
            "--api-key",
            "test-key",
            "--engine",
            "vllm",
            "--gpu-type",
            "A100_80GB",
            "--model",
            "Qwen/Qwen2.5-7B-Instruct",
        ],
    )

    args = module.parse_args()

    assert args.base_url == "https://inferai.co.in"
    assert args.engine == "vllm"
    assert args.provider == "runpod"
    assert args.preset == "conversation"
    assert args.warm_runs == 3
    assert args.warmup == 2
    assert args.concurrency == 4
    assert args.output_dir == "/tmp/infera-engine-benchmarks"
    assert args.skip_warm is False
    assert args.skip_cold_start is False
    assert args.skip_startup_health is False


def test_build_phase1_steps_includes_expected_commands(tmp_path):
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "python_bin": "/venv/bin/python",
            "base_url": "https://inferai.co.in",
            "api_key": "test-key",
            "engine": "sglang",
            "provider": "runpod",
            "gpu_type": "A100_80GB",
            "provider_gpu_type_id": "NVIDIA A100 80GB PCIe",
            "gpu_count": 1,
            "model": "Qwen/Qwen2.5-7B-Instruct",
            "preset": "conversation",
            "warm_runs": 3,
            "warmup": 2,
            "concurrency": 4,
            "cache_key_prefix": "baseline",
            "cost_per_hour": 1.19,
            "instance_name_prefix": "engine-phase1",
            "output_dir": str(tmp_path),
            "benchmark_header": ["X-Debug: on"],
            "skip_warm": False,
            "skip_cold_start": False,
            "skip_startup_health": False,
            "terminate_final_instance": True,
            "health_insecure": True,
            "quiet_progress": True,
            "continue_on_error": False,
            "dry_run": False,
            "json_output": None,
        },
    )()

    steps = module.build_phase1_steps(args)

    assert [step.name for step in steps] == [
        "warm_none",
        "warm_affinity",
        "cold_start",
        "startup_health",
    ]
    assert steps[0].output_path.endswith("/sglang/infera-benchmark-sglang-a100-80gb-none.json")
    assert steps[1].output_path.endswith("/sglang/infera-benchmark-sglang-a100-80gb-affinity.json")
    assert "--engine-label" in steps[0].command
    assert "sglang" in steps[0].command
    assert "--provider-label" in steps[0].command
    assert "--gpu-label" in steps[0].command
    assert "--header" in steps[0].command
    assert "--cost-per-hour" in steps[0].command
    assert "--cache-reuse-mode" in steps[0].command
    assert "none" in steps[0].command
    assert "--cache-key-prefix" in steps[1].command
    assert "--engine" in steps[2].command
    assert "--health-insecure" in steps[2].command
    assert "--quiet-progress" in steps[2].command
    assert "--terminate-final-instance" in steps[2].command
    assert "--include-restart" in steps[3].command


def test_build_phase1_steps_respects_skip_flags(tmp_path):
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "python_bin": sys.executable,
            "base_url": "https://inferai.co.in",
            "api_key": "test-key",
            "engine": "tensorrt_llm",
            "provider": "runpod",
            "gpu_type": "A100_80GB",
            "provider_gpu_type_id": "",
            "gpu_count": 1,
            "model": "Qwen/Qwen2.5-7B-Instruct",
            "preset": "conversation",
            "warm_runs": 3,
            "warmup": 2,
            "concurrency": 4,
            "cache_key_prefix": "baseline",
            "cost_per_hour": None,
            "instance_name_prefix": "engine-phase1",
            "output_dir": str(tmp_path),
            "benchmark_header": [],
            "skip_warm": True,
            "skip_cold_start": False,
            "skip_startup_health": True,
            "terminate_final_instance": False,
            "health_insecure": False,
            "quiet_progress": False,
            "continue_on_error": False,
            "dry_run": False,
            "json_output": None,
        },
    )()

    steps = module.build_phase1_steps(args)

    assert [step.name for step in steps] == ["cold_start"]
    assert "--cost-per-hour" not in steps[0].command


def test_run_step_marks_dry_run():
    module = load_module()

    result = module.run_step(
        module.Phase1Step(
            name="warm_none",
            category="warm",
            output_path="/tmp/out.json",
            command=["python3", "scripts/benchmark-chat.py", "--model", "test"],
        ),
        dry_run=True,
    )

    assert result.status == "dry_run"
    assert result.returncode is None
    assert result.duration_ms == 0
    assert "benchmark-chat.py" in result.command_display


def test_write_json_output_creates_parent_directories(tmp_path):
    module = load_module()
    payload = {"status": "ok"}
    output = tmp_path / "nested" / "phase1" / "manifest.json"
    written_path = module.write_json_output(output, payload)
    assert written_path == output
    assert json.loads(output.read_text(encoding="utf-8")) == payload
