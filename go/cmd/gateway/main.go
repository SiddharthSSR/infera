// Infera Gateway - HTTP API server
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
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
	"github.com/infera/infera/go/internal/vault"
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

	// Create router — batcher tuning via env vars for zero-rebuild tuning in production.
	routerConfig := router.DefaultConfig()
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
	r := router.New(routerConfig)

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

	instanceMgr, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
		WorkerImage:     workerImage,
		WorkerImages:    workerImages,
		GatewayAddress:  gatewayAddress,
		CostDBPath:      "data/costs.db",
	})
	if err != nil {
		log.Error("failed to initialize instance manager", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if err := instanceMgr.Close(); err != nil {
			log.Warn("failed to close instance manager", slog.String("error", err.Error()))
		}
	}()

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
	authStore, err := auth.NewStore("data/auth.db")
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

	// Initialize inference audit store (best-effort, non-fatal)
	auditStore, err := audit.NewStore("data/audit.db")
	if err != nil {
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
