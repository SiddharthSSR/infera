"""Tests for benchmark execution isolation and lifecycle cleanup."""

from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace

from infera_bench.execution import ExecutionStepResult, ProvisionExecutor
from infera_bench.orchestration import execute_suite
from infera_bench.schema import BenchmarkProfile, ExperimentExecutionResult, ExperimentSuite, ResolvedRunSpec


def build_run_spec(tmp_path: Path, *, run_id: str, workload_id: str) -> ResolvedRunSpec:
    return ResolvedRunSpec(
        suite_id="suite",
        run_id=run_id,
        engine_id="sglang",
        hardware_id="a100_sxm4_80gb",
        gpu_count=1,
        model_id="Qwen/Qwen2.5-7B-Instruct",
        workload_id=workload_id,
        benchmark_profile_id="provision_full",
        runtime_preset_id="baseline",
        provider="runpod",
        provider_gpu_type_id="NVIDIA A100-SXM4-80GB",
        provider_gpu_type="A100_SXM4_80GB",
        execution_mode="provision",
        compatibility_status="ready",
        output_dir=str(tmp_path / run_id),
    )


def test_provision_executor_terminates_retained_instance_after_warm_steps(monkeypatch, tmp_path):
    run_spec = build_run_spec(tmp_path, run_id="run-1", workload_id="mixed")
    profile = BenchmarkProfile(id="provision_full", display_name="Provision Full")
    executor = ProvisionExecutor(
        base_url="https://inferai.co.in",
        api_key="test-key",
        workload_file=tmp_path / "workloads.json",
        python_bin="/venv/bin/python",
        terminate_final_instance=True,
    )

    monkeypatch.setattr(
        "infera_bench.execution.run_step",
        lambda step, dry_run: ExecutionStepResult(
            name=step.name,
            category=step.category,
            output_path=step.output_path,
            command=step.command or ["echo", step.name],
            command_display=" ".join(step.command or ["echo", step.name]),
            started_at="2026-03-28T00:00:00Z",
            finished_at="2026-03-28T00:00:00Z",
            duration_ms=0,
            returncode=0,
            status="ok",
        ),
    )
    monkeypatch.setattr("infera_bench.execution.build_warm_command", lambda *args, **kwargs: ["echo", "warm"])
    monkeypatch.setattr("infera_bench.execution.wait_for_warm_registration", lambda **kwargs: None)
    monkeypatch.setattr(
        "infera_bench.execution.retained_health_url_for_step",
        lambda step_name, output_path: "https://worker.example/health",
    )
    monkeypatch.setattr(
        "infera_bench.execution.retained_instance_id_for_step",
        lambda step_name, output_path: "instance-123",
    )
    terminated: list[tuple[str, str, str]] = []
    monkeypatch.setattr(
        "infera_bench.execution.terminate_instance",
        lambda base_url, api_key, instance_id: terminated.append((base_url, api_key, instance_id)) or {"success": True},
    )

    result = executor.execute(run_spec, profile)

    assert result.status == "ok"
    assert terminated == [("https://inferai.co.in", "test-key", "instance-123")]


def test_execute_suite_waits_for_registry_drain_between_runs(monkeypatch, tmp_path):
    suite = ExperimentSuite(
        suite_id="suite",
        matrix={
            "engines": ["sglang"],
            "hardware": ["a100_sxm4_80gb"],
            "gpu_counts": [1],
            "models": ["Qwen/Qwen2.5-7B-Instruct"],
            "workloads": ["mixed"],
            "benchmark_profiles": ["provision_full"],
            "runtime_presets": ["baseline"],
        },
        output_root=str(tmp_path),
    )
    run_specs = [
        build_run_spec(tmp_path, run_id="run-1", workload_id="mixed"),
        build_run_spec(tmp_path, run_id="run-2", workload_id="repeated_prefix"),
    ]
    profile = BenchmarkProfile(id="provision_full", display_name="Provision Full")
    catalog = SimpleNamespace(
        root="/catalog",
        resolve_benchmark_profile=lambda benchmark_profile_id: profile,
    )

    monkeypatch.setattr("infera_bench.orchestration.build_adapter_registry", lambda catalog: {})
    monkeypatch.setattr("infera_bench.orchestration.expand_suite", lambda suite, catalog, adapters: run_specs)
    waits: list[dict[str, object]] = []
    monkeypatch.setattr(
        "infera_bench.orchestration.wait_for_model_registry_drain",
        lambda **kwargs: waits.append(kwargs),
    )

    class FakeProvisionExecutor:
        def __init__(self, **kwargs):
            self.kwargs = kwargs

        def execute(self, run_spec, benchmark_profile):
            manifest_path = tmp_path / f"{run_spec.run_id}.json"
            return ExperimentExecutionResult(
                run_spec=run_spec,
                generated_at="2026-03-28T00:00:00Z",
                status="ok",
                manifest_path=str(manifest_path),
            )

    monkeypatch.setattr("infera_bench.orchestration.ProvisionExecutor", FakeProvisionExecutor)
    monkeypatch.setattr(
        "infera_bench.orchestration.write_result_artifacts",
        lambda index, output_root: {
            "json": tmp_path / "index.json",
            "csv": tmp_path / "summary.csv",
            "markdown": tmp_path / "summary.md",
        },
    )

    index, _artifacts, execution_results = execute_suite(
        base_url="https://inferai.co.in",
        api_key="test-key",
        suite=suite,
        catalog=catalog,
        workload_file=tmp_path / "workloads.json",
        terminate_final_instance=True,
    )

    assert len(index.results) == 2
    assert len(execution_results) == 2
    assert waits == [
        {
            "base_url": "https://inferai.co.in",
            "api_key": "test-key",
            "model_id": "Qwen/Qwen2.5-7B-Instruct",
            "timeout_s": profile.registry_drain_timeout_s,
            "poll_interval_ms": profile.registry_drain_poll_interval_ms,
            "log_prefix": "benchmark-suite",
        }
    ]


def test_provision_executor_skips_warm_steps_without_successful_lifecycle(monkeypatch, tmp_path):
    run_spec = build_run_spec(tmp_path, run_id="run-fail", workload_id="mixed")
    profile = BenchmarkProfile(
        id="provision_tuning",
        display_name="Provision Tuning",
        stages=["cold_start", "warm_none", "warm_affinity"],
    )
    executor = ProvisionExecutor(
        base_url="https://inferai.co.in",
        api_key="test-key",
        workload_file=tmp_path / "workloads.json",
        python_bin="/venv/bin/python",
        terminate_final_instance=True,
        continue_on_error=True,
    )

    def fake_run_step(step, dry_run):
        return ExecutionStepResult(
            name=step.name,
            category=step.category,
            output_path=step.output_path,
            command=step.command or ["echo", step.name],
            command_display=" ".join(step.command or ["echo", step.name]),
            started_at="2026-03-29T00:00:00Z",
            finished_at="2026-03-29T00:00:00Z",
            duration_ms=0,
            returncode=1 if step.name == "cold_start" else 0,
            status="failed" if step.name == "cold_start" else "ok",
        )

    monkeypatch.setattr("infera_bench.execution.run_step", fake_run_step)
    monkeypatch.setattr("infera_bench.execution.build_warm_command", lambda *args, **kwargs: ["echo", "warm"])
    monkeypatch.setattr("infera_bench.execution.wait_for_warm_registration", lambda **kwargs: None)
    monkeypatch.setattr("infera_bench.execution.retained_health_url_for_step", lambda *args, **kwargs: None)
    monkeypatch.setattr("infera_bench.execution.retained_instance_id_for_step", lambda *args, **kwargs: None)

    result = executor.execute(run_spec, profile)

    assert result.status == "failed"
    assert [step.status for step in result.steps] == ["failed", "skipped", "skipped"]
    assert any("Skipped warm benchmark steps" in note for note in result.notes)


def test_execute_suite_waits_for_registry_drain_after_failed_provision_runs(monkeypatch, tmp_path):
    suite = ExperimentSuite(
        suite_id="suite",
        matrix={
            "engines": ["sglang"],
            "hardware": ["a100_sxm4_80gb"],
            "gpu_counts": [1],
            "models": ["Qwen/Qwen2.5-7B-Instruct"],
            "workloads": ["mixed"],
            "benchmark_profiles": ["provision_full"],
            "runtime_presets": ["baseline"],
        },
        output_root=str(tmp_path),
    )
    run_specs = [
        build_run_spec(tmp_path, run_id="run-1", workload_id="mixed"),
        build_run_spec(tmp_path, run_id="run-2", workload_id="repeated_prefix"),
    ]
    profile = BenchmarkProfile(id="provision_full", display_name="Provision Full")
    catalog = SimpleNamespace(
        root="/catalog",
        resolve_benchmark_profile=lambda benchmark_profile_id: profile,
    )

    monkeypatch.setattr("infera_bench.orchestration.build_adapter_registry", lambda catalog: {})
    monkeypatch.setattr("infera_bench.orchestration.expand_suite", lambda suite, catalog, adapters: run_specs)
    waits: list[dict[str, object]] = []
    monkeypatch.setattr(
        "infera_bench.orchestration.wait_for_model_registry_drain",
        lambda **kwargs: waits.append(kwargs),
    )

    class FakeProvisionExecutor:
        def __init__(self, **kwargs):
            self.kwargs = kwargs

        def execute(self, run_spec, benchmark_profile):
            manifest_path = tmp_path / f"{run_spec.run_id}.json"
            status = "failed" if run_spec.run_id == "run-1" else "ok"
            return ExperimentExecutionResult(
                run_spec=run_spec,
                generated_at="2026-03-28T00:00:00Z",
                status=status,
                manifest_path=str(manifest_path),
            )

    monkeypatch.setattr("infera_bench.orchestration.ProvisionExecutor", FakeProvisionExecutor)
    monkeypatch.setattr(
        "infera_bench.orchestration.write_result_artifacts",
        lambda index, output_root: {
            "json": tmp_path / "index.json",
            "csv": tmp_path / "summary.csv",
            "markdown": tmp_path / "summary.md",
        },
    )

    index, _artifacts, execution_results = execute_suite(
        base_url="https://inferai.co.in",
        api_key="test-key",
        suite=suite,
        catalog=catalog,
        workload_file=tmp_path / "workloads.json",
        terminate_final_instance=True,
    )

    assert len(index.results) == 2
    assert len(execution_results) == 2
    assert waits == [
        {
            "base_url": "https://inferai.co.in",
            "api_key": "test-key",
            "model_id": "Qwen/Qwen2.5-7B-Instruct",
            "timeout_s": profile.registry_drain_timeout_s,
            "poll_interval_ms": profile.registry_drain_poll_interval_ms,
            "log_prefix": "benchmark-suite",
        }
    ]
