#!/usr/bin/env python3
"""Validate and consume the single production runtime environment source."""

from __future__ import annotations

import argparse
import os
import shlex
import shutil
import stat
import sys
from pathlib import Path

REQUIRED_NAMES = (
    "INFERA_ADMIN_KEY",
    "INFERA_ALLOWED_ORIGINS",
    "INFERA_GATEWAY_ADDRESS",
    "INFERA_WORKER_SHARED_TOKEN",
    "INFERA_RELEASE_ID",
    "INFERA_WORKER_PROTOCOL_VERSION",
    "INFERA_RECOVERY_API_PROTOCOL_VERSION",
    "INFERA_GATEWAY_IMAGE",
    "INFERA_FRONTEND_IMAGE",
    "INFERA_CONTROL_STATE_DSN",
    "INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY",
    "INFERA_WORKER_IMAGE",
    "INFERA_GATEWAY_REPLICAS",
    "INFERA_AUDIT_LEDGER_BACKEND",
    "GRAFANA_ADMIN_USER",
    "GRAFANA_ADMIN_PASSWORD",
    "ALERT_EMAIL_TO",
    "ALERT_SMTP_FROM",
    "ALERT_SMTP_SMARTHOST",
    "ALERT_SMTP_USERNAME",
    "ALERT_SMTP_PASSWORD",
    "RUNPOD_API_KEY",
    "INFERA_RECOVERY_WORKER_MAX_COST_HOUR",
)
LEGACY_SOURCE_NAMES = ("ENV_FILE", "INFERA_ENV_FILE", "INFERA_RUNTIME_ENV_FILE")
COMPOSE_OVERRIDE_NAMES = frozenset(
    {
        "INFERA_RELEASE_ID",
        "INFERA_WORKER_PROTOCOL_VERSION",
        "INFERA_RECOVERY_API_PROTOCOL_VERSION",
        "INFERA_GATEWAY_IMAGE",
        "INFERA_FRONTEND_IMAGE",
        "INFERA_WORKER_IMAGE",
        "INFERA_WORKER_IMAGE_VLLM",
        "INFERA_WORKER_IMAGE_SGLANG",
        "INFERA_WORKER_IMAGE_TENSORRT_LLM",
        "INFERA_WORKER_IMAGE_MOCK",
    }
)
AMBIENT_PREFIXES = ("INFERA_", "GRAFANA_", "ALERT_", "RUNPOD_", "VASTAI_", "HF_")


class ContractError(Exception):
    pass


def parse_assignment(raw_line: str, line_number: int) -> tuple[str, str] | None:
    line = raw_line.strip()
    if not line or line.startswith("#"):
        return None
    if line.startswith("export "):
        line = line[len("export ") :].strip()
    if "=" not in line:
        raise ContractError(f"invalid assignment on line {line_number}")
    name, raw_value = line.split("=", 1)
    name = name.strip()
    if not name or not (name[0].isalpha() or name[0] == "_"):
        raise ContractError(f"invalid variable name on line {line_number}")
    if any(not (character.isalnum() or character == "_") for character in name):
        raise ContractError(f"invalid variable name on line {line_number}")
    raw_value = raw_value.strip()
    try:
        parts = shlex.split(raw_value, comments=True, posix=True)
    except ValueError as error:
        raise ContractError(f"invalid value syntax for {name}") from error
    if len(parts) > 1:
        raise ContractError(f"ambiguous value syntax for {name}")
    return name, parts[0] if parts else ""


def open_validated_source(expected_uid: int) -> dict[str, str]:
    configured = os.environ.get("INFERA_PRODUCTION_ENV_FILE", "")
    if not configured:
        raise ContractError("INFERA_PRODUCTION_ENV_FILE is required")
    if any(os.environ.get(name, "") for name in LEGACY_SOURCE_NAMES):
        raise ContractError("ambiguous production environment source selectors")
    path = Path(configured)
    if not path.is_absolute():
        raise ContractError("INFERA_PRODUCTION_ENV_FILE must be absolute")

    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0)
    if not hasattr(os, "O_NOFOLLOW"):
        raise ContractError("O_NOFOLLOW is required")
    flags |= os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise ContractError("production environment source is unavailable or unsafe") from error

    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode):
            raise ContractError("production environment source must be a regular file")
        if info.st_uid != expected_uid:
            raise ContractError("production environment source has the wrong owner")
        if stat.S_IMODE(info.st_mode) not in {0o400, 0o600}:
            raise ContractError("production environment source must have mode 0400 or 0600")
        if info.st_nlink != 1:
            raise ContractError("production environment source must have exactly one link")
        with os.fdopen(os.dup(descriptor), encoding="utf-8", errors="strict") as source:
            lines = source.read().splitlines()
    except UnicodeError as error:
        raise ContractError("production environment source is not valid UTF-8") from error
    finally:
        os.close(descriptor)

    values: dict[str, str] = {}
    for line_number, raw_line in enumerate(lines, 1):
        assignment = parse_assignment(raw_line, line_number)
        if assignment is None:
            continue
        name, value = assignment
        if name in values:
            raise ContractError(f"duplicate production variable name: {name}")
        values[name] = value

    missing = [name for name in REQUIRED_NAMES if not values.get(name)]
    backend = values.get("INFERA_AUDIT_LEDGER_BACKEND", "").lower()
    if backend in {"postgres", "postgresql"} and not values.get("INFERA_AUDIT_LEDGER_DSN"):
        missing.append("INFERA_AUDIT_LEDGER_DSN")
    if missing:
        raise ContractError("missing required production variable names: " + ", ".join(missing))
    return values


def validate(expected_uid: int) -> dict[str, str]:
    return open_validated_source(expected_uid)


def command_validate(args: argparse.Namespace) -> int:
    validate(args.expected_uid)
    print(
        f"Production environment source validation passed "
        f"({len(REQUIRED_NAMES)} required names present; values hidden)."
    )
    return 0


def command_value(args: argparse.Namespace) -> int:
    values = validate(args.expected_uid)
    value = values.get(args.name, "")
    if not value:
        raise ContractError(f"production variable is missing: {args.name}")
    sys.stdout.write(value)
    return 0


def command_compose(args: argparse.Namespace) -> int:
    validate(args.expected_uid)
    compose_args = list(args.compose_args)
    if compose_args[:1] == ["--"]:
        compose_args.pop(0)
    if not compose_args:
        raise ContractError("Docker Compose arguments are required")
    override_names = args.override or []
    invalid = sorted(set(override_names) - COMPOSE_OVERRIDE_NAMES)
    if invalid:
        raise ContractError("unsupported Compose override names: " + ", ".join(invalid))
    missing_overrides = [name for name in override_names if not os.environ.get(name)]
    if missing_overrides:
        raise ContractError("missing Compose override names: " + ", ".join(missing_overrides))

    child_environment = {
        name: value
        for name, value in os.environ.items()
        if not name.startswith(AMBIENT_PREFIXES)
        and name not in LEGACY_SOURCE_NAMES
        and name != "INFERA_PRODUCTION_ENV_FILE"
        and name != "BASH_ENV"
        and not name.startswith("BASH_FUNC_")
    }
    for name in override_names:
        child_environment[name] = os.environ[name]

    source = os.environ["INFERA_PRODUCTION_ENV_FILE"]
    command = ["docker", "compose", "--env-file", source, *compose_args]
    executable = shutil.which(command[0], path=child_environment.get("PATH"))
    if executable is None:
        raise ContractError("Docker executable is unavailable")
    os.execve(executable, command, child_environment)
    return 127


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(add_help=False)
    root.add_argument("--expected-uid", type=int, default=0, help=argparse.SUPPRESS)
    command_parser = argparse.ArgumentParser(description=__doc__)
    subparsers = command_parser.add_subparsers(dest="command", required=True)

    validate_parser = subparsers.add_parser("validate", parents=[root])
    validate_parser.set_defaults(function=command_validate)

    value_parser = subparsers.add_parser("value", parents=[root])
    value_parser.add_argument("name")
    value_parser.set_defaults(function=command_value)

    compose_parser = subparsers.add_parser("compose", parents=[root])
    compose_parser.add_argument("--override", action="append")
    compose_parser.add_argument("compose_args", nargs=argparse.REMAINDER)
    compose_parser.set_defaults(function=command_compose)
    return command_parser


def main() -> int:
    args = parser().parse_args()
    try:
        return args.function(args)
    except ContractError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 2
    except OSError:
        print("ERROR: production environment source operation failed", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
