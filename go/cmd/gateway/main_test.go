package main

import (
	"strings"
	"testing"
)

func TestRolloutIdentityRequiresProductionValues(t *testing.T) {
	t.Setenv("INFERA_RELEASE_ID", "")
	t.Setenv("INFERA_WORKER_PROTOCOL_VERSION", "")
	if _, _, err := rolloutIdentityFromEnv(false); err == nil {
		t.Fatal("expected production rollout identity validation to fail")
	}

	t.Setenv("INFERA_RELEASE_ID", "release-2026-07-16")
	t.Setenv("INFERA_WORKER_PROTOCOL_VERSION", "1")
	releaseID, protocolVersion, err := rolloutIdentityFromEnv(false)
	if err != nil {
		t.Fatalf("rolloutIdentityFromEnv: %v", err)
	}
	if releaseID != "release-2026-07-16" || protocolVersion != "1" {
		t.Fatalf("unexpected rollout identity: release=%q protocol=%q", releaseID, protocolVersion)
	}
}

func TestRolloutIdentityUsesDevelopmentDefaults(t *testing.T) {
	t.Setenv("INFERA_RELEASE_ID", "")
	t.Setenv("INFERA_WORKER_PROTOCOL_VERSION", "")
	releaseID, protocolVersion, err := rolloutIdentityFromEnv(true)
	if err != nil {
		t.Fatalf("rolloutIdentityFromEnv: %v", err)
	}
	if releaseID != "dev" || protocolVersion != "1" {
		t.Fatalf("unexpected development defaults: release=%q protocol=%q", releaseID, protocolVersion)
	}
}

func TestAuditLedgerTopologyRejectsUnsafeReplicas(t *testing.T) {
	if err := validateAuditLedgerTopology("1", "sqlite"); err != nil {
		t.Fatalf("single-replica sqlite should be valid: %v", err)
	}
	if err := validateAuditLedgerTopology("2", "sqlite"); err == nil || !strings.Contains(err.Error(), "shared transactional audit ledger") {
		t.Fatalf("expected shared-ledger rejection, got %v", err)
	}
	if err := validateAuditLedgerTopology("1", "postgres"); err == nil {
		t.Fatal("expected unsupported backend rejection")
	}
}
