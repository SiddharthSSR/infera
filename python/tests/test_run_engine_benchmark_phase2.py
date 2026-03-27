"""Tests for the Phase 2 named-profile runner."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import sys


REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT_PATH = REPO_ROOT / "scripts" / "run-engine-benchmark-phase2.py"


def load_module():
    spec = importlib.util.spec_from_file_location("run_engine_benchmark_phase2", SCRIPT_PATH)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def write_json(path: Path, payload: dict) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    return path


def test_build_profiles_merges_baseline_env_and_profile_overrides(tmp_path):
    module = load_module()
    baseline_path = write_json(
        tmp_path / "phase1.json",
        {
            "engines": {
                "vllm": {
                    "worker_env": {
                        "INFERA_VLLM_GPU_MEMORY_UTILIZATION": "0.90",
                        "INFERA_VLLM_ENABLE_PREFIX_CACHING": "true",
                        "INFERA_VLLM_MAX_NUM_SEQS": "48",
                    }
                }
            }
        },
    )
    tuning_path = write_json(
        tmp_path / "phase2.json",
        {
            "engines": {
                "vllm": {
                    "profiles": [
                        {
                            "name": "prefill_batching_4096",
                            "group": "prefill_batching",
                            "description": "Tune batching",
                            "worker_env": {"INFERA_VLLM_MAX_NUM_BATCHED_TOKENS": "4096"},
                        }
                    ]
                }
            }
        },
    )
    args = type(
        "Args",
        (),
        {
            "engine": "vllm",
            "profile": ["prefill_batching_4096"],
            "all_profiles": False,
            "baseline_preset_file": str(baseline_path),
            "tuning_preset_file": str(tuning_path),
        },
    )()

    baseline_payload, tuning_payload, blocked_reason = module.load_profile_config(args)
    profiles = module.build_profiles(args, baseline_payload, tuning_payload)

    assert blocked_reason is None
    assert profiles == [
        module.Phase2Profile(
            name="prefill_batching_4096",
            group="prefill_batching",
            description="Tune batching",
            runtime_options={
                "INFERA_VLLM_GPU_MEMORY_UTILIZATION": "0.90",
                "INFERA_VLLM_ENABLE_PREFIX_CACHING": "true",
                "INFERA_VLLM_MAX_NUM_SEQS": "48",
                "INFERA_VLLM_MAX_NUM_BATCHED_TOKENS": "4096",
            },
        )
    ]


def test_build_phase1_command_includes_profile_metadata_and_runtime_options(tmp_path):
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "python_bin": sys.executable,
            "phase1_runner": "scripts/run-engine-benchmark-phase1.py",
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
            "cache_key_prefix": "phase2",
            "cost_per_hour": 1.19,
            "instance_name_prefix": "engine-phase2",
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
            "warm_ready_timeout_s": 180,
        },
    )()
    profile = module.Phase2Profile(
        name="running_requests_32",
        group="running_requests",
        description="Limit running requests",
        runtime_options={
            "INFERA_SGLANG_MAX_RUNNING_REQUESTS": "32",
        },
    )

    command = module.build_phase1_command(args, profile)

    assert "--phase-label" in command
    assert "phase2" in command
    assert "--profile-name" in command
    assert "running_requests_32" in command
    assert "--runtime-option" in command
    assert "INFERA_SGLANG_MAX_RUNNING_REQUESTS=32" in command
    assert str(tmp_path / "sglang" / "running-requests-32" / "phase2-sglang-a100-80gb-running-requests-32-manifest.json") in command


def test_load_profile_config_returns_blocked_reason(tmp_path):
    module = load_module()
    baseline_path = write_json(tmp_path / "phase1.json", {"engines": {}})
    tuning_path = write_json(
        tmp_path / "phase2.json",
        {
            "blocked_engines": {"tensorrt_llm": {"reason": "Provider/runtime blocked."}},
            "engines": {},
        },
    )
    args = type(
        "Args",
        (),
        {
            "engine": "tensorrt_llm",
            "baseline_preset_file": str(baseline_path),
            "tuning_preset_file": str(tuning_path),
        },
    )()

    _baseline_payload, _tuning_payload, blocked_reason = module.load_profile_config(args)

    assert blocked_reason == "Provider/runtime blocked."


def test_filter_runtime_options_drops_reserved_keys():
    module = load_module()

    filtered = module.filter_runtime_options(
        {
            "INFERA_ENGINE": "vllm",
            " INFERA_VLLM_MAX_MODEL_LEN ": " 32768 ",
            "": "ignored",
        }
    )

    assert filtered == {"INFERA_VLLM_MAX_MODEL_LEN": "32768"}


def test_build_profile_run_spec_preserves_phase2_metadata(tmp_path):
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "engine": "vllm",
            "gpu_type": "A100_80GB",
            "gpu_count": 1,
            "model": "Qwen/Qwen2.5-7B-Instruct",
            "provider": "runpod",
            "provider_gpu_type_id": "NVIDIA A100 80GB PCIe",
            "preset": "conversation",
            "warm_runs": 3,
            "warmup": 2,
            "concurrency": 4,
            "cache_key_prefix": "phase2",
            "instance_name_prefix": "engine-phase2",
            "output_dir": str(tmp_path),
            "benchmark_header": ["X-Debug: on"],
        },
    )()
    profile = module.Phase2Profile(
        name="scheduler_steps_4",
        group="scheduler",
        description="Test scheduler steps 4",
        runtime_options={"INFERA_VLLM_NUM_SCHEDULER_STEPS": "4"},
    )

    run_spec = module.build_profile_run_spec(args, profile)

    assert run_spec.run_id == "phase2-vllm-a100-80gb-scheduler-steps-4"
    assert run_spec.output_dir.endswith("/vllm/scheduler-steps-4")
    assert run_spec.runtime_options == {"INFERA_VLLM_NUM_SCHEDULER_STEPS": "4"}
    assert run_spec.generic_parameters["legacy_prompt_preset"] == "conversation"
    assert run_spec.attach_target is not None
    assert run_spec.attach_target.cache_key_prefix == "phase2"


def test_build_phase2_benchmark_profile_respects_skip_flags(monkeypatch):
    module = load_module()
    args = type(
        "Args",
        (),
        {
            "skip_cold_start": True,
            "skip_startup_health": False,
            "skip_warm": False,
            "warm_ready_timeout_s": 240,
        },
    )()

    profile = module.build_phase2_benchmark_profile(args)

    assert "cold_start" not in profile.stages
    assert "startup_health" in profile.stages
    assert "warm_none" in profile.stages
    assert profile.warm_ready_timeout_s == 240
