"""CLI entry point for Infera Worker."""

import asyncio
import signal
import sys
import structlog

from .config import WorkerConfig
from .worker import Worker
from .http_server import HTTPServer


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
        
        # Wait for shutdown
        await shutdown_event.wait()
        
    except Exception as e:
        logger.error("Worker error", error=str(e))
        raise
        
    finally:
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
