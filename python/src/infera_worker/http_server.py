"""HTTP server for Infera Worker - simpler than gRPC for vertical slice."""

import asyncio
import json
import os
from datetime import datetime
from typing import Any
import structlog
import httpx

from aiohttp import web

from .config import WorkerConfig, ModelConfig
from .worker import Worker
from .types import (
    InferenceRequest,
    InferenceParameters,
    Message,
    Role,
    Priority,
)

logger = structlog.get_logger()


def build_gateway_url(router_address: str, path: str) -> str:
    """Build a gateway URL with proper protocol.
    
    Uses HTTPS for ngrok and other public URLs, HTTP for localhost.
    """
    if not router_address:
        return ""
    
    # If protocol is already included, use it
    if router_address.startswith("https://"):
        return f"{router_address}{path}"
    elif router_address.startswith("http://"):
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
        self._registration_task: asyncio.Task | None = None
        self._heartbeat_task: asyncio.Task | None = None
        self._setup_routes()

    def _setup_routes(self) -> None:
        """Set up HTTP routes."""
        self.app.router.add_post("/infer", self.handle_infer)
        self.app.router.add_post("/infer/stream", self.handle_infer_stream)
        self.app.router.add_get("/health", self.handle_health)
        self.app.router.add_get("/models", self.handle_list_models)
        self.app.router.add_post("/models/load", self.handle_load_model)
        self.app.router.add_post("/models/unload", self.handle_unload_model)
        self.app.router.add_get("/stats", self.handle_stats)

    async def start(self) -> None:
        """Start the HTTP server."""
        self.runner = web.AppRunner(self.app)
        await self.runner.setup()
        
        site = web.TCPSite(self.runner, "0.0.0.0", self.config.http_port)
        await site.start()
        
        logger.info("HTTP server started", port=self.config.http_port)
        
        # Start gateway registration
        if self.config.router_address:
            self._registration_task = asyncio.create_task(self._register_with_gateway())
            self._heartbeat_task = asyncio.create_task(self._heartbeat_loop())

    async def stop(self) -> None:
        """Stop the HTTP server."""
        # Cancel background tasks
        if self._registration_task:
            self._registration_task.cancel()
        if self._heartbeat_task:
            self._heartbeat_task.cancel()
        
        # Deregister from gateway
        await self._deregister_from_gateway()
        
        if self.runner:
            await self.runner.cleanup()
            logger.info("HTTP server stopped")

    async def _register_with_gateway(self) -> None:
        """Register this worker with the gateway."""
        gateway_url = build_gateway_url(self.config.router_address, "/api/workers/register")
        
        # Get public IP/hostname for the gateway to reach us
        worker_address = self._get_worker_address()
        
        registration_data = {
            "worker_id": self.worker.worker_id,
            "address": worker_address,
            "status": "healthy",
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
                async with httpx.AsyncClient() as client:
                    response = await client.post(
                        gateway_url,
                        json=registration_data,
                        headers=self.config.gateway_headers(),
                        timeout=10.0,
                    )
                    
                    if response.status_code == 200:
                        logger.info(
                            "Registered with gateway",
                            gateway=self.config.router_address,
                            worker_id=self.worker.worker_id,
                        )
                        return
                    else:
                        logger.warning(
                            "Gateway registration failed",
                            status=response.status_code,
                            response=response.text,
                        )
                        
            except Exception as e:
                logger.warning(
                    "Gateway registration attempt failed",
                    attempt=attempt + 1,
                    error=str(e),
                )
            
            await asyncio.sleep(retry_delay)
        
        logger.error("Failed to register with gateway after max retries")

    async def _deregister_from_gateway(self) -> None:
        """Deregister this worker from the gateway."""
        if not self.config.router_address:
            return
        
        gateway_url = build_gateway_url(
            self.config.router_address, 
            f"/api/workers/{self.worker.worker_id}"
        )
        
        try:
            async with httpx.AsyncClient() as client:
                await client.delete(gateway_url, headers=self.config.gateway_headers(), timeout=5.0)
                logger.info("Deregistered from gateway")
        except Exception as e:
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
                
                async with httpx.AsyncClient() as client:
                    response = await client.post(
                        gateway_url,
                        json=heartbeat_data,
                        headers=self.config.gateway_headers(),
                        timeout=5.0,
                    )
                    if response.status_code == 200:
                        logger.debug("Heartbeat sent successfully")
                    
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.debug("Heartbeat failed", error=str(e))

    def _get_worker_address(self) -> str:
        """Get the address where the gateway can reach this worker."""
        # Check for RunPod pod ID to construct proxy URL
        runpod_pod_id = os.environ.get("RUNPOD_POD_ID")
        if runpod_pod_id:
            return f"{runpod_pod_id}-{self.config.http_port}.proxy.runpod.net"
        
        # Check for RunPod public IP
        runpod_public_ip = os.environ.get("RUNPOD_PUBLIC_IP")
        if runpod_public_ip:
            return runpod_public_ip
        
        # Check for explicit address
        explicit_address = os.environ.get("INFERA_WORKER_ADDRESS")
        if explicit_address:
            return explicit_address
        
        # Default to localhost for local development
        return f"localhost:{self.config.http_port}"

    async def handle_infer(self, request: web.Request) -> web.Response:
        """Handle non-streaming inference request."""
        try:
            data = await request.json()
            inference_request = self._parse_request(data)
            
            response = await self.worker.infer(inference_request)
            
            return web.json_response(self._format_response(response))
            
        except ValueError as e:
            return web.json_response(
                {"error": {"type": "invalid_request", "message": str(e)}},
                status=400
            )
        except RuntimeError as e:
            return web.json_response(
                {"error": {"type": "worker_error", "message": str(e)}},
                status=503
            )
        except Exception as e:
            logger.error("Inference error", error=str(e))
            return web.json_response(
                {"error": {"type": "internal_error", "message": str(e)}},
                status=500
            )

    async def handle_infer_stream(self, request: web.Request) -> web.StreamResponse:
        """Handle streaming inference request."""
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
            data = await request.json()
            inference_request = self._parse_request(data)
            inference_request.stream = True

            async for chunk in self.worker.infer_stream(inference_request):
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

                await response.write(json.dumps(chunk_data).encode() + b"\n")

        except Exception as e:
            logger.error("Streaming error", error=str(e))
            error_data = {"error": {"message": str(e)}}
            await response.write(json.dumps(error_data).encode() + b"\n")

        await response.write_eof()
        return response

    async def handle_health(self, request: web.Request) -> web.Response:
        """Handle health check."""
        state = self.worker.get_state()
        stats = self.worker.get_stats()
        
        return web.json_response({
            "status": "healthy" if state.value == "ready" else state.value,
            "worker_id": self.worker.worker_id,
            "state": state.value,
            "models_loaded": len(self.worker.get_loaded_models()),
            "memory_used_bytes": stats.memory_used_bytes,
            "memory_total_bytes": stats.memory_total_bytes,
        })

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
        messages = [
            Message(
                role=Role(msg.get("role", "user")),
                content=msg.get("content", ""),
                name=msg.get("name"),
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

        return InferenceRequest(
            request_id=data.get("request_id", ""),
            model_id=model_id,
            messages=messages,
            parameters=parameters,
            stream=data.get("stream", False),
            priority=Priority(data.get("priority", 2)),
            metadata=data.get("metadata", {}),
        )

    def _format_response(self, response) -> dict[str, Any]:
        """Format InferenceResponse as dict."""
        return {
            "request_id": response.request_id,
            "model_id": response.model_id,
            "choices": [
                {
                    "index": choice.index,
                    "message": {
                        "role": choice.message.role.value,
                        "content": choice.message.content,
                    },
                    "finish_reason": choice.finish_reason.value,
                }
                for choice in response.choices
            ],
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
