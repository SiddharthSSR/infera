"""Tests for benchmark result comparison."""

from __future__ import annotations

import json

from infera_bench.results import (
    compare_result_indexes,
    format_comparison_markdown,
    summarize_warm_output,
)
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


def test_warm_summary_exposes_cost_units_and_accuracy(tmp_path) -> None:
    path = tmp_path / "warm.json"
    path.write_text(
        json.dumps(
            {
                "presets": {
                    "short": [
                        {
                            "ttft_ms": 10,
                            "stream_total_ms": 20,
                            "non_stream_total_ms": 30,
                            "cost_per_request_usd": 0.002,
                            "cost_query_usd": 99.0,
                            "cost_per_paired_sample_usd": 0.004,
                            "cost_per_token_usd": 0.00001,
                            "cost_per_1m_tokens_usd": 10.0,
                            "cost_accuracy": "estimated",
                            "cost_token_accuracy": "estimated",
                            "cost_attribution_method": "active_instance_group_time_share_v1",
                        }
                    ]
                }
            }
        ),
        encoding="utf-8",
    )

    summary = summarize_warm_output(path, "none")

    assert summary.cost_per_request_usd == 0.002
    assert summary.cost_per_paired_sample_usd == 0.004
    assert summary.cost_per_token_usd == 0.00001
    assert summary.cost_per_1m_tokens_usd == 10.0
    assert summary.cost_accuracy == "estimated"
    assert summary.cost_token_accuracy == "estimated"


def test_warm_summary_reads_legacy_cost_query_alias(tmp_path) -> None:
    path = tmp_path / "legacy-warm.json"
    path.write_text(
        json.dumps({"presets": {"short": [{"cost_query_usd": 0.003}]}}),
        encoding="utf-8",
    )

    assert summarize_warm_output(path, "none").cost_per_request_usd == 0.003


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
