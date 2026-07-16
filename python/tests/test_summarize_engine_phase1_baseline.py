"""Tests for summarize-engine-phase1-baseline.py."""

from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT_PATH = REPO_ROOT / "scripts" / "summarize-engine-phase1-baseline.py"


def load_module():
    spec = importlib.util.spec_from_file_location("summarize_engine_phase1_baseline", SCRIPT_PATH)
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


def build_warm_payload(engine: str, cache_reuse_mode: str) -> dict:
    return {
        "base_url": "https://inferai.co.in",
        "model": "Qwen/Qwen2.5-7B-Instruct",
        "engine": engine,
        "provider": "runpod",
        "gpu_type": "A100_80GB",
        "runs": 2,
        "concurrency": 4,
        "warmup": 2,
        "cache_reuse_mode": cache_reuse_mode,
        "cost_per_hour": 1.19,
        "presets": {
            "conversation": [
                {
                    "run": 1,
                    "group_run": 1,
                    "client_index": 1,
                    "ttft_ms": 100.0,
                    "stream_total_ms": 400.0,
                    "non_stream_total_ms": 420.0,
                    "completion_tokens": 10,
                    "total_tokens": 20,
                    "decode_tok_s": 50.0,
                    "aggregate_decode_tok_s": 180.0,
                    "cost_query_usd": 0.001,
                },
                {
                    "run": 2,
                    "group_run": 2,
                    "client_index": 1,
                    "ttft_ms": 140.0,
                    "stream_total_ms": 440.0,
                    "non_stream_total_ms": 460.0,
                    "completion_tokens": 12,
                    "total_tokens": 22,
                    "decode_tok_s": 60.0,
                    "aggregate_decode_tok_s": 220.0,
                    "cost_query_usd": 0.002,
                },
            ]
        },
    }


def build_cold_payload(engine: str) -> dict:
    return {
        "base_url": "https://inferai.co.in",
        "provider": "runpod",
        "engine": engine,
        "gpu_type": "A100_80GB",
        "provider_gpu_type_id": "NVIDIA A100 80GB PCIe",
        "gpu_count": 1,
        "model": "Qwen/Qwen2.5-7B-Instruct",
        "scenarios": [
            {
                "scenario": "fresh_provision",
                "durations_ms": {
                    "request_to_running_ms": 3000,
                    "request_to_server_started_ms": 12000,
                    "server_to_model_ready_ms": 90000,
                    "request_to_registered_ms": 104000,
                    "request_to_first_success_ms": 105500,
                    "registered_to_first_success_ms": 1500,
                },
                "health_snapshot": {
                    "gateway_registered": True,
                    "memory_used_bytes": 21474836480,
                    "memory_total_bytes": 42949672960,
                    "startup": {
                        "metadata": {
                            "model_loads": {
                                "Qwen/Qwen2.5-7B-Instruct": {
                                    "inferred_hf_repo_cache_exists": False,
                                    "inferred_hf_snapshot_count": 0,
                                }
                            }
                        }
                    },
                },
                "notes": [],
            },
            {
                "scenario": "stopped_instance_start",
                "durations_ms": {
                    "request_to_running_ms": 2000,
                    "request_to_server_started_ms": 4000,
                    "server_to_model_ready_ms": 30000,
                    "request_to_registered_ms": 36000,
                    "request_to_first_success_ms": 37000,
                    "registered_to_first_success_ms": 1000,
                },
                "health_snapshot": {
                    "gateway_registered": False,
                    "memory_used_bytes": 21474836480,
                    "memory_total_bytes": 42949672960,
                    "startup": {"metadata": {"model_loads": {}}},
                },
                "notes": ["terminated final instance"],
            },
        ],
    }


def build_startup_payload(engine: str) -> dict:
    return {
        "base_url": "https://inferai.co.in",
        "provider": "runpod",
        "engine": engine,
        "gpu_type": "A100_80GB",
        "gpu_count": 1,
        "model": "Qwen/Qwen2.5-7B-Instruct",
        "captures": [
            {
                "label": "fresh_provision",
                "t0_request_sent": 1000,
                "t1_instance_running": 2000,
                "t2_server_started": 4000,
                "t3_model_load_finished": 94000,
                "health_snapshot": {
                    "gateway_registered": False,
                    "memory_used_bytes": 21474836480,
                    "memory_total_bytes": 42949672960,
                    "startup": {
                        "durations_ms": {
                            "worker_ready": 90000,
                            "engine_create_started": 0,
                            "engine_create_finished": 30000,
                            "vllm_engine_init_started": 30000,
                            "vllm_engine_init_finished": 88000,
                        },
                        "metadata": {
                            "model_loads": {
                                "Qwen/Qwen2.5-7B-Instruct": {
                                    "local_model_path_exists": False,
                                    "inferred_hf_repo_cache_exists": False,
                                    "inferred_hf_snapshot_count": 0,
                                }
                            }
                        },
                    },
                },
                "notes": [],
            },
            {
                "label": "stopped_instance_start",
                "t0_request_sent": 100000,
                "t1_instance_running": 102000,
                "t2_server_started": 104000,
                "t3_model_load_finished": 154000,
                "health_snapshot": {
                    "gateway_registered": False,
                    "memory_used_bytes": 21474836480,
                    "memory_total_bytes": 42949672960,
                    "startup": {
                        "durations_ms": {
                            "worker_ready": 50000,
                            "engine_create_started": 0,
                            "engine_create_finished": 12000,
                            "vllm_engine_init_started": 12000,
                            "vllm_engine_init_finished": 48000,
                        },
                        "metadata": {
                            "model_loads": {
                                "Qwen/Qwen2.5-7B-Instruct": {
                                    "local_model_path_exists": False,
                                    "inferred_hf_repo_cache_exists": True,
                                    "inferred_hf_snapshot_count": 1,
                                }
                            }
                        },
                    },
                },
                "notes": ["terminated final instance"],
            },
        ],
    }


def create_manifest(tmp_path: Path, engine: str, *, include_warm: bool) -> Path:
    out_dir = tmp_path / engine.replace("_", "-")
    steps = []
    if include_warm:
        warm_none_path = (
            out_dir / f"infera-benchmark-{engine.replace('_', '-')}-a100-80gb-none.json"
        )
        warm_affinity_path = (
            out_dir / f"infera-benchmark-{engine.replace('_', '-')}-a100-80gb-affinity.json"
        )
        write_json(warm_none_path, build_warm_payload(engine, "none"))
        write_json(warm_affinity_path, build_warm_payload(engine, "affinity"))
        steps.extend(
            [
                {
                    "name": "warm_none",
                    "category": "warm",
                    "output_path": str(warm_none_path),
                    "status": "ok",
                },
                {
                    "name": "warm_affinity",
                    "category": "warm",
                    "output_path": str(warm_affinity_path),
                    "status": "ok",
                },
            ]
        )

    cold_path = out_dir / f"cold-start-{engine.replace('_', '-')}-a100-80gb.json"
    startup_path = out_dir / f"startup-health-{engine.replace('_', '-')}-a100-80gb.json"
    write_json(cold_path, build_cold_payload(engine))
    write_json(startup_path, build_startup_payload(engine))
    steps.extend(
        [
            {
                "name": "cold_start",
                "category": "cold_start",
                "output_path": str(cold_path),
                "status": "ok",
            },
            {
                "name": "startup_health",
                "category": "startup_health",
                "output_path": str(startup_path),
                "status": "ok",
            },
        ]
    )

    manifest_path = out_dir / f"phase1-{engine.replace('_', '-')}-a100-80gb-manifest.json"
    write_json(
        manifest_path,
        {
            "base_url": "https://inferai.co.in",
            "engine": engine,
            "provider": "runpod",
            "gpu_type": "A100_80GB",
            "gpu_count": 1,
            "model": "Qwen/Qwen2.5-7B-Instruct",
            "preset": "conversation",
            "concurrency": 4,
            "warmup": 2,
            "warm_runs": 3,
            "cost_per_hour": 1.19,
            "notes": ["Warm results require a single active engine fleet."],
            "steps": steps,
        },
    )
    return manifest_path


def test_discover_manifest_paths_from_root(tmp_path):
    module = load_module()
    manifest_path = create_manifest(tmp_path, "vllm", include_warm=True)

    discovered = module.discover_manifest_paths([str(tmp_path)])

    assert discovered == [manifest_path.resolve()]


def test_build_report_summarizes_phase1_outputs_and_gaps(tmp_path):
    module = load_module()
    vllm_manifest = create_manifest(tmp_path, "vllm", include_warm=True)
    create_manifest(tmp_path, "tensorrt_llm", include_warm=False)

    manifest_paths = module.discover_manifest_paths([str(vllm_manifest.parent.parent)])
    report = module.build_report(manifest_paths, ["vllm", "sglang", "tensorrt_llm"])

    assert [row["engine"] for row in report["engines"]] == ["vllm", "tensorrt_llm"]
    assert [row["cache_reuse_mode"] for row in report["warm_matrix"]] == ["none", "affinity"]
    assert report["warm_matrix"][0]["ttft_p50_ms"] == 120.0
    assert report["warm_matrix"][0]["aggregate_decode_tok_s_p50"] == 200.0
    assert report["cold_start_matrix"][0]["request_to_first_success_ms"] == 105500
    assert report["startup_health_matrix"][0]["engine_init_stage"] == "vllm_engine_init"
    assert report["startup_health_matrix"][0]["engine_init_duration_ms"] == 58000
    assert "sglang: no phase 1 manifest discovered" in report["gaps"]
    assert "tensorrt_llm: manifest is missing expected step warm_none" in report["gaps"]
    assert "tensorrt_llm: manifest is missing expected step warm_affinity" in report["gaps"]


def test_build_report_marks_blocked_engines_separately(tmp_path):
    module = load_module()
    vllm_manifest = create_manifest(tmp_path, "vllm", include_warm=True)
    create_manifest(tmp_path, "tensorrt_llm", include_warm=False)

    manifest_paths = module.discover_manifest_paths([str(vllm_manifest.parent.parent)])
    report = module.build_report(
        manifest_paths,
        ["vllm", "tensorrt_llm"],
        {"tensorrt_llm": "Blocked on current RunPod provider/model combination."},
    )

    assert report["blocked_engines"] == [
        {
            "engine": "tensorrt_llm",
            "reason": "Blocked on current RunPod provider/model combination.",
            "manifest_path": str(
                (
                    tmp_path / "tensorrt-llm" / "phase1-tensorrt-llm-a100-80gb-manifest.json"
                ).resolve()
            ),
            "step_names": ["cold_start", "startup_health"],
        }
    ]
    assert not any(gap.startswith("tensorrt_llm:") for gap in report["gaps"])


def test_render_markdown_includes_all_sections(tmp_path):
    module = load_module()
    manifest_path = create_manifest(tmp_path, "vllm", include_warm=True)
    manifest_paths = module.discover_manifest_paths([str(manifest_path.parent.parent)])
    report = module.build_report(manifest_paths, ["vllm"])

    markdown = module.render_markdown(report)

    assert "# Untuned Engine Phase 1 Baseline" in markdown
    assert "## Warm Baseline" in markdown
    assert "## Cold-Start Baseline" in markdown
    assert "## Startup Health" in markdown
    assert "vllm" in markdown
    assert "120.0" in markdown
    assert "fresh_provision" in markdown


def test_render_markdown_includes_blocked_engines_section(tmp_path):
    module = load_module()
    manifest_path = create_manifest(tmp_path, "vllm", include_warm=True)
    manifest_paths = module.discover_manifest_paths([str(manifest_path.parent.parent)])
    report = module.build_report(
        manifest_paths,
        ["vllm", "tensorrt_llm"],
        {"tensorrt_llm": "Blocked on current RunPod provider/model combination."},
    )

    markdown = module.render_markdown(report)

    assert "## Blocked Engines" in markdown
    assert "Blocked on current RunPod provider/model combination." in markdown
