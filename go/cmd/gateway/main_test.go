package main

import (
	"strings"
	"testing"
	"time"
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

func TestAuditPostgresConfigFromEnv(t *testing.T) {
	t.Setenv("INFERA_AUDIT_LEDGER_MAX_OPEN_CONNS", "")
	t.Setenv("INFERA_AUDIT_LEDGER_MAX_IDLE_CONNS", "")
	t.Setenv("INFERA_AUDIT_LEDGER_CONN_MAX_LIFETIME", "")
	config, err := auditPostgresConfigFromEnv()
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if config.MaxOpenConns != 20 || config.MaxIdleConns != 5 || config.ConnMaxLifetime != 30*time.Minute {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	t.Setenv("INFERA_AUDIT_LEDGER_MAX_OPEN_CONNS", "12")
	t.Setenv("INFERA_AUDIT_LEDGER_MAX_IDLE_CONNS", "3")
	t.Setenv("INFERA_AUDIT_LEDGER_CONN_MAX_LIFETIME", "10m")
	config, err = auditPostgresConfigFromEnv()
	if err != nil {
		t.Fatalf("configured values: %v", err)
	}
	if config.MaxOpenConns != 12 || config.MaxIdleConns != 3 || config.ConnMaxLifetime != 10*time.Minute {
		t.Fatalf("unexpected configured values: %+v", config)
	}
}

func TestAuditPostgresConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{name: "zero open", env: "INFERA_AUDIT_LEDGER_MAX_OPEN_CONNS", value: "0"},
		{name: "negative idle", env: "INFERA_AUDIT_LEDGER_MAX_IDLE_CONNS", value: "-1"},
		{name: "bad lifetime", env: "INFERA_AUDIT_LEDGER_CONN_MAX_LIFETIME", value: "forever"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, tt.value)
			if _, err := auditPostgresConfigFromEnv(); err == nil {
				t.Fatal("expected invalid pool configuration to fail")
			}
		})
	}
	t.Run("idle exceeds open", func(t *testing.T) {
		t.Setenv("INFERA_AUDIT_LEDGER_MAX_OPEN_CONNS", "2")
		t.Setenv("INFERA_AUDIT_LEDGER_MAX_IDLE_CONNS", "3")
		if _, err := auditPostgresConfigFromEnv(); err == nil {
			t.Fatal("expected max idle above max open to fail")
		}
	})
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
	if err := validateAuditLedgerTopology("1", "sqlite", ""); err != nil {
		t.Fatalf("single-replica sqlite should be valid: %v", err)
	}
	if err := validateAuditLedgerTopology("2", "sqlite", ""); err == nil || !strings.Contains(err.Error(), "shared transactional audit ledger") {
		t.Fatalf("expected shared-ledger rejection, got %v", err)
	}
	if err := validateAuditLedgerTopology("2", "postgres", ""); err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("expected missing DSN rejection, got %v", err)
	}
	if err := validateAuditLedgerTopology("3", "postgres", "postgres://ledger"); err != nil {
		t.Fatalf("multi-replica postgres should be valid: %v", err)
	}
	if err := validateAuditLedgerTopology("1", "mysql", "mysql://ledger"); err == nil {
		t.Fatal("expected unsupported backend rejection")
	}
}

func TestControlStateTopologyRequiresDurabilityInProductionAndAcrossReplicas(t *testing.T) {
	if err := validateControlStateTopology(true, "1", ""); err != nil {
		t.Fatalf("single-replica development should allow in-memory state: %v", err)
	}
	if err := validateControlStateTopology(false, "1", ""); err == nil || !strings.Contains(err.Error(), "INFERA_CONTROL_STATE_DSN") {
		t.Fatalf("expected production DSN requirement, got %v", err)
	}
	if err := validateControlStateTopology(true, "2", ""); err == nil || !strings.Contains(err.Error(), "INFERA_CONTROL_STATE_DSN") {
		t.Fatalf("expected multi-replica DSN requirement, got %v", err)
	}
	if err := validateControlStateTopology(false, "2", "postgres://control"); err != nil {
		t.Fatalf("shared production control state should be valid: %v", err)
	}
}

func TestControlStatePostgresConfigsFromEnv(t *testing.T) {
	t.Setenv("INFERA_CONTROL_STATE_QUERY_TIMEOUT", "2s")
	t.Setenv("INFERA_CONTROL_STATE_MAX_OPEN_CONNS", "12")
	t.Setenv("INFERA_CONTROL_STATE_MAX_IDLE_CONNS", "3")
	t.Setenv("INFERA_CONTROL_STATE_CONN_MAX_LIFETIME", "10m")
	instanceConfig, registryConfig, err := controlStatePostgresConfigsFromEnv()
	if err != nil {
		t.Fatalf("controlStatePostgresConfigsFromEnv: %v", err)
	}
	if instanceConfig.QueryTimeout != 2*time.Second || instanceConfig.MaxOpenConns != 12 || instanceConfig.MaxIdleConns != 3 || instanceConfig.ConnMaxLifetime != 10*time.Minute {
		t.Fatalf("unexpected instance config: %+v", instanceConfig)
	}
	if registryConfig.QueryTimeout != instanceConfig.QueryTimeout || registryConfig.MaxOpenConns != instanceConfig.MaxOpenConns || registryConfig.MaxIdleConns != instanceConfig.MaxIdleConns || registryConfig.ConnMaxLifetime != instanceConfig.ConnMaxLifetime {
		t.Fatalf("registry config diverged: %+v", registryConfig)
	}

	t.Setenv("INFERA_CONTROL_STATE_MAX_OPEN_CONNS", "2")
	t.Setenv("INFERA_CONTROL_STATE_MAX_IDLE_CONNS", "3")
	if _, _, err := controlStatePostgresConfigsFromEnv(); err == nil {
		t.Fatal("expected invalid pool bounds to fail")
	}
}
