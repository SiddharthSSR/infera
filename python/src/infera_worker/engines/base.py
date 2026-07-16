"""Shared engine utilities for startup hooks, prompt building, and cache diagnostics."""

from __future__ import annotations

import asyncio
import os
import threading
from collections.abc import Callable
from pathlib import Path
from typing import Any

import structlog

from ..config import ModelConfig, WorkerConfig
from ..engine import InferenceEngine
from ..types import FinishReason, InferenceRequest, Message, Role

logger = structlog.get_logger()

_TOKENIZER_UNINITIALIZED = object()


class BaseInferenceEngine(InferenceEngine):
    """Common startup hooks and resource helpers for engine implementations."""

    def __init__(self, config: WorkerConfig) -> None:
        self.config = config
        self.active_requests: set[str] = set()
        self._startup_stage_recorder: Callable[[str], None] | None = None
        self._startup_metadata_recorder: Callable[[str, dict[str, Any]], None] | None = None

    def set_startup_stage_recorder(
        self,
        recorder: Callable[[str], None] | None,
    ) -> None:
        self._startup_stage_recorder = recorder

    def set_startup_metadata_recorder(
        self,
        recorder: Callable[[str, dict[str, Any]], None] | None,
    ) -> None:
        self._startup_metadata_recorder = recorder

    def _record_stage(self, stage: str) -> None:
        if self._startup_stage_recorder is not None:
            self._startup_stage_recorder(stage)

    def _record_metadata(self, key: str, payload: dict[str, Any]) -> None:
        if self._startup_metadata_recorder is not None:
            self._startup_metadata_recorder(key, payload)

    def _record_model_cache_probe(
        self, model_config: ModelConfig, model_path: str
    ) -> dict[str, Any]:
        probe = self._collect_model_cache_probe(model_config.model_id, model_path)
        self._record_metadata("model_loads", {model_config.model_id: probe})
        logger.info(
            "Model load cache probe",
            model_id=model_config.model_id,
            model_path=model_path,
            model_source=probe["model_source"],
            local_model_path_exists=probe["local_model_path_exists"],
            inferred_hf_repo_cache_exists=probe["inferred_hf_repo_cache_exists"],
            inferred_hf_snapshot_count=probe["inferred_hf_snapshot_count"],
        )
        return probe

    def _collect_model_cache_probe(self, model_id: str, model_path: str) -> dict[str, Any]:
        """Collect lightweight cache/path diagnostics for startup analysis."""
        resolved_model_path = Path(model_path).expanduser()
        local_model_path_exists = resolved_model_path.exists()
        local_model_path_is_dir = resolved_model_path.is_dir()
        local_model_config_exists = (
            (resolved_model_path / "config.json").exists() if local_model_path_is_dir else False
        )
        local_tokenizer_config_exists = (
            (resolved_model_path / "tokenizer_config.json").exists()
            if local_model_path_is_dir
            else False
        )

        huggingface_hub_cache = (
            os.getenv("HUGGINGFACE_HUB_CACHE")
            or os.getenv("TRANSFORMERS_CACHE")
            or self._default_huggingface_hub_cache()
        )
        inferred_repo_cache_dir = self._infer_huggingface_repo_cache_dir(
            model_path, huggingface_hub_cache
        )
        inferred_repo_cache_exists = (
            inferred_repo_cache_dir.exists() if inferred_repo_cache_dir is not None else False
        )
        inferred_snapshot_dir = self._latest_snapshot_dir(inferred_repo_cache_dir)
        inferred_snapshot_count = self._snapshot_count(inferred_repo_cache_dir)

        return {
            "model_id": model_id,
            "requested_model_path": model_path,
            "model_source": "local_path" if local_model_path_exists else "huggingface_repo",
            "local_model_path": str(resolved_model_path),
            "local_model_path_exists": local_model_path_exists,
            "local_model_path_is_dir": local_model_path_is_dir,
            "local_model_has_config_json": local_model_config_exists,
            "local_model_has_tokenizer_config_json": local_tokenizer_config_exists,
            "cache_dirs": {
                "hf_home": os.getenv("HF_HOME", ""),
                "huggingface_hub": huggingface_hub_cache or "",
                "transformers": os.getenv("TRANSFORMERS_CACHE", ""),
                "torch": os.getenv("TORCH_HOME", ""),
            },
            "inferred_hf_repo_cache_dir": str(inferred_repo_cache_dir)
            if inferred_repo_cache_dir is not None
            else None,
            "inferred_hf_repo_cache_exists": inferred_repo_cache_exists,
            "inferred_hf_snapshot_count": inferred_snapshot_count,
            "inferred_latest_snapshot_dir": str(inferred_snapshot_dir)
            if inferred_snapshot_dir is not None
            else None,
            "inferred_latest_snapshot_has_config_json": (
                (inferred_snapshot_dir / "config.json").exists()
                if inferred_snapshot_dir is not None
                else False
            ),
            "inferred_latest_snapshot_has_tokenizer_config_json": (
                (inferred_snapshot_dir / "tokenizer_config.json").exists()
                if inferred_snapshot_dir is not None
                else False
            ),
        }

    def _default_huggingface_hub_cache(self) -> str:
        hf_home = os.getenv("HF_HOME", "")
        if not hf_home:
            return ""
        return str(Path(hf_home).expanduser() / "hub")

    def _infer_huggingface_repo_cache_dir(self, model_path: str, hub_cache: str) -> Path | None:
        if not hub_cache or "/" not in model_path:
            return None
        normalized_repo = model_path.replace("/", "--")
        return Path(hub_cache).expanduser() / f"models--{normalized_repo}"

    def _latest_snapshot_dir(self, repo_cache_dir: Path | None) -> Path | None:
        if repo_cache_dir is None:
            return None
        snapshots_dir = repo_cache_dir / "snapshots"
        if not snapshots_dir.exists() or not snapshots_dir.is_dir():
            return None
        snapshot_dirs = sorted(
            (path for path in snapshots_dir.iterdir() if path.is_dir()), key=lambda path: path.name
        )
        if not snapshot_dirs:
            return None
        return snapshot_dirs[-1]

    def _snapshot_count(self, repo_cache_dir: Path | None) -> int:
        if repo_cache_dir is None:
            return 0
        snapshots_dir = repo_cache_dir / "snapshots"
        if not snapshots_dir.exists() or not snapshots_dir.is_dir():
            return 0
        return sum(1 for path in snapshots_dir.iterdir() if path.is_dir())

    def get_memory_usage(self) -> tuple[int, int]:
        try:
            import pynvml

            pynvml.nvmlInit()
            handle = pynvml.nvmlDeviceGetHandleByIndex(0)
            mem = pynvml.nvmlDeviceGetMemoryInfo(handle)
            return int(mem.used), int(mem.total)
        except Exception:
            pass

        try:
            import torch

            if torch.cuda.is_available():
                used = max(torch.cuda.memory_allocated(), torch.cuda.memory_reserved())
                total = torch.cuda.get_device_properties(0).total_memory
                return used, total
        except ImportError:
            pass

        return 0, 0

    def _map_finish_reason(self, reason: str | None) -> FinishReason:
        if reason is None:
            return FinishReason.STOP
        normalized = str(reason).strip().lower()
        reason_map = {
            "stop": FinishReason.STOP,
            "length": FinishReason.LENGTH,
            "abort": FinishReason.ERROR,
            "cancelled": FinishReason.ERROR,
            "error": FinishReason.ERROR,
            "tool_calls": FinishReason.TOOL_CALLS,
        }
        return reason_map.get(normalized, FinishReason.STOP)


class TokenizerPromptEngine(BaseInferenceEngine):
    """Base engine for runtimes that need lazy tokenizer prompt building."""

    def __init__(self, config: WorkerConfig) -> None:
        super().__init__(config)
        self.tokenizers: dict[str, Any] = {}
        self.model_paths: dict[str, str] = {}
        self._tokenizer_lock = threading.RLock()

    def _resolve_model_path(self, model_config: ModelConfig) -> str:
        return model_config.model_path or model_config.model_id

    def _register_model_path(self, model_id: str, model_path: str) -> None:
        self.model_paths[model_id] = model_path
        self.tokenizers[model_id] = _TOKENIZER_UNINITIALIZED

    def _clear_model_path(self, model_id: str) -> None:
        self.model_paths.pop(model_id, None)
        self.tokenizers.pop(model_id, None)

    async def warm_model_runtime(self, model_id: str) -> None:
        if model_id not in self.model_paths:
            return
        self._record_stage("tokenizer_warmup_started")
        await asyncio.to_thread(self._get_tokenizer, model_id)
        self._record_stage("tokenizer_warmup_finished")

    def _get_tokenizer(self, model_id: str) -> Any:
        tokenizer = self.tokenizers.get(model_id)
        if tokenizer is not _TOKENIZER_UNINITIALIZED:
            return tokenizer

        with self._tokenizer_lock:
            tokenizer = self.tokenizers.get(model_id)
            if tokenizer is not _TOKENIZER_UNINITIALIZED:
                return tokenizer

            model_path = self.model_paths.get(model_id)
            if not model_path:
                self.tokenizers[model_id] = None
                return None

            self._record_stage("tokenizer_load_started")
            try:
                from transformers import AutoTokenizer

                if self.config.trust_remote_code:
                    logger.warning(
                        "Loading tokenizer with trust_remote_code enabled",
                        model_id=model_id,
                        model_path=model_path,
                    )
                tokenizer = AutoTokenizer.from_pretrained(
                    model_path,
                    trust_remote_code=self.config.trust_remote_code,
                )
            except Exception:
                tokenizer = None
            self.tokenizers[model_id] = tokenizer
            self._record_stage("tokenizer_load_finished")
            return tokenizer

    def _count_tokens_from_text(self, model_id: str, text: str) -> int | None:
        if not text:
            return 0
        tokenizer = self._get_tokenizer(model_id)
        if tokenizer is None:
            return None
        try:
            if hasattr(tokenizer, "encode"):
                token_ids = tokenizer.encode(text, add_special_tokens=False)
                if isinstance(token_ids, list):
                    return len(token_ids)
            encoded = tokenizer(text, add_special_tokens=False)
            if isinstance(encoded, dict):
                token_ids = encoded.get("input_ids")
            else:
                token_ids = getattr(encoded, "input_ids", None)
            if token_ids is None:
                return None
            return len(token_ids)
        except Exception:
            return None

    def _count_prompt_tokens_from_prompt(
        self, model_id: str, prompt: str, request: InferenceRequest
    ) -> int:
        count = self._count_tokens_from_text(model_id, prompt)
        if count is not None:
            return count
        return request.token_estimate()

    def _count_completion_tokens(self, model_id: str, text: str) -> int:
        count = self._count_tokens_from_text(model_id, text)
        if count is not None:
            return count
        return max(1, len(text) // 4) if text else 0

    def _template_message(self, msg: Message) -> dict[str, Any]:
        name = getattr(msg, "name", None)
        tool_calls = getattr(msg, "tool_calls", None)
        tool_call_id = getattr(msg, "tool_call_id", None)
        payload: dict[str, Any] = {
            "role": msg.role.value,
            "content": msg.content or "",
        }
        if name:
            payload["name"] = name
        if tool_calls:
            payload["tool_calls"] = [
                {
                    "id": tool_call.id,
                    "type": tool_call.type,
                    "function": {
                        "name": tool_call.function.name,
                        "arguments": tool_call.function.arguments,
                    },
                }
                for tool_call in tool_calls
            ]
        if tool_call_id:
            payload["tool_call_id"] = tool_call_id
        return payload

    def _template_messages(self, request: InferenceRequest) -> list[dict[str, Any]]:
        return [self._template_message(msg) for msg in request.messages]

    def _apply_chat_template(
        self,
        tokenizer: Any,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: Any = None,
    ) -> str:
        base_kwargs: dict[str, Any] = {
            "tokenize": False,
            "add_generation_prompt": True,
        }
        kwargs = dict(base_kwargs)
        if tools is not None:
            kwargs["tools"] = tools
        if tool_choice is not None:
            kwargs["tool_choice"] = tool_choice

        try:
            return tokenizer.apply_chat_template(messages, **kwargs)
        except TypeError as exc:
            message = str(exc)
            unsupported_tools = (
                "tools" in kwargs and "tools" in message and "unexpected keyword" in message
            )
            unsupported_choice = (
                "tool_choice" in kwargs
                and "tool_choice" in message
                and "unexpected keyword" in message
            )
            if not unsupported_tools and not unsupported_choice:
                raise
            return tokenizer.apply_chat_template(messages, **base_kwargs)

    def _build_prompt(self, request: InferenceRequest) -> str:
        messages = self._template_messages(request)

        tokenizer = self._get_tokenizer(request.model_id)
        if tokenizer is not None and hasattr(tokenizer, "apply_chat_template"):
            try:
                prompt = self._apply_chat_template(tokenizer, messages)
                return prompt
            except Exception:
                pass

        parts: list[str] = []
        system_prompt = ""

        i = 0
        while i < len(request.messages):
            msg = request.messages[i]

            if msg.role == Role.SYSTEM:
                system_prompt = msg.content
                i += 1
                continue

            if msg.role == Role.USER:
                user_content = msg.content
                if system_prompt:
                    user_content = f"{system_prompt}\n\n{user_content}"
                    system_prompt = ""

                assistant_content = ""
                if i + 1 < len(request.messages) and request.messages[i + 1].role == Role.ASSISTANT:
                    assistant_content = request.messages[i + 1].content
                    i += 1

                if assistant_content:
                    parts.append(f"<s>[INST] {user_content} [/INST] {assistant_content}</s>")
                else:
                    parts.append(f"<s>[INST] {user_content} [/INST]")

            i += 1

        return "".join(parts)

    def _build_prompt_with_tools(self, request: InferenceRequest) -> str:
        if not request.tools:
            return self._build_prompt(request)

        tokenizer = self._get_tokenizer(request.model_id)
        if tokenizer is not None and hasattr(tokenizer, "apply_chat_template"):
            tools_schema = [
                {"type": tool.type, "function": tool.function} for tool in request.tools
            ]
            try:
                return self._apply_chat_template(
                    tokenizer,
                    self._template_messages(request),
                    tools=tools_schema,
                    tool_choice=request.tool_choice,
                )
            except Exception as exc:
                logger.warning(
                    "apply_chat_template with tools failed, falling back to base prompt",
                    error=str(exc),
                    model_id=request.model_id,
                )

        return self._build_prompt(request)

    def _build_response_message(self, text: str) -> Message:
        return Message(role=Role.ASSISTANT, content=text)
