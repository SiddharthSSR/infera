"""HTTP server for Infera Worker - simpler than gRPC for vertical slice."""

import asyncio
import json
import os
import time
from datetime import datetime
from typing import Any

import httpx
import structlog
from aiohttp import web
from prometheus_client import (
    CONTENT_TYPE_LATEST,
    CollectorRegistry,
    Counter,
    Gauge,
    Histogram,
    generate_latest,
)

from .config import ModelConfig, WorkerConfig
from .types import (
    FunctionCall,
    InferenceParameters,
    InferenceRequest,
    Message,
    Priority,
    Role,
    ToolCall,
    ToolDefinition,
    WorkerState,
)
from .worker import Worker

logger = structlog.get_logger()


def build_gateway_url(router_address: str, path: str) -> str:
    """Build a gateway URL with proper protocol.

    Uses HTTPS for ngrok and other public URLs, HTTP for localhost.
    """
    if not router_address:
        return ""

    # If protocol is already included, use it
    if router_address.startswith("https://") or router_address.startswith("http://"):
        return f"{router_address}{path}"

    # Default to HTTPS for public URLs (ngrok, etc.), HTTP for localhost
    if "localhost" in router_address or "127.0.0.1" in router_address:
        return f"http://{router_address}{path}"
    else:
        return f"https://{router_address}{path}"


class HTTPServer:
    """HTTP server for the worker."""

    def __init__(self, worker: Worker, config: WorkerConfig) -> None:
        self.worker = worker
        self.config = config
        self.app = web.Application()
        self.runner: web.AppRunner | None = None
        self._gateway_client: httpx.AsyncClient | None = None
        self._registration_task: asyncio.Task | None = None
        self._heartbeat_task: asyncio.Task | None = None
        self._consecutive_auth_failures = 0
        self._gateway_registered = False
        self._metrics_registry = CollectorRegistry()
        self._setup_metrics()
        self._setup_routes()

    def _setup_routes(self) -> None:
        """Set up HTTP routes."""
        self.app.router.add_post("/infer", self.handle_infer)
        self.app.router.add_post("/infer/stream", self.handle_infer_stream)
        self.app.router.add_get("/health", self.handle_health)
        self.app.router.add_get("/metrics", self.handle_metrics)
        self.app.router.add_get("/models", self.handle_list_models)
        self.app.router.add_post("/models/load", self.handle_load_model)
        self.app.router.add_post("/models/unload", self.handle_unload_model)
        self.app.router.add_get("/stats", self.handle_stats)

    def _setup_metrics(self) -> None:
        """Initialize Prometheus metrics for worker observability."""
        self._inference_requests = Counter(
            "infera_worker_inference_requests_total",
            "Total inference requests handled by the worker.",
            ["stream", "status"],
            registry=self._metrics_registry,
        )
        self._inference_duration = Histogram(
            "infera_worker_inference_duration_seconds",
            "Inference request duration in seconds.",
            ["stream", "status"],
            buckets=(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120),
            registry=self._metrics_registry,
        )
        self._inference_tokens = Counter(
            "infera_worker_inference_tokens_total",
            "Total tokens produced/used in inference responses.",
            registry=self._metrics_registry,
        )
        self._loaded_models_gauge = Gauge(
            "infera_worker_loaded_models",
            "Number of models currently loaded in the worker.",
            registry=self._metrics_registry,
        )
        self._gpu_utilization_gauge = Gauge(
            "infera_worker_gpu_utilization",
            "Current GPU utilization percentage.",
            registry=self._metrics_registry,
        )
        self._memory_used_gauge = Gauge(
            "infera_worker_memory_used_bytes",
            "Current GPU memory used in bytes.",
            registry=self._metrics_registry,
        )
        self._memory_total_gauge = Gauge(
            "infera_worker_memory_total_bytes",
            "Current GPU memory total in bytes.",
            registry=self._metrics_registry,
        )
        self._gateway_registration = Counter(
            "infera_worker_gateway_registration_total",
            "Gateway registration attempts by result status.",
            ["status"],
            registry=self._metrics_registry,
        )
        self._gateway_heartbeats = Counter(
            "infera_worker_gateway_heartbeats_total",
            "Gateway heartbeat attempts by result status.",
            ["status"],
            registry=self._metrics_registry,
        )
        self._worker_info = Gauge(
            "infera_worker_info",
            "Static worker metadata.",
            ["worker_id", "engine", "provider", "env", "version"],
            registry=self._metrics_registry,
        )
        self._worker_info.labels(
            worker_id=self.worker.worker_id,
            engine=self.config.engine,
            provider=self._runtime_provider(),
            env=self._runtime_env(),
            version=self._runtime_version(),
        ).set(1)

    def _worker_tags(self) -> dict[str, str]:
        """Build stable worker metadata sent to the gateway."""
        tags = {
            "provider": self._runtime_provider(),
            "engine": self.config.engine,
            "env": self._runtime_env(),
            "version": self._runtime_version(),
        }

        if runpod_pod_id := os.environ.get("RUNPOD_POD_ID"):
            tags["instance_id"] = runpod_pod_id
            tags["provider_id"] = runpod_pod_id

        return tags

    def _record_gateway_registration(self, status: str) -> None:
        self._gateway_registration.labels(status=status).inc()

    def _record_gateway_heartbeat(self, status: str) -> None:
        self._gateway_heartbeats.labels(status=status).inc()

    def _record_inference_metrics(
        self,
        *,
        stream: bool,
        status: str,
        duration_seconds: float,
        token_count: int = 0,
    ) -> None:
        """Record inference metrics for a completed request."""
        stream_label = "true" if stream else "false"
        status_label = status or "unknown"
        self._inference_requests.labels(stream=stream_label, status=status_label).inc()
        self._inference_duration.labels(stream=stream_label, status=status_label).observe(
            max(duration_seconds, 0.0)
        )
        if token_count > 0:
            self._inference_tokens.inc(token_count)

    def _refresh_runtime_gauges(self) -> None:
        """Refresh worker resource gauges from runtime stats."""
        stats = self.worker.get_stats()
        self._loaded_models_gauge.set(len(self.worker.get_loaded_models()))
        self._gpu_utilization_gauge.set(stats.gpu_utilization)
        self._memory_used_gauge.set(stats.memory_used_bytes)
        self._memory_total_gauge.set(stats.memory_total_bytes)

    def _ensure_gateway_client(self) -> httpx.AsyncClient:
        """Return a reusable client for gateway control-plane calls."""
        if self._gateway_client is None or self._gateway_client.is_closed:
            self._gateway_client = httpx.AsyncClient()
        return self._gateway_client

    async def start(self) -> None:
        """Start the HTTP server."""
        self.runner = web.AppRunner(self.app)
        await self.runner.setup()

        site = web.TCPSite(self.runner, "0.0.0.0", self.config.http_port)
        await site.start()
        self.worker.record_startup_stage("server_started")

        logger.info("HTTP server started", port=self.config.http_port)

        if self._worker_ready_for_gateway():
            try:
                await self.activate_gateway_reporting()
            except Exception:
                if self.runner:
                    await self.runner.cleanup()
                    self.runner = None
                raise

    async def stop(self) -> None:
        """Stop the HTTP server."""
        # Cancel background tasks
        tasks = []
        if self._registration_task:
            self._registration_task.cancel()
            tasks.append(self._registration_task)
        if self._heartbeat_task:
            self._heartbeat_task.cancel()
            tasks.append(self._heartbeat_task)
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)

        # Deregister only if registration succeeded.
        if self._gateway_registered:
            await self._deregister_from_gateway()
        if self._gateway_client is not None:
            await self._gateway_client.aclose()
            self._gateway_client = None

        if self.runner:
            await self.runner.cleanup()
            logger.info("HTTP server stopped")

    def _worker_ready_for_gateway(self) -> bool:
        """Return true when the worker is ready to be registered for traffic."""
        return self.worker.get_state() in {WorkerState.READY, WorkerState.BUSY}

    async def activate_gateway_reporting(self) -> None:
        """Register a ready worker with the gateway and start heartbeats."""
        if not self.config.router_address or self._gateway_registered:
            return
        if not self._worker_ready_for_gateway():
            return

        await self._register_with_gateway()
        self._gateway_registered = True
        self.worker.record_startup_stage("gateway_registered")
        self._heartbeat_task = asyncio.create_task(self._heartbeat_loop())

    async def _register_with_gateway(self) -> None:
        """Register this worker with the gateway."""
        gateway_url = build_gateway_url(self.config.router_address, "/api/workers/register")

        # Get public IP/hostname for the gateway to reach us
        worker_address = self._get_worker_address()

        registration_data = {
            "worker_id": self.worker.worker_id,
            "address": worker_address,
            "status": "healthy",
            "tags": self._worker_tags(),
            "loaded_models": [
                {
                    "model_id": m.model_id,
                    "version": m.version,
                    "memory_bytes": m.memory_bytes,
                    "max_batch_size": 0,
                    "max_sequence_length": 0,
                }
                for m in self.worker.get_loaded_models()
            ],
            "gpu_type": os.environ.get("RUNPOD_GPU_TYPE", "unknown"),
            "gpu_count": int(os.environ.get("RUNPOD_GPU_COUNT", "1")),
        }

        max_retries = 10
        retry_delay = 5

        for attempt in range(max_retries):
            try:
                client = self._ensure_gateway_client()
                response = await client.post(
                    gateway_url,
                    json=registration_data,
                    headers=self.config.gateway_headers(),
                    timeout=10.0,
                )

                if response.status_code == 200:
                    self._record_gateway_registration("success")
                    logger.info(
                        "Registered with gateway",
                        gateway=self.config.router_address,
                        worker_id=self.worker.worker_id,
                    )
                    return
                elif response.status_code in (401, 403):
                    self._record_gateway_registration("auth_rejected")
                    logger.error(
                        "Gateway registration rejected by auth",
                        status=response.status_code,
                        response=response.text,
                        gateway=self.config.router_address,
                        gateway_url=gateway_url,
                        worker_id=self.worker.worker_id,
                    )
                    raise RuntimeError("Gateway auth rejected worker registration")
                elif 400 <= response.status_code < 500:
                    self._record_gateway_registration("client_error")
                    logger.error(
                        "Gateway registration failed with non-retriable client error",
                        status=response.status_code,
                        response=response.text,
                        gateway=self.config.router_address,
                        gateway_url=gateway_url,
                        worker_id=self.worker.worker_id,
                    )
                    raise RuntimeError("Gateway registration failed with non-retriable client error")
                else:
                    self._record_gateway_registration("http_error")
                    logger.warning(
                        "Gateway registration failed",
                        status=response.status_code,
                        response=response.text,
                    )

            except RuntimeError:
                raise
            except Exception as e:
                self._record_gateway_registration("exception")
                logger.warning(
                    "Gateway registration attempt failed",
                    attempt=attempt + 1,
                    error=str(e),
                )

            await asyncio.sleep(retry_delay)

        logger.error("Failed to register with gateway after max retries")
        raise RuntimeError("Failed to register with gateway after max retries")

    async def _deregister_from_gateway(self) -> None:
        """Deregister this worker from the gateway."""
        if not self.config.router_address:
            return

        gateway_url = build_gateway_url(
            self.config.router_address,
            f"/api/workers/{self.worker.worker_id}"
        )

        try:
            client = self._ensure_gateway_client()
            resp = await client.delete(
                gateway_url,
                headers=self.config.gateway_headers(),
                timeout=5.0,
            )
            if resp.is_error:
                logger.error(
                    "Failed to deregister from gateway",
                    status=resp.status_code,
                    response=resp.text,
                )
                self._gateway_registered = False
                return
            logger.info("Deregistered from gateway")
            self._gateway_registered = False
        except Exception as e:
            self._gateway_registered = False
            logger.warning("Failed to deregister from gateway", error=str(e))

    async def _heartbeat_loop(self) -> None:
        """Send periodic heartbeats to the gateway."""
        gateway_url = build_gateway_url(
            self.config.router_address,
            "/api/workers/heartbeat"
        )
        interval = self.config.health_report_interval_ms / 1000.0

        while True:
            try:
                await asyncio.sleep(interval)

                stats = self.worker.get_stats()
                heartbeat_data = {
                    "worker_id": self.worker.worker_id,
                    "status": self.worker.get_state().value,
                    "stats": {
                        "queue_depth": stats.queue_depth,
                        "active_requests": stats.active_requests,
                        "gpu_utilization": stats.gpu_utilization,
                        "memory_used_bytes": stats.memory_used_bytes,
                        "memory_total_bytes": stats.memory_total_bytes,
                        "requests_per_second": stats.requests_per_second,
                        "avg_latency_ms": stats.avg_latency_ms,
                        "p50_latency_ms": stats.p50_latency_ms,
                        "p99_latency_ms": stats.p99_latency_ms,
                        "error_rate": stats.error_rate,
                    },
                    "loaded_models": [
                        {"model_id": m.model_id, "version": m.version}
                        for m in self.worker.get_loaded_models()
                    ],
                }

                client = self._ensure_gateway_client()
                response = await client.post(
                    gateway_url,
                    json=heartbeat_data,
                    headers=self.config.gateway_headers(),
                    timeout=5.0,
                )
                if response.status_code == 200:
                    self._record_gateway_heartbeat("success")
                    self._consecutive_auth_failures = 0
                    logger.debug("Heartbeat sent successfully")
                elif response.status_code in (401, 403):
                    self._record_gateway_heartbeat("auth_rejected")
                    self._consecutive_auth_failures += 1
                    logger.warning(
                        "Heartbeat rejected by gateway auth",
                        status=response.status_code,
                        response=response.text,
                        gateway_url=gateway_url,
                        worker_id=self.worker.worker_id,
                        consecutive_failures=self._consecutive_auth_failures,
                    )
                    # Warn at halfway point so operators can act before shutdown.
                    if self._consecutive_auth_failures == 5:
                        logger.error(
                            "Sustained heartbeat auth failures — worker will shut down after 10 consecutive failures",
                            worker_id=self.worker.worker_id,
                            gateway_url=gateway_url,
                        )
                    if self._consecutive_auth_failures >= 10:
                        logger.error(
                            "Stopping heartbeat loop after repeated auth failures",
                            worker_id=self.worker.worker_id,
                            failures=self._consecutive_auth_failures,
                            gateway_url=gateway_url,
                        )
                        self.worker.request_shutdown()
                        break
                else:
                    self._record_gateway_heartbeat("http_error")
                    self._consecutive_auth_failures = 0
                    logger.warning(
                        "Heartbeat failed with non-200 response",
                        status=response.status_code,
                        response=response.text,
                        gateway_url=gateway_url,
                        worker_id=self.worker.worker_id,
                    )

            except asyncio.CancelledError:
                break
            except Exception as e:
                self._record_gateway_heartbeat("exception")
                logger.debug("Heartbeat failed", error=str(e))

    def _get_worker_address(self) -> str:
        """Get the address where the gateway can reach this worker."""
        # Explicit address should always win when operators need to override
        # provider-derived routing.
        explicit_address = os.environ.get("INFERA_WORKER_ADDRESS")
        if explicit_address:
            return explicit_address

        # Check for RunPod pod ID to construct proxy URL
        runpod_pod_id = os.environ.get("RUNPOD_POD_ID")
        if runpod_pod_id:
            return f"{runpod_pod_id}-{self.config.http_port}.proxy.runpod.net"

        # Check for RunPod public IP
        runpod_public_ip = os.environ.get("RUNPOD_PUBLIC_IP")
        if runpod_public_ip:
            return f"{runpod_public_ip}:{self.config.http_port}"

        # Default to localhost for local development
        return f"localhost:{self.config.http_port}"

    def _runtime_provider(self) -> str:
        if os.environ.get("RUNPOD_POD_ID") or os.environ.get("RUNPOD_PUBLIC_IP"):
            return "runpod"
        return "local"

    def _runtime_env(self) -> str:
        raw = os.environ.get("INFERA_ENV", "").strip().lower()
        if raw in {"prod", "production"}:
            return "production"
        if raw in {"stage", "staging"}:
            return "staging"
        if raw in {"", "dev", "development", "local"}:
            return "development"
        return raw

    def _runtime_version(self) -> str:
        return os.environ.get("INFERA_VERSION", "dev")

    async def handle_infer(self, request: web.Request) -> web.Response:
        """Handle non-streaming inference request."""
        start_time = time.perf_counter()
        status = "success"
        token_count = 0
        try:
            data = await request.json()
            inference_request = self._parse_request(data)

            response = await self.worker.infer(inference_request)
            token_count = response.usage.total_tokens

            return web.json_response(self._format_response(response))

        except ValueError as e:
            status = "invalid_request"
            return web.json_response(
                {"error": {"type": "invalid_request", "message": str(e)}},
                status=400
            )
        except RuntimeError as e:
            status = "worker_error"
            return web.json_response(
                {"error": {"type": "worker_error", "message": str(e)}},
                status=503
            )
        except Exception as e:
            status = "internal_error"
            logger.error("Inference error", error=str(e))
            return web.json_response(
                {"error": {"type": "internal_error", "message": str(e)}},
                status=500
            )
        finally:
            self._record_inference_metrics(
                stream=False,
                status=status,
                duration_seconds=time.perf_counter() - start_time,
                token_count=token_count,
            )

    async def handle_infer_stream(self, request: web.Request) -> web.StreamResponse:
        """Handle streaming inference request."""
        start_time = time.perf_counter()
        status = "success"
        token_count = 0

        try:
            data = await request.json()
            inference_request = self._parse_request(data)
            inference_request.stream = True
            stream = self.worker.infer_stream(inference_request)

            response = web.StreamResponse(
                status=200,
                reason="OK",
                headers={
                    "Content-Type": "application/x-ndjson",
                    "Cache-Control": "no-cache",
                    "Connection": "keep-alive",
                }
            )
            await response.prepare(request)

            try:
                async for chunk in stream:
                    chunk_data = {
                        "request_id": chunk.request_id,
                        "index": chunk.index,
                        "delta": chunk.delta,
                    }

                    if chunk.finish_reason is not None:
                        chunk_data["finish_reason"] = chunk.finish_reason.value

                    if chunk.usage is not None:
                        chunk_data["usage"] = {
                            "prompt_tokens": chunk.usage.prompt_tokens,
                            "completion_tokens": chunk.usage.completion_tokens,
                            "total_tokens": chunk.usage.total_tokens,
                        }
                        token_count = max(token_count, chunk.usage.total_tokens)

                    if chunk.tool_calls is not None:
                        chunk_data["tool_calls"] = [
                            {
                                "index": tc.index,
                                "id": tc.id,
                                "type": tc.type,
                                "function": tc.function,
                            }
                            for tc in chunk.tool_calls
                        ]

                    await response.write(json.dumps(chunk_data).encode() + b"\n")
            except ValueError as e:
                status = "invalid_request"
                await response.write(json.dumps({
                    "error": {"type": "invalid_request", "message": str(e)}
                }).encode() + b"\n")
            except RuntimeError as e:
                status = "worker_error"
                await response.write(json.dumps({
                    "error": {"type": "worker_error", "message": str(e)}
                }).encode() + b"\n")
            except Exception as e:
                status = "internal_error"
                logger.error("Streaming error", error=str(e))
                await response.write(json.dumps({
                    "error": {"type": "internal_error", "message": str(e)}
                }).encode() + b"\n")

            await response.write_eof()
            return response
        except ValueError as e:
            status = "invalid_request"
            return web.json_response(
                {"error": {"type": "invalid_request", "message": str(e)}},
                status=400
            )
        except Exception as e:
            status = "internal_error"
            logger.error("Streaming error", error=str(e))
            return web.json_response(
                {"error": {"type": "internal_error", "message": str(e)}},
                status=500
            )
        finally:
            self._record_inference_metrics(
                stream=True,
                status=status,
                duration_seconds=time.perf_counter() - start_time,
                token_count=token_count,
            )

    async def handle_health(self, request: web.Request) -> web.Response:
        """Handle health check."""
        state = self.worker.get_state()
        stats = self.worker.get_stats()
        ready = state in {WorkerState.READY, WorkerState.BUSY}
        live = state not in {WorkerState.ERROR, WorkerState.SHUTTING_DOWN}

        return web.json_response({
            "status": "healthy" if ready else state.value,
            "worker_id": self.worker.worker_id,
            "state": state.value,
            "live": live,
            "ready": ready,
            "draining": state == WorkerState.DRAINING,
            "gateway_registered": self._gateway_registered,
            "startup": self.worker.get_startup_status(),
            "models_loaded": len(self.worker.get_loaded_models()),
            "memory_used_bytes": stats.memory_used_bytes,
            "memory_total_bytes": stats.memory_total_bytes,
        })

    async def handle_metrics(self, request: web.Request) -> web.Response:
        """Expose Prometheus metrics for scraping."""
        self._refresh_runtime_gauges()
        payload = generate_latest(self._metrics_registry)
        return web.Response(
            body=payload,
            headers={"Content-Type": CONTENT_TYPE_LATEST},
        )

    async def handle_list_models(self, request: web.Request) -> web.Response:
        """Handle list models request."""
        models = self.worker.get_loaded_models()

        return web.json_response({
            "models": [
                {
                    "model_id": m.model_id,
                    "version": m.version,
                    "memory_bytes": m.memory_bytes,
                    "max_batch_size": m.max_batch_size,
                    "max_sequence_length": m.max_sequence_length,
                    "loaded_at": m.loaded_at.isoformat(),
                }
                for m in models
            ]
        })

    async def handle_load_model(self, request: web.Request) -> web.Response:
        """Handle load model request."""
        try:
            data = await request.json()
            model_config = ModelConfig(
                model_id=data.get("model_id"),
                model_path=data.get("model_path"),
                revision=data.get("revision"),
                quantization=data.get("quantization"),
                max_batch_size=data.get("max_batch_size", 8),
                max_sequence_length=data.get("max_sequence_length", 4096),
            )

            start_time = datetime.now()
            loaded = await self.worker.load_model(model_config)
            load_time_ms = int((datetime.now() - start_time).total_seconds() * 1000)

            return web.json_response({
                "success": True,
                "model_id": loaded.model_id,
                "memory_bytes": loaded.memory_bytes,
                "load_time_ms": load_time_ms,
            })

        except Exception as e:
            return web.json_response(
                {"success": False, "error": str(e)},
                status=400
            )

    async def handle_unload_model(self, request: web.Request) -> web.Response:
        """Handle unload model request."""
        try:
            data = await request.json()
            model_id = data.get("model_id")

            success = await self.worker.unload_model(model_id)

            return web.json_response({
                "success": success,
                "model_id": model_id,
            })

        except Exception as e:
            return web.json_response(
                {"success": False, "error": str(e)},
                status=400
            )

    async def handle_stats(self, request: web.Request) -> web.Response:
        """Handle stats request."""
        stats = self.worker.get_stats()

        return web.json_response({
            "queue_depth": stats.queue_depth,
            "active_requests": stats.active_requests,
            "gpu_utilization": stats.gpu_utilization,
            "memory_used_bytes": stats.memory_used_bytes,
            "memory_total_bytes": stats.memory_total_bytes,
            "requests_per_second": stats.requests_per_second,
            "avg_latency_ms": stats.avg_latency_ms,
            "p50_latency_ms": stats.p50_latency_ms,
            "p99_latency_ms": stats.p99_latency_ms,
            "error_rate": stats.error_rate,
        })

    def _parse_request(self, data: dict[str, Any]) -> InferenceRequest:
        """Parse request data into InferenceRequest."""

        def _parse_tool_calls(raw_calls: list | None) -> list[ToolCall] | None:
            if not raw_calls:
                return None
            result = []
            for tc in raw_calls:
                fn = tc.get("function", {})
                result.append(ToolCall(
                    id=tc.get("id", ""),
                    type=tc.get("type", "function"),
                    function=FunctionCall(
                        name=fn.get("name", ""),
                        arguments=fn.get("arguments", ""),
                    ),
                ))
            return result or None

        messages = [
            Message(
                role=Role(msg.get("role", "user")),
                content=msg.get("content") or "",
                name=msg.get("name"),
                tool_calls=_parse_tool_calls(msg.get("tool_calls")),
                tool_call_id=msg.get("tool_call_id"),
            )
            for msg in data.get("messages", [])
        ]

        if not messages:
            raise ValueError("messages is required")

        model_id = data.get("model_id")
        if not model_id:
            raise ValueError("model_id is required")

        params_data = data.get("parameters", {})
        parameters = InferenceParameters(
            max_tokens=params_data.get("max_tokens", 256),
            temperature=params_data.get("temperature", 1.0),
            top_p=params_data.get("top_p", 1.0),
            top_k=params_data.get("top_k"),
            stop_sequences=params_data.get("stop_sequences", []),
            presence_penalty=params_data.get("presence_penalty", 0.0),
            frequency_penalty=params_data.get("frequency_penalty", 0.0),
            seed=params_data.get("seed"),
        )

        tools: list[ToolDefinition] | None = None
        raw_tools = data.get("tools")
        if raw_tools:
            tools = [
                ToolDefinition(
                    type=t.get("type", "function"),
                    function=t.get("function", {}),
                )
                for t in raw_tools
            ]

        tool_choice = data.get("tool_choice")

        return InferenceRequest(
            request_id=data.get("request_id", ""),
            model_id=model_id,
            messages=messages,
            parameters=parameters,
            stream=data.get("stream", False),
            priority=Priority(data.get("priority", 2)),
            metadata=data.get("metadata", {}),
            tools=tools,
            tool_choice=tool_choice,
        )

    def _format_response(self, response) -> dict[str, Any]:
        """Format InferenceResponse as dict."""
        choices = []
        for choice in response.choices:
            choice_dict: dict[str, Any] = {
                "index": choice.index,
                "message": {
                    "role": choice.message.role.value,
                    "content": choice.message.content,
                },
                "finish_reason": choice.finish_reason.value,
            }
            if choice.message.tool_calls:
                choice_dict["message"]["tool_calls"] = [
                    {
                        "id": tc.id,
                        "type": tc.type,
                        "function": {
                            "name": tc.function.name,
                            "arguments": tc.function.arguments,
                        },
                    }
                    for tc in choice.message.tool_calls
                ]
            choices.append(choice_dict)

        return {
            "request_id": response.request_id,
            "model_id": response.model_id,
            "choices": choices,
            "usage": {
                "prompt_tokens": response.usage.prompt_tokens,
                "completion_tokens": response.usage.completion_tokens,
                "total_tokens": response.usage.total_tokens,
            },
            "latency": {
                "queue_ms": response.latency.queue_ms,
                "inference_ms": response.latency.inference_ms,
                "total_ms": response.latency.total_ms,
                "time_to_first_token_ms": response.latency.time_to_first_token_ms,
            },
        }
