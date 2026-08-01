#!/usr/bin/env python3
"""Verify sanitized production recovery metadata without exposing runtime values."""

from __future__ import annotations

import argparse
import importlib.util
import json
import os
import re
import stat
import sys
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any


class VerificationError(Exception):
    pass


MANIFEST_NAMES = frozenset(
    {
        "INFERA_RELEASE_ID",
        "INFERA_GATEWAY_IMAGE",
        "INFERA_WORKER_IMAGE",
        "INFERA_WORKER_PROTOCOL_VERSION",
        "INFERA_RECOVERY_API_PROTOCOL_VERSION",
        "INFERA_AUDIT_LEDGER_WRITER_PROTOCOL",
    }
)
POLICY_NAME = "INFERA_RECOVERY_WORKER_MAX_COST_HOUR"
SAFE_MANIFEST_VALUE = re.compile(r"^[A-Za-z0-9._:/@+-]+$")
SAFE_DECIMAL = re.compile(r"^(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$")
MAX_RECOVERY_WORKER_COST_HOUR = Decimal("1.00")
DEFAULT_POLICY_PATH = Path("deploy/production/infera-production-recovery-policy")

LABEL_PROJECT = "com.docker.compose.project"
LABEL_SERVICE = "com.docker.compose.service"
LABEL_ONEOFF = "com.docker.compose.oneoff"
LABEL_CONFIG_FILES = "com.docker.compose.project.config_files"
LABEL_WORKING_DIR = "com.docker.compose.project.working_dir"
LABEL_CONFIG_HASH = "com.docker.compose.config-hash"
LABEL_CONTAINER_NUMBER = "com.docker.compose.container-number"


def load_assignments(path: Path, allowed_names: frozenset[str]) -> dict[str, str]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as error:
        raise VerificationError("non-secret recovery source is unavailable") from error

    values: dict[str, str] = {}
    for line in lines:
        if not line or line.startswith("#"):
            continue
        name, separator, value = line.partition("=")
        if (
            not separator
            or name not in allowed_names
            or name in values
            or not value
        ):
            raise VerificationError("non-secret recovery source has invalid syntax")
        values[name] = value
    if set(values) != set(allowed_names):
        raise VerificationError("non-secret recovery source is incomplete")
    return values


def load_nonsecret_contract(manifest_path: Path, policy_path: Path) -> dict[str, str]:
    try:
        manifest_info = os.lstat(manifest_path)
    except OSError as error:
        raise VerificationError("release manifest is unavailable") from error
    if (
        not stat.S_ISREG(manifest_info.st_mode)
        or manifest_info.st_nlink != 1
        or stat.S_IMODE(manifest_info.st_mode) not in {0o400, 0o600}
        or manifest_info.st_uid != os.geteuid()
    ):
        raise VerificationError("release manifest metadata is unsafe")
    manifest = load_assignments(manifest_path, MANIFEST_NAMES)
    if any(not SAFE_MANIFEST_VALUE.fullmatch(value) for value in manifest.values()):
        raise VerificationError("release manifest has an unsafe value")

    try:
        policy_info = os.lstat(policy_path)
    except OSError as error:
        raise VerificationError("recovery policy is unavailable") from error
    if (
        not stat.S_ISREG(policy_info.st_mode)
        or policy_info.st_nlink != 1
        or stat.S_IMODE(policy_info.st_mode) & 0o022
        or policy_info.st_uid != os.geteuid()
    ):
        raise VerificationError("recovery policy metadata is unsafe")
    policy = load_assignments(policy_path, frozenset({POLICY_NAME}))
    raw_cost = policy[POLICY_NAME]
    if not SAFE_DECIMAL.fullmatch(raw_cost):
        raise VerificationError("recovery policy has an invalid cost ceiling")
    try:
        cost = Decimal(raw_cost)
    except InvalidOperation as error:
        raise VerificationError("recovery policy has an invalid cost ceiling") from error
    if not cost.is_finite() or cost <= 0 or cost > MAX_RECOVERY_WORKER_COST_HOUR:
        raise VerificationError("recovery policy cost ceiling is outside the approved range")

    return {
        "INFERA_RECOVERY_API_PROTOCOL_VERSION": manifest[
            "INFERA_RECOVERY_API_PROTOCOL_VERSION"
        ],
        POLICY_NAME: raw_cost,
    }


def normalized_absolute_path(value: str) -> str | None:
    if not value or not os.path.isabs(value) or value != os.path.normpath(value):
        return None
    return value


def label_checks(
    labels: dict[str, str],
    *,
    project: str,
    service: str,
    config_files: str,
    working_dir: str,
) -> dict[str, bool]:
    expected_config_files = normalized_absolute_path(config_files)
    expected_working_dir = normalized_absolute_path(working_dir)
    actual_config_files = labels.get(LABEL_CONFIG_FILES, "")
    actual_working_dir = labels.get(LABEL_WORKING_DIR, "")
    return {
        "label_project": labels.get(LABEL_PROJECT) == project,
        "label_service": labels.get(LABEL_SERVICE) == service,
        "label_oneoff": labels.get(LABEL_ONEOFF) == "False",
        "label_config_files_serialization": (
            expected_config_files is not None
            and normalized_absolute_path(actual_config_files) == expected_config_files
        ),
        "label_working_dir_path": (
            expected_working_dir is not None
            and normalized_absolute_path(actual_working_dir) == expected_working_dir
        ),
        "label_config_hash": bool(labels.get(LABEL_CONFIG_HASH)),
        "label_container_number": bool(
            re.fullmatch(r"[1-9][0-9]*", labels.get(LABEL_CONTAINER_NUMBER, ""))
        ),
    }


def normalized_mounts(mounts: list[dict[str, Any]]) -> tuple[str, ...]:
    if any(not isinstance(mount, dict) for mount in mounts):
        raise VerificationError("runtime mount metadata is invalid")
    return tuple(
        sorted(
            json.dumps(mount, sort_keys=True, separators=(",", ":"))
            for mount in mounts
        )
    )


def runtime_key(container: dict[str, Any]) -> tuple[str, str]:
    labels = container.get("labels")
    if not isinstance(labels, dict):
        raise VerificationError("runtime labels are invalid")
    service = labels.get(LABEL_SERVICE, "")
    number = labels.get(LABEL_CONTAINER_NUMBER, "")
    if not service or not number:
        raise VerificationError("runtime identity is incomplete")
    return service, number


def validated_mount_expectations(
    expected_mounts: list[dict[str, Any]], expected_services: set[str]
) -> list[tuple[str, str | None, str, str, str]]:
    allowed_names = {
        "service",
        "container_number",
        "Type",
        "Source",
        "Destination",
    }
    validated: list[tuple[str, str | None, str, str, str]] = []
    scopes: dict[tuple[str, str, str, str], set[str | None]] = {}
    for mount in expected_mounts:
        if not isinstance(mount, dict) or set(mount) - allowed_names:
            raise VerificationError("checked-in mount expectation is malformed")
        service = mount.get("service")
        container_number = mount.get("container_number")
        mount_type = mount.get("Type")
        source = mount.get("Source")
        destination = mount.get("Destination")
        if (
            not isinstance(service, str)
            or service not in expected_services
            or (
                container_number is not None
                and (
                    not isinstance(container_number, str)
                    or not re.fullmatch(r"[1-9][0-9]*", container_number)
                )
            )
            or mount_type != "bind"
            or not isinstance(source, str)
            or normalized_absolute_path(source) is None
            or not isinstance(destination, str)
            or normalized_absolute_path(destination) is None
        ):
            raise VerificationError("checked-in mount expectation is malformed")
        scope_key = (service, mount_type, source, destination)
        scopes.setdefault(scope_key, set()).add(container_number)
        validated.append(
            (service, container_number, mount_type, source, destination)
        )
    if len(set(validated)) != len(validated) or any(
        None in numbers and len(numbers) > 1 for numbers in scopes.values()
    ):
        raise VerificationError("checked-in mount expectation is ambiguous")
    return validated


def checked_in_mounts_present(
    containers: list[dict[str, Any]],
    expected_mounts: list[tuple[str, str | None, str, str, str]],
) -> bool:
    indexed = {runtime_key(container): container for container in containers}
    for service, number, mount_type, source, destination in expected_mounts:
        targets = [
            container
            for (actual_service, actual_number), container in indexed.items()
            if actual_service == service
            and (number is None or actual_number == number)
        ]
        if not targets:
            return False
        for container in targets:
            mounts = container.get("mounts", [])
            if not isinstance(mounts, list):
                return False
            actual = {
                (
                    str(mount.get("Type", "")),
                    str(mount.get("Source", "")),
                    str(mount.get("Destination", "")),
                )
                for mount in mounts
                if isinstance(mount, dict)
            }
            if (mount_type, source, destination) not in actual:
                return False
    return True


def load_validated_production_values(path: Path) -> dict[str, str]:
    module_path = Path(__file__).with_name("production-env-source.py")
    spec = importlib.util.spec_from_file_location(
        "production_env_source", module_path
    )
    if spec is None or spec.loader is None:
        raise VerificationError("production environment validator is unavailable")
    module = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(module)
        values = module.open_validated_source_path(str(path), os.geteuid())
    except Exception as error:
        raise VerificationError(
            "production environment validation failed"
        ) from error
    if not isinstance(values, dict):
        raise VerificationError("production environment validation failed")
    return values


def compare_runtime_metadata(
    before: list[dict[str, Any]],
    after: list[dict[str, Any]],
    expectations: dict[str, Any],
) -> dict[str, bool]:
    project = expectations.get("project")
    config_files = expectations.get("config_files")
    working_dir = expectations.get("working_dir")
    services = expectations.get("services")
    expected_mounts = expectations.get("checked_in_mounts", [])
    if (
        not isinstance(project, str)
        or not isinstance(config_files, str)
        or not isinstance(working_dir, str)
        or not isinstance(services, dict)
        or not isinstance(expected_mounts, list)
    ):
        raise VerificationError("runtime expectations are invalid")

    def indexed(
        containers: list[dict[str, Any]],
    ) -> dict[tuple[str, str], dict[str, Any]]:
        result: dict[tuple[str, str], dict[str, Any]] = {}
        for container in containers:
            key = runtime_key(container)
            if key in result:
                raise VerificationError("runtime identity is ambiguous")
            result[key] = container
        return result

    before_index = indexed(before)
    after_index = indexed(after)
    expected_counts: dict[str, int] = {}
    for service, count in services.items():
        if (
            not isinstance(service, str)
            or not service
            or isinstance(count, bool)
            or not isinstance(count, int)
            or count <= 0
        ):
            raise VerificationError("runtime service cardinality is invalid")
        expected_counts[service] = count
    validated_expected_mounts = validated_mount_expectations(
        expected_mounts, set(expected_counts)
    )

    def cardinality(index: dict[tuple[str, str], dict[str, Any]]) -> bool:
        actual: dict[str, int] = {}
        for service, _ in index:
            actual[service] = actual.get(service, 0) + 1
        return actual == expected_counts

    results = {
        "cardinality_before": cardinality(before_index),
        "cardinality_after": cardinality(after_index),
        "identity_set_unchanged": set(before_index) == set(after_index),
        "container_identity_unchanged": True,
        "image_identity_unchanged": True,
        "start_time_unchanged": True,
        "restart_count_unchanged": True,
        "mount_sets_unchanged": True,
        "checked_in_mounts_before": checked_in_mounts_present(
            before, validated_expected_mounts
        ),
        "checked_in_mounts_after": checked_in_mounts_present(
            after, validated_expected_mounts
        ),
    }
    for check_name in (
        "label_project",
        "label_service",
        "label_oneoff",
        "label_config_files_serialization",
        "label_working_dir_path",
        "label_config_hash",
        "label_container_number",
    ):
        results[f"{check_name}_before"] = True
        results[f"{check_name}_after"] = True

    for phase, containers in (("before", before), ("after", after)):
        for container in containers:
            labels = container.get("labels")
            if not isinstance(labels, dict):
                raise VerificationError("runtime labels are invalid")
            checks = label_checks(
                labels,
                project=project,
                service=str(labels.get(LABEL_SERVICE, "")),
                config_files=config_files,
                working_dir=working_dir,
            )
            checks["label_service"] = (
                str(labels.get(LABEL_SERVICE, "")) in expected_counts
            )
            for name, passed in checks.items():
                results[f"{name}_{phase}"] &= passed

    for key in set(before_index) & set(after_index):
        old = before_index[key]
        new = after_index[key]
        results["container_identity_unchanged"] &= old.get("id") == new.get("id")
        results["image_identity_unchanged"] &= (
            old.get("image_id") == new.get("image_id")
            and old.get("config_image") == new.get("config_image")
        )
        results["start_time_unchanged"] &= (
            old.get("started_at") == new.get("started_at")
        )
        results["restart_count_unchanged"] &= (
            old.get("restart_count") == new.get("restart_count")
        )
        old_mounts = old.get("mounts")
        new_mounts = new.get("mounts")
        if not isinstance(old_mounts, list) or not isinstance(new_mounts, list):
            raise VerificationError("runtime mount metadata is invalid")
        results["mount_sets_unchanged"] &= (
            normalized_mounts(old_mounts) == normalized_mounts(new_mounts)
        )
    if set(before_index) != set(after_index):
        for name in (
            "container_identity_unchanged",
            "image_identity_unchanged",
            "start_time_unchanged",
            "restart_count_unchanged",
            "mount_sets_unchanged",
        ):
            results[name] = False
    return results


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise VerificationError("runtime metadata is unavailable") from error


def production_contract_matches(
    authoritative_values: dict[str, str], production_values: dict[str, str]
) -> bool:
    def value_matches(name: str, value: str) -> bool:
        actual = production_values.get(name)
        if name != POLICY_NAME:
            return actual == value
        if actual is None or not SAFE_DECIMAL.fullmatch(actual):
            return False
        return Decimal(actual) == Decimal(value)

    return all(
        value_matches(name, value)
        for name, value in authoritative_values.items()
    )


def command_verify_environment(args: argparse.Namespace) -> int:
    authoritative_values = load_nonsecret_contract(args.manifest, args.policy)
    production_values = load_validated_production_values(args.production_env)
    matches = production_contract_matches(authoritative_values, production_values)
    print("nonsecret_contract_complete=true")
    print(f"nonsecret_contract_matches={str(matches).lower()}")
    return 0 if matches else 1


def command_verify(args: argparse.Namespace) -> int:
    authoritative_values = load_nonsecret_contract(args.manifest, args.policy)
    production_values = load_validated_production_values(args.production_env)
    before = load_json(args.before)
    after = load_json(args.after)
    expectations = load_json(args.expectations)
    if not isinstance(before, list) or not isinstance(after, list) or not isinstance(
        expectations, dict
    ):
        raise VerificationError("runtime metadata has the wrong shape")
    results = compare_runtime_metadata(before, after, expectations)
    results["nonsecret_contract_complete"] = True

    results["nonsecret_contract_matches"] = production_contract_matches(
        authoritative_values, production_values
    )
    for name in sorted(results):
        print(f"{name}={str(results[name]).lower()}")
    return 0 if all(results.values()) else 1


def parser() -> argparse.ArgumentParser:
    command_parser = argparse.ArgumentParser(description=__doc__)
    subparsers = command_parser.add_subparsers(dest="command", required=True)
    verify = subparsers.add_parser("verify")
    verify.add_argument("--before", type=Path, required=True)
    verify.add_argument("--after", type=Path, required=True)
    verify.add_argument("--expectations", type=Path, required=True)
    verify.add_argument("--manifest", type=Path, required=True)
    verify.add_argument("--production-env", type=Path, required=True)
    verify.add_argument(
        "--policy",
        type=Path,
        default=DEFAULT_POLICY_PATH,
    )
    verify.set_defaults(function=command_verify)
    verify_environment = subparsers.add_parser("verify-environment")
    verify_environment.add_argument("--manifest", type=Path, required=True)
    verify_environment.add_argument("--production-env", type=Path, required=True)
    verify_environment.add_argument(
        "--policy",
        type=Path,
        default=DEFAULT_POLICY_PATH,
    )
    verify_environment.set_defaults(function=command_verify_environment)
    return command_parser


def main() -> int:
    args = parser().parse_args()
    try:
        return args.function(args)
    except VerificationError:
        print("ERROR: production recovery verification failed", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
