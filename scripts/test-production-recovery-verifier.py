#!/usr/bin/env python3
"""Focused regression tests for production-recovery-verifier.py."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parent
MODULE_PATH = SCRIPT_DIR / "production-recovery-verifier.py"
POLICY_PATH = (
    REPO_ROOT / "deploy/production/infera-production-recovery-policy"
)
spec = importlib.util.spec_from_file_location("production_recovery_verifier", MODULE_PATH)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(module)


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
                "Type": "bind",
                "Source": "/opt/infera/deploy/caddy/Caddyfile",
                "Destination": "/etc/caddy/Caddyfile",
            }
        ],
    }


class RecoveryVerifierTests(unittest.TestCase):
    def test_reordered_identical_mounts_pass(self) -> None:
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
        base_mount = mount(
            "/opt/infera/deploy/caddy/Caddyfile", "/etc/caddy/Caddyfile"
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            before = root / "before.json"
            after = root / "after.json"
            expected = root / "expectations.json"
            manifest = root / "manifest"
            policy = root / "policy"
            before.write_text(json.dumps([container([base_mount])]), encoding="utf-8")
            after.write_text(json.dumps([container([base_mount])]), encoding="utf-8")
            expected.write_text(json.dumps(expectations()), encoding="utf-8")
            manifest.write_text(
                "\n".join(
                    [
                        "INFERA_RELEASE_ID=synthetic",
                        "INFERA_GATEWAY_IMAGE=example.invalid/gateway@sha256:aaa",
                        "INFERA_WORKER_IMAGE=example.invalid/worker@sha256:bbb",
                        "INFERA_WORKER_PROTOCOL_VERSION=synthetic-worker",
                        f"INFERA_RECOVERY_API_PROTOCOL_VERSION={sentinel}",
                        "INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=synthetic-ledger",
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            manifest.chmod(0o600)
            policy.write_text(
                f"{module.POLICY_NAME}=1.00\n", encoding="utf-8"
            )
            result = subprocess.run(
                [
                    "python3",
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
                ],
                text=True,
                capture_output=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0)
            self.assertNotIn(sentinel, result.stdout)
            self.assertNotIn(sentinel, result.stderr)


if __name__ == "__main__":
    unittest.main()
