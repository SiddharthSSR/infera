"""CLI entry point for Infera Worker."""

import asyncio
import os
import signal
import sys
import structlog

from .config import WorkerConfig
from .worker import Worker
from .http_server import HTTPServer, build_gateway_url

# Optional httpx for registration
try:
    import httpx
    HTTPX_AVAILABLE = True
except ImportError:
    HTTPX_AVAILABLE = False


def setup_logging(config: WorkerConfig) -> None:
    """Configure structured logging."""
    processors = [
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.add_log_level,
    ]
    if config.log_format == "json":
        processors.append(structlog.processors.JSONRenderer())
    else:
        processors.append(structlog.dev.ConsoleRenderer())
    
    structlog.configure(processors=processors)


def get_worker_public_address(config: WorkerConfig) -> str:
    """Get the worker's public address for registration.
    
    In RunPod, the public URL is: https://{POD_ID}-{PORT}.proxy.runpod.net
    """
    # First check if explicitly set
    if config.worker_address:
        return config.worker_address
    
    # Check for RunPod environment
    pod_id = os.getenv("RUNPOD_POD_ID")
    if pod_id:
        # RunPod proxy URL format
        return f"{pod_id}-{config.http_port}.proxy.runpod.net"
    
    # Fallback to localhost
    return f"localhost:{config.http_port}"


async def register_with_gateway(worker: Worker, config: WorkerConfig) -> bool:
    """Register this worker with the gateway."""
    if not HTTPX_AVAILABLE:
        return False
    
    if not config.router_address:
        return False
        
    logger = structlog.get_logger()
    
    # Get the worker's public address
    worker_address = get_worker_public_address(config)
    
    try:
        async with httpx.AsyncClient() as client:
            stats = worker.get_stats()
            models = worker.get_loaded_models()
            
            registration_data = {
                "worker_id": worker.worker_id,
                "address": worker_address,
                "status": "healthy",
                "loaded_models": [
                    {
                        "model_id": m.model_id,
                        "version": m.version,
                        "memory_bytes": m.memory_bytes,
                        "max_batch_size": m.max_batch_size,
                        "max_sequence_length": m.max_sequence_length,
                    }
                    for m in models
                ],
                "stats": {
                    "queue_depth": stats.queue_depth,
                    "active_requests": stats.active_requests,
                    "gpu_utilization": stats.gpu_utilization,
                    "memory_used_bytes": stats.memory_used_bytes,
                    "memory_total_bytes": stats.memory_total_bytes,
                    "requests_per_second": stats.requests_per_second,
                    "avg_latency_ms": stats.avg_latency_ms,
                    "error_rate": stats.error_rate,
                },
            }
            
            gateway_url = build_gateway_url(config.router_address, "/api/workers/register")
            
            logger.info("Registering with gateway", gateway_url=gateway_url, worker_address=worker_address)
            
            response = await client.post(
                gateway_url,
                json=registration_data,
                headers=config.gateway_headers(),
                timeout=10.0,
            )
            
            if response.status_code == 200:
                logger.info("Registered with gateway", gateway=config.router_address)
                return True
            elif response.status_code in (401, 403):
                logger.error(
                    "Gateway registration rejected by auth",
                    status=response.status_code,
                    body=response.text,
                    gateway=config.router_address,
                    gateway_url=gateway_url,
                )
                raise RuntimeError("Gateway auth rejected worker registration")
            else:
                logger.warning("Failed to register", status=response.status_code, body=response.text)
                return False
                
    except RuntimeError:
        raise
    except Exception as e:
        logger.warning("Could not register with gateway", error=str(e), gateway=config.router_address)
        return False


async def heartbeat_loop(worker: Worker, config: WorkerConfig, interval: float = 5.0) -> None:
    """Send periodic heartbeats to the gateway."""
    if not HTTPX_AVAILABLE or not config.router_address:
        return
    
    heartbeat_url = build_gateway_url(config.router_address, "/api/workers/heartbeat")
        
    logger = structlog.get_logger()
        
    while True:
        await asyncio.sleep(interval)
        
        try:
            async with httpx.AsyncClient() as client:
                stats = worker.get_stats()
                models = worker.get_loaded_models()
                
                heartbeat_data = {
                    "worker_id": worker.worker_id,
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
                        for m in models
                    ],
                }
                
                response = await client.post(
                    heartbeat_url,
                    json=heartbeat_data,
                    headers=config.gateway_headers(),
                    timeout=5.0,
                )
                if response.is_error:
                    logger.error(
                        "Heartbeat request rejected",
                        status=response.status_code,
                        body=response.text,
                        gateway_url=heartbeat_url,
                    )
                
        except Exception as e:
            logger.debug("Heartbeat failed", error=str(e))


async def run_worker(config: WorkerConfig) -> None:
    """Run the worker."""
    logger = structlog.get_logger()
    worker = Worker(config)
    http_server = HTTPServer(worker, config)
    shutdown_event = asyncio.Event()

    def signal_handler():
        logger.info("Received shutdown signal")
        shutdown_event.set()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, signal_handler)

    heartbeat_task = None
    
    try:
        # Start worker
        await worker.start()
        
        # Start HTTP server
        await http_server.start()
        
        logger.info(
            "Worker running",
            worker_id=worker.worker_id,
            http_port=config.http_port,
            engine=config.engine,
        )
        
        # Try to register with gateway
        await register_with_gateway(worker, config)
        
        # Start heartbeat task
        heartbeat_task = asyncio.create_task(heartbeat_loop(worker, config))
        
        # Wait for shutdown
        await shutdown_event.wait()
        
    except Exception as e:
        logger.error("Worker error", error=str(e))
        raise
        
    finally:
        # Cancel heartbeat
        if heartbeat_task:
            heartbeat_task.cancel()
            try:
                await heartbeat_task
            except asyncio.CancelledError:
                pass
        
        logger.info("Shutting down...")
        await http_server.stop()
        await worker.stop()
        logger.info("Shutdown complete")


def main() -> None:
    """Main entry point."""
    config = WorkerConfig()
    setup_logging(config)
    
    logger = structlog.get_logger()
    logger.info(
        "Starting Infera Worker",
        engine=config.engine,
        http_port=config.http_port,
    )

    try:
        asyncio.run(run_worker(config))
    except KeyboardInterrupt:
        pass
    except Exception as e:
        logger.error("Fatal error", error=str(e))
        sys.exit(1)


if __name__ == "__main__":
    main()
