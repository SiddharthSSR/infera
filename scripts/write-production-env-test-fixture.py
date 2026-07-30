#!/usr/bin/env python3
"""Write a complete non-production dotenv fixture for shell tests."""

from __future__ import annotations

import argparse
import importlib.util
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path)
    parser.add_argument("--gateway-replicas", default="1")
    args = parser.parse_args()

    source_path = Path(__file__).with_name("production-env-source.py")
    spec = importlib.util.spec_from_file_location("production_env_source", source_path)
    if spec is None or spec.loader is None:
        raise SystemExit(1)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

    values = {name: f"test-{name.lower()}" for name in module.REQUIRED_NAMES}
    values.update(
        {
            "INFERA_ADMIN_KEY": "test-admin-key",
            "RUNPOD_API_KEY": "test-runpod-key",
            "INFERA_GATEWAY_IMAGE": "ghcr.io/example/gateway@sha256:" + "a" * 64,
            "INFERA_FRONTEND_IMAGE": "ghcr.io/example/frontend@sha256:" + "b" * 64,
            "INFERA_WORKER_IMAGE": "ghcr.io/example/worker@sha256:" + "c" * 64,
            "INFERA_GATEWAY_REPLICAS": args.gateway_replicas,
            "INFERA_AUDIT_LEDGER_BACKEND": "postgres",
            "INFERA_AUDIT_LEDGER_DSN": "postgresql://test.invalid/infera",
            "INFERA_RECOVERY_WORKER_MAX_COST_HOUR": "3.5",
        }
    )
    args.output.write_text(
        "".join(f"{name}={value}\n" for name, value in values.items()),
        encoding="utf-8",
    )
    args.output.chmod(0o600)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
