"""Hermes tool definitions and mock sample outputs."""

from __future__ import annotations

from typing import Any

from .models import Attachment, RunMode

CURRENT_HERMES_TOOL_ORDER = [
    "list_models",
    "list_workers",
    "get_gateway_stats",
    "list_instances",
    "list_deployments",
    "get_provider_status",
    "get_usage_summary",
    "get_quota_status",
    "web_search",
    "vision_analyze",
]


CURRENT_HERMES_TOOL_MODES: dict[str, list[RunMode]] = {
    "list_models": ["operations", "research", "multimodal"],
    "list_workers": ["operations"],
    "get_gateway_stats": ["operations"],
    "list_instances": ["operations"],
    "list_deployments": ["operations"],
    "get_provider_status": ["operations"],
    "get_usage_summary": ["operations"],
    "get_quota_status": ["operations"],
    "web_search": ["research"],
    "vision_analyze": ["multimodal"],
}


CURRENT_HERMES_TOOL_DESCRIPTIONS: dict[str, str] = {
    "list_models": "List model ids and loaded/vault metadata available in Infera.",
    "list_workers": "List worker runtime health, loaded models, queue depth, latency, and memory statistics.",
    "get_gateway_stats": "Read the aggregated gateway statistics snapshot for workers, requests, latency, GPU, and memory.",
    "list_instances": "List provisioned instances visible to the current workspace.",
    "list_deployments": "List recent deployment attempts visible to the current workspace.",
    "get_provider_status": "Read provider connectivity, quota, and capability status for the current workspace.",
    "get_usage_summary": "Read current-month workspace totals plus a 7-day daily trend for requests, tokens, successes, and errors.",
    "get_quota_status": "Read workspace quota settings and the current request or token pressure against this month's usage.",
    "web_search": "Search allowlisted official sources for external docs, status pages, or release notes.",
    "vision_analyze": "Inspect an uploaded screenshot attachment.",
}


def build_tool_result(
    tool_name: str, arguments: dict[str, Any], attachment: Attachment | None
) -> Any:
    """Return stable mock tool output for a given Hermes tool."""

    if tool_name == "list_models":
        return [
            {"id": "Qwen/Qwen2.5-7B-Instruct", "loaded": True, "owned_by": "infera"},
            {"id": "meta-llama/Meta-Llama-3.1-8B-Instruct", "loaded": False, "owned_by": "infera"},
        ]
    if tool_name == "list_workers":
        return [
            {
                "worker_id": "worker-1",
                "status": "healthy",
                "queue_depth": 0,
                "active_requests": 1,
                "memory_used_bytes": 2147483648,
                "memory_total_bytes": 8589934592,
                "models": ["Qwen/Qwen2.5-7B-Instruct"],
            }
        ]
    if tool_name == "get_gateway_stats":
        return {
            "healthy_workers": 1,
            "requests_per_second": 2.4,
            "avg_latency_ms": 215,
            "memory_utilization_ratio": 0.25,
        }
    if tool_name == "list_instances":
        return [
            {
                "id": "inst_1",
                "provider": "runpod",
                "status": "running",
                "gpu_type": "A100",
                "models": ["Qwen/Qwen2.5-7B-Instruct"],
            }
        ]
    if tool_name == "list_deployments":
        return [
            {
                "id": "dep_1",
                "status": "succeeded",
                "selected_model_name": "Qwen2.5 7B Instruct",
                "created_at": "2026-04-08T06:00:00Z",
                "limit": arguments.get("limit", 25),
            }
        ]
    if tool_name == "get_provider_status":
        return [
            {
                "provider": "runpod",
                "connected": True,
                "quota_remaining": 12,
                "error": "",
            }
        ]
    if tool_name == "get_usage_summary":
        return {
            "current_month": {"requests": 128, "tokens": 48000, "successes": 122, "errors": 6},
            "last_7_days": [
                {
                    "date": "2026-04-02",
                    "requests": 14,
                    "tokens": 5000,
                    "successes": 14,
                    "errors": 0,
                },
                {
                    "date": "2026-04-03",
                    "requests": 18,
                    "tokens": 6200,
                    "successes": 17,
                    "errors": 1,
                },
            ],
        }
    if tool_name == "get_quota_status":
        return {
            "request_limit": 1000,
            "token_limit": 250000,
            "current_request_pressure": 0.128,
            "current_token_pressure": 0.192,
        }
    if tool_name == "web_search":
        query = str(arguments.get("query", "")).strip() or "RunPod status page"
        return {
            "query": query,
            "topic": str(arguments.get("topic", "")).strip(),
            "results": [
                {
                    "title": "RunPod Status",
                    "url": "https://status.runpod.io/",
                    "domain": "status.runpod.io",
                    "snippet": "Official platform status page.",
                }
            ],
        }
    if tool_name == "vision_analyze":
        attachment_payload = None
        if attachment is not None:
            attachment_payload = {
                "id": attachment.id,
                "file_name": attachment.file_name,
                "mime_type": attachment.mime_type,
                "size_bytes": attachment.size_bytes,
                "width": attachment.width,
                "height": attachment.height,
            }
        return {
            "attachment": attachment_payload,
            "focus": str(arguments.get("focus", "")).strip(),
            "question": str(arguments.get("question", "")).strip(),
            "ocr_available": True,
            "ocr_text": "Cluster healthy. Queue depth 0. No active incidents.",
            "summary": "The screenshot looks like a healthy runtime dashboard with no obvious errors.",
        }
    raise KeyError(f"Unsupported mock tool: {tool_name}")
