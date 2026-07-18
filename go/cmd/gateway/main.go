// Infera Gateway - HTTP API server
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/infera/infera/go/internal/agents"
	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/gateway"
	"github.com/infera/infera/go/internal/providers"
	_ "github.com/infera/infera/go/internal/providers/e2e"
	"github.com/infera/infera/go/internal/providers/mock"
	_ "github.com/infera/infera/go/internal/providers/runpod"
	_ "github.com/infera/infera/go/internal/providers/vastai"
	"github.com/infera/infera/go/internal/router"
	routerregistry "github.com/infera/infera/go/internal/router/registry"
	"github.com/infera/infera/go/internal/vault"
	"github.com/infera/infera/go/pkg/types"
)

func main() {
	// Initialize structured logger
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	// Parse flags
	httpPort := flag.Int("port", 8080, "HTTP port")
	runpodKey := flag.String("runpod-key", os.Getenv("RUNPOD_API_KEY"), "RunPod API key")
	flag.Parse()

	log.Info("Starting Infera Gateway...")
	devMode := os.Getenv("INFERA_DEV_MODE") == "1"
	releaseID, workerProtocolVersion, err := rolloutIdentityFromEnv(devMode)
	if err != nil {
		log.Error("invalid coordinated rollout configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := validateAuditLedgerTopology(os.Getenv("INFERA_GATEWAY_REPLICAS"), os.Getenv("INFERA_AUDIT_LEDGER_BACKEND"), os.Getenv("INFERA_AUDIT_LEDGER_DSN")); err != nil {
		log.Error("invalid audit ledger topology", slog.String("error", err.Error()))
		os.Exit(1)
	}
	controlStateDSN := strings.TrimSpace(os.Getenv("INFERA_CONTROL_STATE_DSN"))
	if err := validateControlStateTopology(devMode, os.Getenv("INFERA_GATEWAY_REPLICAS"), controlStateDSN); err != nil {
		log.Error("invalid control-state topology", slog.String("error", err.Error()))
		os.Exit(1)
	}
	instanceStoreConfig, registryStoreConfig, err := controlStatePostgresConfigsFromEnv()
	if err != nil {
		log.Error("invalid control-state pool configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}
	postgresLedgerConfig, err := auditPostgresConfigFromEnv()
	if err != nil {
		log.Error("invalid audit ledger pool configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}
	var instanceMgr *providers.Manager

	// Create router — batcher tuning via env vars for zero-rebuild tuning in production.
	routerConfig := router.DefaultConfig()
	routerConfig, err = routingConfigFromEnv(routerConfig)
	if err != nil {
		log.Error("invalid routing configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}
	routerConfig.CostResolver = func(workerID string) (router.CostEvidence, bool, error) {
		if instanceMgr == nil {
			return router.CostEvidence{}, false, nil
		}
		snapshot, found, err := instanceMgr.GetPriceSnapshotForWorkerWithError(workerID)
		if err != nil || !found {
			return router.CostEvidence{}, false, err
		}
		if snapshot.Version != providers.PriceSnapshotVersionV1 ||
			snapshot.Currency != providers.PriceCurrencyUSD || snapshot.TimeUnit != providers.PriceTimeUnitHour ||
			snapshot.AmountNano <= 0 {
			return router.CostEvidence{}, false, nil
		}
		return router.CostEvidence{AmountNanoPerHour: snapshot.AmountNano, CapturedAt: snapshot.CapturedAt}, true, nil
	}
	if v := strings.TrimSpace(os.Getenv("INFERA_ENABLE_BATCHING")); v != "" {
		routerConfig.EnableBatching = parseBoolEnv(v, routerConfig.EnableBatching)
	}
	if v := strings.TrimSpace(os.Getenv("INFERA_MAX_BATCH_SIZE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			routerConfig.MaxBatchSize = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("INFERA_MAX_BATCH_WAIT_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			routerConfig.MaxBatchWaitMS = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("INFERA_AFFINITY_TTL_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n <= 0 {
				routerConfig.AffinityTTL = 0
			} else {
				routerConfig.AffinityTTL = time.Duration(n) * time.Second
			}
		}
	}
	var durableInstanceStore *providers.PostgresInstanceStore
	var durableWorkerRegistry *routerregistry.PostgresRegistry
	if controlStateDSN != "" {
		encryptionKey := strings.TrimSpace(os.Getenv("INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY"))
		if encryptionKey == "" {
			log.Error("INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY is required with INFERA_CONTROL_STATE_DSN")
			os.Exit(1)
		}
		durableInstanceStore, err = providers.NewPostgresInstanceStoreWithConfig(controlStateDSN, encryptionKey, instanceStoreConfig)
		if err != nil {
			log.Error("failed to initialize durable provider control state", slog.String("error", err.Error()))
			os.Exit(1)
		}
		durableWorkerRegistry, err = routerregistry.NewPostgresRegistry(controlStateDSN, registryStoreConfig)
		if err != nil {
			_ = durableInstanceStore.Close()
			log.Error("failed to initialize durable worker registry", slog.String("error", err.Error()))
			os.Exit(1)
		}
		reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), registryStoreConfig.QueryTimeout)
		err = durableWorkerRegistry.Reconcile(reconcileCtx)
		reconcileCancel()
		if err != nil {
			_ = durableWorkerRegistry.Close()
			_ = durableInstanceStore.Close()
			log.Error("failed to reconcile durable worker registry", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}
	var r *router.Router
	if durableWorkerRegistry != nil {
		r = router.NewWithRegistry(routerConfig, durableWorkerRegistry)
	} else {
		r = router.New(routerConfig)
	}

	// Create instance manager
	// Prefer explicitly pinned worker images for reproducible warm restarts.
	workerImage := strings.TrimSpace(os.Getenv("INFERA_WORKER_IMAGE"))
	workerImages := map[providers.InferenceEngine]string{
		providers.EngineVLLM:        strings.TrimSpace(os.Getenv("INFERA_WORKER_IMAGE_VLLM")),
		providers.EngineSGLang:      strings.TrimSpace(os.Getenv("INFERA_WORKER_IMAGE_SGLANG")),
		providers.EngineTensorRTLLM: strings.TrimSpace(os.Getenv("INFERA_WORKER_IMAGE_TENSORRT_LLM")),
		providers.EngineMock:        strings.TrimSpace(os.Getenv("INFERA_WORKER_IMAGE_MOCK")),
	}
	if workerImage == "" && allWorkerImagesEmpty(workerImages) {
		log.Warn("no worker image is configured; set INFERA_WORKER_IMAGE or engine-specific INFERA_WORKER_IMAGE_<ENGINE> values before non-mock provisioning")
	}
	enableMockProvider := os.Getenv("INFERA_DEV_MODE") == "1" || os.Getenv("INFERA_ENABLE_MOCK_PROVIDER") == "1"
	gatewayAddress := strings.TrimSpace(os.Getenv("INFERA_GATEWAY_ADDRESS"))
	if gatewayAddress == "" {
		gatewayAddress = fmt.Sprintf("localhost:%d", *httpPort)
	}
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Error("failed to create data directory", slog.String("error", err.Error()))
		os.Exit(1)
	}

	managerConfig := providers.ManagerConfig{
		DefaultProvider:           providers.ProviderMock,
		WorkerImage:               workerImage,
		WorkerImages:              workerImages,
		GatewayAddress:            gatewayAddress,
		ReleaseID:                 releaseID,
		WorkerProtocolVersion:     workerProtocolVersion,
		CostDBPath:                "data/costs.db",
		WorkerRegistrationTimeout: parseDurationEnv("INFERA_WORKER_REGISTRATION_TIMEOUT", 10*time.Minute),
	}
	if durableInstanceStore != nil {
		instanceMgr, err = providers.NewManagerWithStore(managerConfig, durableInstanceStore)
	} else {
		instanceMgr, err = providers.NewManager(managerConfig)
	}
	if err != nil {
		if durableWorkerRegistry != nil {
			_ = durableWorkerRegistry.Close()
		}
		if durableInstanceStore != nil {
			_ = durableInstanceStore.Close()
		}
		log.Error("failed to initialize instance manager", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if err := instanceMgr.Close(); err != nil {
			log.Warn("failed to close instance manager", slog.String("error", err.Error()))
		}
	}()
	if durableWorkerRegistry != nil {
		defer func() {
			if err := durableWorkerRegistry.Close(); err != nil {
				log.Warn("failed to close durable worker registry", slog.String("error", err.Error()))
			}
		}()
	}

	if enableMockProvider {
		instanceMgr.RegisterProvider(mock.New())
		log.Info("provider registered", slog.String("provider", "mock"))
	}

	// Register RunPod if API key provided
	if *runpodKey != "" {
		runpodProvider, err := providers.CreateProvider(providers.ProviderConfig{
			Type:   providers.ProviderRunPod,
			APIKey: *runpodKey,
		})
		if err != nil {
			log.Warn("failed to create RunPod provider", slog.String("error", err.Error()))
		} else {
			instanceMgr.RegisterProvider(runpodProvider)
			log.Info("provider registered", slog.String("provider", "runpod"))
		}
	}

	// Register Vast.ai if API key provided
	if vastaiKey := strings.TrimSpace(os.Getenv("VASTAI_API_KEY")); vastaiKey != "" {
		vastaiProvider, err := providers.CreateProvider(providers.ProviderConfig{
			Type:   providers.ProviderVastAI,
			APIKey: vastaiKey,
		})
		if err != nil {
			log.Warn("failed to create Vast.ai provider", slog.String("error", err.Error()))
		} else {
			instanceMgr.RegisterProvider(vastaiProvider)
			log.Info("provider registered", slog.String("provider", "vastai"))
		}
	}

	// Create gateway
	gatewayConfig := gateway.DefaultConfig()
	gatewayConfig.HTTPPort = *httpPort
	gatewayConfig.WorkerSharedToken = strings.TrimSpace(os.Getenv("INFERA_WORKER_SHARED_TOKEN"))
	gatewayConfig.ReleaseID = releaseID
	gatewayConfig.WorkerProtocolVersion = workerProtocolVersion
	gatewayConfig.RequireMatchingWorkerRelease = !devMode
	gatewayConfig.RateLimiter = parseRateLimiterConfigFromEnv()
	if v := strings.TrimSpace(os.Getenv("INFERA_MAX_IN_FLIGHT_REQUESTS")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			gatewayConfig.MaxInFlight = n
		}
	}
	if gatewayConfig.WorkerSharedToken == "" {
		log.Error("INFERA_WORKER_SHARED_TOKEN is required and cannot be empty")
		os.Exit(1)
	}
	if allowedOrigins := parseAllowedOrigins(os.Getenv("INFERA_ALLOWED_ORIGINS")); len(allowedOrigins) > 0 {
		gatewayConfig.AllowedOrigins = allowedOrigins
	}
	gw := gateway.New(gatewayConfig, r, instanceMgr)

	// Initialize vault (model registry)
	vaultStore, err := vault.NewStore("data/vault.db")
	if err != nil {
		log.Error("failed to initialize vault", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer vaultStore.Close()

	if err := vault.SeedDefaultModels(vaultStore); err != nil {
		log.Warn("failed to seed vault", slog.String("error", err.Error()))
	}

	gw.SetVaultHandler(vault.NewHandler(vaultStore))

	// Initialize auth (API key authentication)
	providerCredentialEncryptionKey := strings.TrimSpace(os.Getenv("INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY"))
	var authStore *auth.Store
	if providerCredentialEncryptionKey == "" {
		if os.Getenv("INFERA_DEV_MODE") != "1" {
			log.Error("INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY is required outside development mode")
			os.Exit(1)
		}
		log.Warn("workspace provider credential storage is disabled without INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY")
		authStore, err = auth.NewStore("data/auth.db")
	} else {
		authStore, err = auth.NewStoreWithProviderCredentialEncryption("data/auth.db", providerCredentialEncryptionKey)
	}
	if err != nil {
		log.Error("failed to initialize auth store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer authStore.Close()

	// Bootstrap admin key from env or auto-generate on first run
	keyCount, err := authStore.Count()
	if err != nil {
		log.Error("failed to count existing API keys", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if keyCount == 0 {
		adminKey := os.Getenv("INFERA_ADMIN_KEY")
		if adminKey != "" {
			if _, err := authStore.CreateKeyFromRaw(adminKey, "Bootstrap Admin", "admin"); err != nil {
				log.Error("failed to store bootstrap admin key", slog.String("error", err.Error()))
				os.Exit(1)
			}
			log.Info("admin key configured from INFERA_ADMIN_KEY")
		} else {
			fullKey, record, err := authStore.CreateKey("Auto Admin", "admin")
			if err != nil {
				log.Error("failed to generate admin key", slog.String("error", err.Error()))
				os.Exit(1)
			}
			if err := persistBootstrapAdminKey("data/bootstrap_admin_key.txt", fullKey); err != nil {
				if rollbackErr := authStore.DeleteKey(record.ID); rollbackErr != nil {
					log.Error("failed to rollback bootstrap admin key", slog.String("key_prefix", record.KeyPrefix), slog.String("error", rollbackErr.Error()))
				}
				log.Error("failed to persist bootstrap admin key", slog.String("error", err.Error()))
				os.Exit(1)
			}
			log.Info("auto-generated admin API key created",
				slog.String("key_prefix", record.KeyPrefix),
				slog.String("key_file", "data/bootstrap_admin_key.txt"),
			)
		}
	}

	authHandler := auth.NewHandler(authStore)
	authHandler.SetSecure(os.Getenv("INFERA_DEV_MODE") != "1")
	authHandler.SetProviderConfigValidator(func(ctx context.Context, config providers.ProviderConfig) error {
		provider, err := providers.CreateProvider(config)
		if err != nil {
			return err
		}
		status, err := provider.GetStatus(ctx)
		if err != nil {
			return err
		}
		if status == nil || !status.Connected {
			code := providers.ProviderErrorRequestFailed
			if status != nil && status.ErrorCode != "" {
				code = status.ErrorCode
			}
			return &providers.ProviderError{
				Provider: config.Type,
				Code:     code,
				Message:  "provider did not confirm the supplied credentials",
			}
		}
		return nil
	})
	gw.SetAuthHandler(authHandler)
	instanceMgr.SetWorkspaceProviderConfigResolver(func(workspaceID string, providerType providers.ProviderType) (*providers.ProviderConfig, error) {
		apiKey, apiSecret, endpoint, options, err := authStore.ResolveWorkspaceProviderConfig(workspaceID, string(providerType))
		if err != nil {
			if errors.Is(err, auth.ErrWorkspaceProviderConfigNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return &providers.ProviderConfig{
			Type:        providerType,
			APIKey:      apiKey,
			APISecret:   apiSecret,
			Endpoint:    endpoint,
			DefaultOpts: options,
		}, nil
	})

	// Quota admission and usage reconciliation depend on this ledger. Production
	// must not start in a superficially healthy state without it.
	auditStore, err := audit.NewLedgerWithPostgresConfig(
		os.Getenv("INFERA_AUDIT_LEDGER_BACKEND"),
		"data/audit.db",
		os.Getenv("INFERA_AUDIT_LEDGER_DSN"),
		postgresLedgerConfig,
	)
	if err != nil {
		if !devMode {
			log.Error("failed to initialize required audit and quota ledger", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Warn("failed to initialize audit store", slog.String("error", err.Error()))
	} else {
		defer auditStore.Close()
		gw.SetAuditStore(auditStore)
	}

	deploymentStore, err := deployments.NewStore("data/deployments.db")
	if err != nil {
		log.Warn("failed to initialize deployment store", slog.String("error", err.Error()))
	} else {
		defer deploymentStore.Close()
		gw.SetDeploymentStore(deploymentStore)
	}

	agentStore, err := agents.NewStore("data/agents.db")
	if err != nil {
		log.Warn("failed to initialize agents store", slog.String("error", err.Error()))
	} else {
		defer agentStore.Close()
		agentRuntime, err := gw.NewAgentsRuntime(agentStore)
		if err != nil {
			log.Warn("failed to initialize agents runtime", slog.String("error", err.Error()))
		} else {
			gw.SetAgentRuntime(agentRuntime)
		}
	}

	// Handle shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start background instance refresh loop
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := instanceMgr.RefreshInstances(ctx); err != nil {
					log.Warn("failed to refresh instances", slog.String("error", err.Error()))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		<-sigChan
		log.Info("shutting down...")

		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
		defer shutdownCancel()

		if err := gw.Stop(shutdownCtx); err != nil {
			log.Error("error during shutdown", slog.String("error", err.Error()))
		}

		r.Stop()
		cancel()
		log.Info("shutdown complete")
	}()

	// Start gateway
	log.Info("gateway listening", slog.Int("port", *httpPort), slog.Any("providers", instanceMgr.ListProviders()))
	if err := gw.Start(); err != nil {
		log.Error("gateway error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func parseAllowedOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			out = append(out, origin)
		}
	}
	return out
}

func rolloutIdentityFromEnv(devMode bool) (string, string, error) {
	releaseID := strings.TrimSpace(os.Getenv("INFERA_RELEASE_ID"))
	protocolVersion := strings.TrimSpace(os.Getenv("INFERA_WORKER_PROTOCOL_VERSION"))
	if devMode {
		if releaseID == "" {
			releaseID = "dev"
		}
		if protocolVersion == "" {
			protocolVersion = "1"
		}
		return releaseID, protocolVersion, nil
	}
	if releaseID == "" {
		return "", "", errors.New("INFERA_RELEASE_ID is required outside development mode")
	}
	if protocolVersion == "" {
		return "", "", errors.New("INFERA_WORKER_PROTOCOL_VERSION is required outside development mode")
	}
	return releaseID, protocolVersion, nil
}

func routingConfigFromEnv(config router.Config) (router.Config, error) {
	if raw := strings.TrimSpace(os.Getenv("INFERA_ROUTING_STRATEGY")); raw != "" {
		strategyType := types.StrategyType(strings.ToLower(raw))
		switch strategyType {
		case types.StrategyLeastLoaded, types.StrategyRoundRobin, types.StrategyLatencyBased, types.StrategyMinCostUnderLatencySLO:
			config.DefaultStrategy = strategyType
		default:
			return router.Config{}, fmt.Errorf("INFERA_ROUTING_STRATEGY %q is not supported", raw)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("INFERA_ROUTING_LATENCY_SLO_MS")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return router.Config{}, errors.New("INFERA_ROUTING_LATENCY_SLO_MS must be a positive finite number")
		}
		config.LatencySLOMS = value
	}
	maxAge, err := parseOptionalDurationEnv("INFERA_ROUTING_EVIDENCE_MAX_AGE", config.EvidenceMaxAge)
	if err != nil {
		return router.Config{}, err
	}
	config.EvidenceMaxAge = maxAge
	return config, nil
}

func validateAuditLedgerTopology(rawReplicas, rawBackend, rawDSN string) error {
	replicas := 1
	if value := strings.TrimSpace(rawReplicas); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return errors.New("INFERA_GATEWAY_REPLICAS must be a positive integer")
		}
		replicas = parsed
	}
	backend := strings.ToLower(strings.TrimSpace(rawBackend))
	if backend == "" {
		backend = "sqlite"
	}
	if backend != "sqlite" && backend != "postgres" && backend != "postgresql" {
		return fmt.Errorf("INFERA_AUDIT_LEDGER_BACKEND %q is not supported by this release", backend)
	}
	if replicas > 1 && backend == "sqlite" {
		return errors.New("multiple gateway replicas require a shared transactional audit ledger; configure the postgres backend")
	}
	if (backend == "postgres" || backend == "postgresql") && strings.TrimSpace(rawDSN) == "" {
		return errors.New("INFERA_AUDIT_LEDGER_DSN is required for the postgres audit ledger")
	}
	return nil
}

func validateControlStateTopology(devMode bool, rawReplicas, rawDSN string) error {
	replicas := 1
	if value := strings.TrimSpace(rawReplicas); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return errors.New("INFERA_GATEWAY_REPLICAS must be a positive integer")
		}
		replicas = parsed
	}
	if strings.TrimSpace(rawDSN) == "" && (!devMode || replicas > 1) {
		return errors.New("INFERA_CONTROL_STATE_DSN is required outside development mode and for multiple gateway replicas")
	}
	return nil
}

func controlStatePostgresConfigsFromEnv() (providers.PostgresInstanceStoreConfig, routerregistry.PostgresRegistryConfig, error) {
	queryTimeout, err := parseOptionalDurationEnv("INFERA_CONTROL_STATE_QUERY_TIMEOUT", 5*time.Second)
	if err != nil {
		return providers.PostgresInstanceStoreConfig{}, routerregistry.PostgresRegistryConfig{}, err
	}
	maxOpen, err := parseOptionalIntEnv("INFERA_CONTROL_STATE_MAX_OPEN_CONNS", 20, false)
	if err != nil {
		return providers.PostgresInstanceStoreConfig{}, routerregistry.PostgresRegistryConfig{}, err
	}
	maxIdleFallback := min(5, maxOpen)
	maxIdle, err := parseOptionalIntEnv("INFERA_CONTROL_STATE_MAX_IDLE_CONNS", maxIdleFallback, true)
	if err != nil {
		return providers.PostgresInstanceStoreConfig{}, routerregistry.PostgresRegistryConfig{}, err
	}
	if maxIdle > maxOpen {
		return providers.PostgresInstanceStoreConfig{}, routerregistry.PostgresRegistryConfig{}, errors.New("INFERA_CONTROL_STATE_MAX_IDLE_CONNS cannot exceed INFERA_CONTROL_STATE_MAX_OPEN_CONNS")
	}
	if strings.TrimSpace(os.Getenv("INFERA_CONTROL_STATE_MAX_IDLE_CONNS")) == "0" {
		// Store normalizers use zero as "unset" and negative as the explicit
		// zero-idle sentinel.
		maxIdle = -1
	}
	connMaxLifetime, err := parseOptionalDurationEnv("INFERA_CONTROL_STATE_CONN_MAX_LIFETIME", 30*time.Minute)
	if err != nil {
		return providers.PostgresInstanceStoreConfig{}, routerregistry.PostgresRegistryConfig{}, err
	}
	instanceConfig := providers.PostgresInstanceStoreConfig{
		QueryTimeout:    queryTimeout,
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxIdle,
		ConnMaxLifetime: connMaxLifetime,
	}
	registryConfig := routerregistry.PostgresRegistryConfig{
		RegistryConfig:  routerregistry.DefaultRegistryConfig(),
		QueryTimeout:    queryTimeout,
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxIdle,
		ConnMaxLifetime: connMaxLifetime,
	}
	return instanceConfig, registryConfig, nil
}

func auditPostgresConfigFromEnv() (audit.PostgresConfig, error) {
	config := audit.DefaultPostgresConfig()
	var err error
	config.MaxOpenConns, err = parseOptionalIntEnv("INFERA_AUDIT_LEDGER_MAX_OPEN_CONNS", config.MaxOpenConns, false)
	if err != nil {
		return audit.PostgresConfig{}, err
	}
	config.MaxIdleConns, err = parseOptionalIntEnv("INFERA_AUDIT_LEDGER_MAX_IDLE_CONNS", config.MaxIdleConns, true)
	if err != nil {
		return audit.PostgresConfig{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("INFERA_AUDIT_LEDGER_CONN_MAX_LIFETIME")); raw != "" {
		config.ConnMaxLifetime, err = time.ParseDuration(raw)
		if err != nil || config.ConnMaxLifetime <= 0 {
			return audit.PostgresConfig{}, errors.New("INFERA_AUDIT_LEDGER_CONN_MAX_LIFETIME must be a positive duration")
		}
	}
	if err := config.Validate(); err != nil {
		return audit.PostgresConfig{}, err
	}
	return config, nil
}

func parseOptionalIntEnv(name string, fallback int, allowZero bool) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || (!allowZero && value == 0) {
		if allowZero {
			return 0, fmt.Errorf("%s must be a non-negative integer", name)
		}
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func parseOptionalDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func allWorkerImagesEmpty(images map[providers.InferenceEngine]string) bool {
	for _, image := range images {
		if strings.TrimSpace(image) != "" {
			return false
		}
	}
	return true
}

func parseBoolEnv(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseDurationEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func parseRateLimiterConfigFromEnv() *gateway.RateLimiterConfig {
	cfg := gateway.DefaultRateLimiterConfig()
	changed := false

	if v := strings.TrimSpace(os.Getenv("INFERA_RATE_LIMIT_REQUESTS_PER_MINUTE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RequestsPerMinute = n
			changed = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("INFERA_RATE_LIMIT_BURST_SIZE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.BurstSize = n
			changed = true
		}
	}

	if changed {
		return &cfg
	}
	return nil
}

func persistBootstrapAdminKey(path, fullKey string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("bootstrap key file already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".bootstrap_admin_key.*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	if _, err := fmt.Fprintf(tmpFile, "%s\n", fullKey); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
