"""Tests for benchmark result comparison."""

from __future__ import annotations

from infera_bench.results import compare_result_indexes, format_comparison_markdown
from infera_bench.schema import ExperimentResultIndex, ExperimentResultRecord, WarmMetricSummary


def test_compare_result_indexes_ranks_best_latency_first() -> None:
    index = ExperimentResultIndex(
        generated_at="2026-03-27T00:00:00Z",
        suite_id="suite-a",
        catalog_root="/tmp/catalog",
        results=[
            ExperimentResultRecord(
                run_id="slow",
                suite_id="suite-a",
                status="ok",
                compatibility_status="ready",
                engine_id="vllm",
                hardware_id="a100_80gb",
                gpu_count=1,
                model_id="model-a",
                workload_id="mixed",
                benchmark_profile_id="provision_full",
                runtime_preset_id="baseline",
                manifest_path="/tmp/slow.json",
                warm_summaries=[
                    WarmMetricSummary(
                        cache_reuse_mode="affinity",
                        workload="mixed",
                        ttft_p50_ms=500,
                        source_path="/tmp/slow-warm.json",
                    )
                ],
            ),
            ExperimentResultRecord(
                run_id="fast",
                suite_id="suite-a",
                status="ok",
                compatibility_status="ready",
                engine_id="sglang",
                hardware_id="a100_80gb",
                gpu_count=1,
                model_id="model-a",
                workload_id="mixed",
                benchmark_profile_id="provision_full",
                runtime_preset_id="baseline",
                manifest_path="/tmp/fast.json",
                warm_summaries=[
                    WarmMetricSummary(
                        cache_reuse_mode="affinity",
                        workload="mixed",
                        ttft_p50_ms=300,
                        source_path="/tmp/fast-warm.json",
                    )
                ],
            ),
        ],
    )

    comparison = compare_result_indexes([index], "lowest_ttft")

    assert comparison.entries[0].run_id == "fast"
    assert comparison.entries[1].run_id == "slow"


def test_format_comparison_markdown_includes_ranking_and_group_winners() -> None:
    index = ExperimentResultIndex(
        generated_at="2026-03-27T00:00:00Z",
        suite_id="suite-a",
        catalog_root="/tmp/catalog",
        results=[
            ExperimentResultRecord(
                run_id="fast",
                suite_id="suite-a",
                status="ok",
                compatibility_status="ready",
                engine_id="sglang",
                hardware_id="a100_80gb",
                gpu_count=1,
                model_id="model-a",
                workload_id="mixed",
                benchmark_profile_id="provision_full",
                runtime_preset_id="baseline",
                manifest_path="/tmp/fast.json",
                warm_summaries=[
                    WarmMetricSummary(
                        cache_reuse_mode="affinity",
                        workload="mixed",
                        ttft_p50_ms=300,
                        aggregate_total_tok_s_p50=400,
                        tpot_p50_ms=10,
                        source_path="/tmp/fast-warm.json",
                    )
                ],
            )
        ],
    )

    comparison = compare_result_indexes([index], "lowest_ttft")
    markdown = format_comparison_markdown(comparison, top_k=5)

    assert "# Benchmark Comparison: lowest_ttft" in markdown
    assert "## Overall Ranking" in markdown
    assert "`fast`" in markdown
    assert "## Winners By Model / Hardware / Workload" in markdown
    assert "model=model-a hardware=a100_80gb workload=mixed" in markdown
