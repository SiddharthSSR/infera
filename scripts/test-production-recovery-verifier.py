#!/usr/bin/env python3
"""Focused regression tests for production-recovery-verifier.py."""

from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parent
MODULE_PATH = SCRIPT_DIR / "production-recovery-verifier.py"
ENV_SOURCE_PATH = SCRIPT_DIR / "production-env-source.py"
POLICY_PATH = (
    REPO_ROOT / "deploy/production/infera-production-recovery-policy"
)
spec = importlib.util.spec_from_file_location("production_recovery_verifier", MODULE_PATH)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(module)
env_spec = importlib.util.spec_from_file_location(
    "production_env_source", ENV_SOURCE_PATH
)
env_module = importlib.util.module_from_spec(env_spec)
assert env_spec.loader is not None
env_spec.loader.exec_module(env_module)


def labels(**overrides: str) -> dict[str, str]:
    values = {
        module.LABEL_PROJECT: "infera",
        module.LABEL_SERVICE: "gateway",
        module.LABEL_ONEOFF: "False",
        module.LABEL_CONFIG_FILES: "/opt/infera/docker-compose.prod.yml",
        module.LABEL_WORKING_DIR: "/opt/infera",
        module.LABEL_CONFIG_HASH: "config-hash",
        module.LABEL_CONTAINER_NUMBER: "1",
    }
    values.update(overrides)
    return values


def mount(source: str, destination: str) -> dict[str, object]:
    return {
        "Type": "bind",
        "Source": source,
        "Destination": destination,
        "Mode": "ro",
        "RW": False,
        "Propagation": "rprivate",
    }


def container(mounts: list[dict[str, object]], **overrides: object) -> dict[str, object]:
    value: dict[str, object] = {
        "id": "container-id",
        "image_id": "sha256:image",
        "config_image": "example.invalid/gateway@sha256:digest",
        "restart_count": 0,
        "started_at": "2026-07-30T00:00:00Z",
        "labels": labels(),
        "mounts": mounts,
    }
    value.update(overrides)
    return value


def expectations() -> dict[str, object]:
    return {
        "project": "infera",
        "config_files": "/opt/infera/docker-compose.prod.yml",
        "working_dir": "/opt/infera",
        "services": {"gateway": 1},
        "checked_in_mounts": [
            {
                "service": "gateway",
                "Type": "bind",
                "Source": "/opt/infera/deploy/caddy/Caddyfile",
                "Destination": "/etc/caddy/Caddyfile",
            }
        ],
    }


def write_production_env(
    path: Path, *, protocol: str, cost: str = "1.00"
) -> None:
    values = {name: f"test-{name.lower()}" for name in env_module.REQUIRED_NAMES}
    values.update(
        {
            "INFERA_GATEWAY_IMAGE": "example.invalid/gateway@sha256:" + "a" * 64,
            "INFERA_FRONTEND_IMAGE": "example.invalid/frontend@sha256:" + "b" * 64,
            "INFERA_WORKER_IMAGE": "example.invalid/worker@sha256:" + "c" * 64,
            "INFERA_GATEWAY_REPLICAS": "1",
            "INFERA_AUDIT_LEDGER_BACKEND": "postgres",
            "INFERA_AUDIT_LEDGER_DSN": "postgresql://test.invalid/infera",
            "INFERA_RECOVERY_API_PROTOCOL_VERSION": protocol,
            module.POLICY_NAME: cost,
        }
    )
    path.write_text(
        "".join(f"{name}={value}\n" for name, value in values.items()),
        encoding="utf-8",
    )
    path.chmod(0o600)


def write_manifest(path: Path, *, protocol: str) -> None:
    path.write_text(
        "\n".join(
            [
                "INFERA_RELEASE_ID=synthetic",
                "INFERA_GATEWAY_IMAGE=example.invalid/gateway@sha256:aaa",
                "INFERA_WORKER_IMAGE=example.invalid/worker@sha256:bbb",
                "INFERA_WORKER_PROTOCOL_VERSION=synthetic-worker",
                f"INFERA_RECOVERY_API_PROTOCOL_VERSION={protocol}",
                "INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=synthetic-ledger",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    path.chmod(0o600)


def run_cli(
    root: Path,
    *,
    manifest_protocol: str,
    production_protocol: str,
    policy_cost: str = "1.00",
    production_cost: str = "1.00",
) -> subprocess.CompletedProcess[str]:
    root = root.resolve()
    base_mount = mount(
        "/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile"
    )
    before = root / "before.json"
    after = root / "after.json"
    expected = root / "expectations.json"
    manifest = root / "manifest"
    policy = root / "policy"
    production_env = root / "production.env"
    before.write_text(json.dumps([container([base_mount])]), encoding="utf-8")
    after.write_text(json.dumps([container([base_mount])]), encoding="utf-8")
    expected.write_text(json.dumps(expectations()), encoding="utf-8")
    write_manifest(manifest, protocol=manifest_protocol)
    policy.write_text(f"{module.POLICY_NAME}={policy_cost}\n", encoding="utf-8")
    write_production_env(
        production_env, protocol=production_protocol, cost=production_cost
    )
    return subprocess.run(
        [
            sys.executable,
            str(MODULE_PATH),
            "verify",
            "--before",
            str(before),
            "--after",
            str(after),
            "--expectations",
            str(expected),
            "--manifest",
            str(manifest),
            "--policy",
            str(policy),
            "--production-env",
            str(production_env),
        ],
        text=True,
        capture_output=True,
        check=False,
    )


class RecoveryVerifierTests(unittest.TestCase):
    def test_correct_service_with_reordered_identical_mounts_pass(self) -> None:
        first = mount("/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile")
        second = mount("/opt/infera/data", "/app/data")
        result = module.compare_runtime_metadata(
            [container([first, second])],
            [container([second, first])],
            expectations(),
        )
        self.assertTrue(result["mount_sets_unchanged"])
        self.assertTrue(all(result.values()))

    def test_real_mount_drift_fails(self) -> None:
        first = mount("/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile")
        changed = mount("/opt/infera/deploy/caddy/other", "/etc/caddy/Caddyfile")
        result = module.compare_runtime_metadata(
            [container([first])], [container([changed])], expectations()
        )
        self.assertFalse(result["mount_sets_unchanged"])
        self.assertFalse(result["checked_in_mounts_after"])

    def test_checked_in_mount_must_be_on_expected_service(self) -> None:
        required = mount(
            "/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile"
        )
        gateway = container([required])
        caddy = container(
            [],
            id="caddy-id",
            labels=labels(
                **{
                    module.LABEL_SERVICE: "caddy",
                    module.LABEL_CONTAINER_NUMBER: "1",
                }
            ),
        )
        expected = expectations()
        expected["services"] = {"gateway": 1, "caddy": 1}
        expected["checked_in_mounts"][0]["service"] = "caddy"
        result = module.compare_runtime_metadata(
            [gateway, caddy], [gateway, caddy], expected
        )
        self.assertFalse(result["checked_in_mounts_before"])
        self.assertFalse(result["checked_in_mounts_after"])

    def test_container_scoped_mount_passes_only_on_that_replica(self) -> None:
        required = mount(
            "/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile"
        )
        first = container([required])
        second = container(
            [],
            id="container-id-2",
            labels=labels(**{module.LABEL_CONTAINER_NUMBER: "2"}),
        )
        expected = expectations()
        expected["services"] = {"gateway": 2}
        expected["checked_in_mounts"][0]["container_number"] = "1"
        result = module.compare_runtime_metadata(
            [first, second], [second, first], expected
        )
        self.assertTrue(result["checked_in_mounts_before"])
        self.assertTrue(result["checked_in_mounts_after"])
        self.assertTrue(all(result.values()))

    def test_invalid_mount_expectations_fail_closed(self) -> None:
        required = mount(
            "/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile"
        )
        duplicate = expectations()
        duplicate["checked_in_mounts"].append(
            dict(duplicate["checked_in_mounts"][0])
        )
        ambiguous = expectations()
        scoped = dict(ambiguous["checked_in_mounts"][0])
        scoped["container_number"] = "1"
        ambiguous["checked_in_mounts"].append(scoped)
        unknown = expectations()
        unknown["checked_in_mounts"][0]["service"] = "unknown"
        malformed = expectations()
        malformed["checked_in_mounts"][0]["unexpected"] = "sentinel"
        for invalid in (duplicate, ambiguous, unknown, malformed):
            with self.subTest(invalid=invalid), self.assertRaises(
                module.VerificationError
            ):
                module.compare_runtime_metadata(
                    [container([required])],
                    [container([required])],
                    invalid,
                )

    def test_missing_and_changed_labels_are_individual_failures(self) -> None:
        base_mount = mount(
            "/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile"
        )
        missing = labels()
        del missing[module.LABEL_CONFIG_HASH]
        changed = labels(**{module.LABEL_PROJECT: "other"})
        missing_result = module.compare_runtime_metadata(
            [container([base_mount], labels=missing)],
            [container([base_mount], labels=missing)],
            expectations(),
        )
        changed_result = module.compare_runtime_metadata(
            [container([base_mount], labels=changed)],
            [container([base_mount], labels=changed)],
            expectations(),
        )
        self.assertFalse(missing_result["label_config_hash_before"])
        self.assertTrue(missing_result["label_project_before"])
        self.assertFalse(changed_result["label_project_before"])
        self.assertTrue(changed_result["label_config_hash_before"])

    def test_path_and_serialization_mismatches_fail(self) -> None:
        path_mismatch = labels(
            **{module.LABEL_WORKING_DIR: "/opt/infera/../infera"}
        )
        serialization_mismatch = labels(
            **{
                module.LABEL_CONFIG_FILES: (
                    "/opt/infera/docker-compose.prod.yml,"
                    "/opt/infera/docker-compose.override.yml"
                )
            }
        )
        checks = module.label_checks(
            path_mismatch,
            project="infera",
            service="gateway",
            config_files="/opt/infera/docker-compose.prod.yml",
            working_dir="/opt/infera",
        )
        serialized = module.label_checks(
            serialization_mismatch,
            project="infera",
            service="gateway",
            config_files="/opt/infera/docker-compose.prod.yml",
            working_dir="/opt/infera",
        )
        self.assertFalse(checks["label_working_dir_path"])
        self.assertFalse(serialized["label_config_files_serialization"])

    def test_identity_start_restart_and_cardinality_drift_fail(self) -> None:
        base_mount = mount(
            "/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile"
        )
        changed = container(
            [base_mount],
            id="other-container",
            image_id="sha256:other",
            started_at="2026-07-30T00:01:00Z",
            restart_count=1,
        )
        result = module.compare_runtime_metadata(
            [container([base_mount])], [changed], expectations()
        )
        self.assertFalse(result["container_identity_unchanged"])
        self.assertFalse(result["image_identity_unchanged"])
        self.assertFalse(result["start_time_unchanged"])
        self.assertFalse(result["restart_count_unchanged"])

    def test_both_nonsecret_contract_values_are_required(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = root / "manifest"
            policy = root / "policy"
            manifest.write_text(
                "\n".join(
                    [
                        "INFERA_RELEASE_ID=synthetic",
                        "INFERA_GATEWAY_IMAGE=example.invalid/gateway@sha256:aaa",
                        "INFERA_WORKER_IMAGE=example.invalid/worker@sha256:bbb",
                        "INFERA_WORKER_PROTOCOL_VERSION=synthetic-worker",
                        "INFERA_RECOVERY_API_PROTOCOL_VERSION=synthetic-recovery",
                        "INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=synthetic-ledger",
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            manifest.chmod(0o600)
            values = module.load_nonsecret_contract(manifest, POLICY_PATH)
            self.assertEqual(len(values), 2)
            self.assertEqual(values[module.POLICY_NAME], "1.00")
            policy.write_text(
                f"{module.POLICY_NAME}=1.01\n", encoding="utf-8"
            )
            with self.assertRaises(module.VerificationError):
                module.load_nonsecret_contract(manifest, policy)

            policy.write_text(
                f"{module.POLICY_NAME}=1.00\n", encoding="utf-8"
            )
            linked_policy = root / "linked-policy"
            linked_policy.symlink_to(policy)
            with self.assertRaises(module.VerificationError):
                module.load_nonsecret_contract(manifest, linked_policy)

    def test_cli_does_not_leak_nonsecret_values(self) -> None:
        sentinel = "synthetic-private-protocol-sentinel"
        with tempfile.TemporaryDirectory() as directory:
            result = run_cli(
                Path(directory),
                manifest_protocol=sentinel,
                production_protocol=sentinel,
            )
            self.assertEqual(result.returncode, 0)
            self.assertIn("nonsecret_contract_matches=true", result.stdout)
            self.assertNotIn(sentinel, result.stdout)
            self.assertNotIn(sentinel, result.stderr)

    def test_mismatched_valid_protocol_fails_without_leaking(self) -> None:
        canonical = "canonical-protocol-sentinel"
        mismatched = "mismatched-protocol-sentinel"
        with tempfile.TemporaryDirectory() as directory:
            result = run_cli(
                Path(directory),
                manifest_protocol=canonical,
                production_protocol=mismatched,
            )
            self.assertEqual(result.returncode, 1)
            self.assertIn("nonsecret_contract_matches=false", result.stdout)
            for sentinel in (canonical, mismatched):
                self.assertNotIn(sentinel, result.stdout)
                self.assertNotIn(sentinel, result.stderr)

    def test_mismatched_valid_cost_fails_without_leaking(self) -> None:
        protocol = "exact-protocol-sentinel"
        with tempfile.TemporaryDirectory() as directory:
            result = run_cli(
                Path(directory),
                manifest_protocol=protocol,
                production_protocol=protocol,
                production_cost="0.50",
            )
            self.assertEqual(result.returncode, 1)
            self.assertIn("nonsecret_contract_matches=false", result.stdout)
            self.assertNotIn(protocol, result.stdout)
            self.assertNotIn(protocol, result.stderr)
            for value in ("0.50", "1.00"):
                self.assertNotIn(value, result.stdout)
                self.assertNotIn(value, result.stderr)

    def test_numerically_equivalent_cost_matches(self) -> None:
        protocol = "exact-protocol-sentinel"
        with tempfile.TemporaryDirectory() as directory:
            result = run_cli(
                Path(directory),
                manifest_protocol=protocol,
                production_protocol=protocol,
                policy_cost="1.00",
                production_cost="1.0",
            )
            self.assertEqual(result.returncode, 0)
            self.assertIn("nonsecret_contract_matches=true", result.stdout)


if __name__ == "__main__":
    unittest.main()
