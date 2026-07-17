package providers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresInstanceStoreCrossConnectionRestartAndBinding(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	key := randomEncodedKey(t)
	storeA, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatalf("open store A: %v", err)
	}
	defer storeA.Close()
	storeB, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatalf("open store B: %v", err)
	}
	defer storeB.Close()

	metadataA, err := storeA.ControlStateMetadata()
	if err != nil {
		t.Fatalf("metadata A: %v", err)
	}
	metadataB, err := storeB.ControlStateMetadata()
	if err != nil {
		t.Fatalf("metadata B: %v", err)
	}
	if metadataA != metadataB || metadataA.ClusterID == "" || metadataA.SchemaVersion != controlStateSchemaVersion || metadataA.WriterProtocol != controlStateWriterProtocol {
		t.Fatalf("replicas do not share complete control-state metadata: A=%+v B=%+v", metadataA, metadataB)
	}

	credential := "deployment-token-cross-connection"
	instance := &Instance{
		ID: "inst-a", ProviderID: "provider-a", Provider: ProviderRunPod,
		WorkspaceID: "ws-a", Name: "worker-a", Status: InstanceStatusRunning,
		Models: []string{"model-a"}, Engine: EngineVLLM, WorkerCredential: credential,
		CreatedAt: time.Now().UTC(),
	}
	if err := storeA.put(instance); err != nil {
		t.Fatalf("persist instance: %v", err)
	}
	var ciphertext string
	if err := storeA.db.QueryRow(`SELECT worker_credential_ciphertext FROM managed_instances WHERE id = $1`, instance.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if ciphertext == credential || strings.Contains(ciphertext, credential) || !strings.HasPrefix(ciphertext, "enc:v1:") {
		t.Fatalf("worker credential was not safely encrypted at rest: %q", ciphertext)
	}

	managerB, err := NewManagerWithStore(ManagerConfig{DefaultProvider: ProviderMock}, storeB)
	if err != nil {
		t.Fatalf("manager B: %v", err)
	}
	defer managerB.Close()
	authenticated, ok, err := managerB.AuthenticateWorkerToken(credential)
	if err != nil || !ok || authenticated.ID != instance.ID {
		t.Fatalf("cross-connection authentication: instance=%+v ok=%v err=%v", authenticated, ok, err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []string{"worker-a", "worker-b"} {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- func() error {
				store := storeA
				if candidate == "worker-b" {
					store = storeB
				}
				return store.linkWorker(instance.ID, candidate, time.Now().UTC())
			}()
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var succeeded int
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one concurrent worker binding, got %d", succeeded)
	}
	bound, found, err := storeB.get(instance.ID)
	if err != nil || !found || bound.WorkerID == "" {
		t.Fatalf("read durable binding: instance=%+v found=%v err=%v", bound, found, err)
	}
	outbound, found, err := managerB.WorkerCredentialForWorker(bound.WorkerID)
	if err != nil || !found || outbound != credential {
		t.Fatalf("reconstruct outbound credential: found=%v err=%v credential_match=%v", found, err, outbound == credential)
	}

	if err := storeA.Close(); err != nil {
		t.Fatalf("close store A before restart: %v", err)
	}
	storeA = nil
	restarted, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatalf("restart store: %v", err)
	}
	defer restarted.Close()
	restartedInstance, found, err := restarted.get(instance.ID)
	if err != nil || !found || restartedInstance.WorkerCredential != credential || restartedInstance.WorkerID != bound.WorkerID {
		t.Fatalf("restart did not reconstruct instance and credential: found=%v err=%v instance=%+v", found, err, restartedInstance)
	}
}

func TestPostgresInstanceStoreRejectsWrongEncryptionKey(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	store, err := NewPostgresInstanceStore(dsn, randomEncodedKey(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.put(&Instance{
		ID: "wrong-key", ProviderID: "wrong-key-provider", Provider: ProviderRunPod,
		WorkspaceID: "ws-key", Status: InstanceStatusRunning,
		WorkerCredential: "secret-for-right-key", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed encrypted instance: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}
	if _, err := NewPostgresInstanceStore(dsn, randomEncodedKey(t)); err == nil || !strings.Contains(err.Error(), "could not be decrypted") {
		t.Fatalf("expected wrong key to fail closed, got %v", err)
	}
}

func TestPostgresInstanceStoreUniqueWorkerBindingAcrossInstances(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	store, err := NewPostgresInstanceStore(dsn, randomEncodedKey(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	for _, id := range []string{"instance-1", "instance-2"} {
		if err := store.put(&Instance{
			ID: id, ProviderID: "provider-" + id, Provider: ProviderRunPod,
			WorkspaceID: "ws-" + id, Status: InstanceStatusRunning,
			WorkerCredential: "credential-" + id, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("persist %s: %v", id, err)
		}
	}
	if err := store.linkWorker("instance-1", "shared-worker", time.Now().UTC()); err != nil {
		t.Fatalf("bind first instance: %v", err)
	}
	if err := store.linkWorker("instance-2", "shared-worker", time.Now().UTC()); err == nil {
		t.Fatal("expected unique worker binding to reject second instance")
	}
}

func TestPostgresInstanceStoreRejectsIncompatibleWriterProtocol(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	key := randomEncodedKey(t)
	store, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE control_state_metadata SET value = '999' WHERE key = 'writer_protocol'`); err != nil {
		t.Fatalf("seed incompatible writer protocol: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := NewPostgresInstanceStore(dsn, key); err == nil || !strings.Contains(err.Error(), "writer protocol") {
		t.Fatalf("expected incompatible writer protocol to fail closed, got %v", err)
	}
}

func TestPostgresInstanceStoreRejectsIncompatibleSchemaVersion(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	key := randomEncodedKey(t)
	store, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE control_state_metadata SET value = '999' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("seed incompatible schema version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := NewPostgresInstanceStore(dsn, key); err == nil || !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("expected incompatible schema version to fail closed, got %v", err)
	}
}

func TestPostgresInstanceStoreInactiveCredentialsFailClosed(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	store, err := NewPostgresInstanceStore(dsn, randomEncodedKey(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	for _, status := range []InstanceStatus{
		InstanceStatusStopping, InstanceStatusStopped, InstanceStatusTerminating,
		InstanceStatusTerminated, InstanceStatusError, "", "future_corrupt",
	} {
		id := "inactive-" + string(status)
		credential := "credential-" + string(status)
		hash := sha256.Sum256([]byte(credential))
		if err := store.put(&Instance{
			ID: id, ProviderID: "provider-" + id, Provider: ProviderRunPod,
			WorkspaceID: "ws-inactive", Status: status, WorkerID: "worker-" + id,
			WorkerCredential: credential, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("persist %s: %v", status, err)
		}
		if instance, found, err := store.authenticateWorkerTokenHash(hash); err != nil || found || instance != nil {
			t.Fatalf("inactive %s authenticated: instance=%+v found=%v err=%v", status, instance, found, err)
		}
		if credential, found, err := store.workerCredentialForWorker("worker-" + id); err != nil || found || credential != "" {
			t.Fatalf("inactive %s returned outbound credential: found=%v err=%v", status, found, err)
		}
	}
	for _, status := range []InstanceStatus{InstanceStatusPending, InstanceStatusProvisioning, InstanceStatusRunning} {
		id := "active-" + string(status)
		credential := "credential-" + string(status)
		hash := sha256.Sum256([]byte(credential))
		if err := store.put(&Instance{
			ID: id, ProviderID: "provider-" + id, Provider: ProviderRunPod,
			WorkspaceID: "ws-active", Status: status, WorkerID: "worker-" + id,
			WorkerCredential: credential, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("persist active %s: %v", status, err)
		}
		if _, found, err := store.authenticateWorkerTokenHash(hash); err != nil || !found {
			t.Fatalf("active %s did not authenticate: found=%v err=%v", status, found, err)
		}
		if outbound, found, err := store.workerCredentialForWorker("worker-" + id); err != nil || !found || outbound != credential {
			t.Fatalf("active %s outbound credential failed: found=%v err=%v", status, found, err)
		}
	}
}

func TestPostgresInstanceStoreUpdateCannotEraseConcurrentWorkerLink(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	key := randomEncodedKey(t)
	storeA, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatalf("open store A: %v", err)
	}
	defer storeA.Close()
	storeB, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatalf("open store B: %v", err)
	}
	defer storeB.Close()
	if err := storeA.put(&Instance{
		ID: "interleaved", ProviderID: "provider-interleaved", Provider: ProviderRunPod,
		WorkspaceID: "ws-interleaved", Status: InstanceStatusRunning,
		WorkerCredential: "credential-interleaved", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	loaded := make(chan struct{})
	release := make(chan struct{})
	storeA.updateLoaded = func() {
		close(loaded)
		<-release
	}
	updateResult := make(chan error, 1)
	go func() {
		_, err := storeA.update("interleaved", func(instance *Instance) {
			instance.ErrorMessage = "provider refresh completed"
		})
		updateResult <- err
	}()
	<-loaded
	competing, err := storeB.db.Begin()
	if err != nil {
		t.Fatalf("begin competing transaction: %v", err)
	}
	var lockedID string
	err = competing.QueryRow(`SELECT id FROM managed_instances WHERE id = $1 FOR UPDATE NOWAIT`, "interleaved").Scan(&lockedID)
	_ = competing.Rollback()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("expected competing connection to observe PostgreSQL row lock, got %v", err)
	}
	linkResult := make(chan error, 1)
	go func() { linkResult <- storeB.linkWorker("interleaved", "worker-interleaved", time.Now().UTC()) }()
	close(release)
	if err := <-updateResult; err != nil {
		t.Fatalf("generic update: %v", err)
	}
	if err := <-linkResult; err != nil {
		t.Fatalf("link after update: %v", err)
	}
	stored, found, err := storeA.get("interleaved")
	if err != nil || !found {
		t.Fatalf("read interleaved instance: found=%v err=%v", found, err)
	}
	if stored.WorkerID != "worker-interleaved" || stored.ErrorMessage != "provider refresh completed" {
		t.Fatalf("concurrent mutation was lost: %+v", stored)
	}
}

func TestPostgresInstanceStoreVersionFenceRejectsCrossConnectionStaleUpdate(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	key := randomEncodedKey(t)
	storeA, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	storeB, err := NewPostgresInstanceStore(dsn, key)
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()
	if err := storeA.put(&Instance{
		ID: "version-fence", ProviderID: "provider-version-fence", Provider: ProviderRunPod,
		WorkspaceID: "ws-version-fence", Status: InstanceStatusRunning,
		WorkerCredential: "credential-version-fence", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	observed, found, err := storeA.get("version-fence")
	if err != nil || !found {
		t.Fatalf("read observed version: found=%v err=%v", found, err)
	}
	if err := storeB.linkWorker("version-fence", "worker-version-fence", time.Now().UTC()); err != nil {
		t.Fatalf("concurrent link: %v", err)
	}
	afterLink, found, err := storeA.get("version-fence")
	if err != nil || !found {
		t.Fatalf("read linked instance: found=%v err=%v", found, err)
	}
	if afterLink.WorkerID != "worker-version-fence" || afterLink.LifecycleVersion != observed.LifecycleVersion {
		t.Fatalf("generic worker link changed lifecycle version: before=%+v after=%+v", observed, afterLink)
	}
	heartbeatAt := time.Now().UTC().Add(time.Second)
	heartbeatUpdated, err := storeB.update("version-fence", func(instance *Instance) {
		instance.WorkerLastHeartbeatAt = &heartbeatAt
	})
	if err != nil || !heartbeatUpdated {
		t.Fatalf("record generic heartbeat: updated=%v err=%v", heartbeatUpdated, err)
	}
	afterHeartbeat, found, err := storeA.get("version-fence")
	if err != nil || !found {
		t.Fatalf("read heartbeat instance: found=%v err=%v", found, err)
	}
	if afterHeartbeat.WorkerID != "worker-version-fence" || afterHeartbeat.WorkerLastHeartbeatAt == nil ||
		!afterHeartbeat.WorkerLastHeartbeatAt.Equal(heartbeatAt) || afterHeartbeat.LifecycleVersion != observed.LifecycleVersion {
		t.Fatalf("generic heartbeat changed lifecycle version or lost worker state: %+v", afterHeartbeat)
	}
	advanced, err := storeB.updateLifecycle("version-fence", func(instance *Instance) {
		instance.Status = InstanceStatusStopped
	})
	if err != nil || !advanced {
		t.Fatalf("advance lifecycle: updated=%v err=%v", advanced, err)
	}
	updated, err := storeA.updateIfLifecycleVersion("version-fence", observed.LifecycleVersion, func(instance *Instance) {
		instance.Status = InstanceStatusProvisioning
	})
	if err != nil {
		t.Fatalf("stale versioned update: %v", err)
	}
	if updated {
		t.Fatal("stale cross-connection version unexpectedly updated the row")
	}
	stored, found, err := storeA.get("version-fence")
	if err != nil || !found {
		t.Fatalf("read final version: found=%v err=%v", found, err)
	}
	if stored.WorkerID != "worker-version-fence" || stored.Status != InstanceStatusStopped || stored.LifecycleVersion <= observed.LifecycleVersion {
		t.Fatalf("version fence did not preserve newer mutation: %+v", stored)
	}
}

func TestManagerControlStateOutageFailsBeforeDuplicateProvisioning(t *testing.T) {
	dsn := isolatedControlStateDSN(t)
	store, err := NewPostgresInstanceStore(dsn, randomEncodedKey(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	manager, err := NewManagerWithStore(ManagerConfig{DefaultProvider: ProviderMock}, store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	provider := newMockTestProvider()
	manager.RegisterProvider(provider)
	if err := store.Close(); err != nil {
		t.Fatalf("close control state: %v", err)
	}
	if _, err := manager.Provision(context.Background(), &ProvisionRequest{
		Name: "must-not-provision", Provider: ProviderMock, GPUType: GPURTX4090,
		GPUCount: 1, Models: []string{"model-a"},
	}); !errors.Is(err, ErrControlStateUnavailable) {
		t.Fatalf("expected unavailable control state, got %v", err)
	}
	if provider.lastReq != nil {
		t.Fatal("provider was called after reusable-instance lookup failed")
	}
	if _, _, err := manager.GetInstanceWithError("missing"); !errors.Is(err, ErrControlStateUnavailable) {
		t.Fatalf("closed store looked like not-found: %v", err)
	}
}

func isolatedControlStateDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INFERA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	schema := "control_state_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL admin connection: %v", err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		_ = admin.Close()
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
		_ = admin.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func randomEncodedKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate encryption key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}
