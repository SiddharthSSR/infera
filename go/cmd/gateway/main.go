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
	"strings"
	"syscall"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/gateway"
	"github.com/infera/infera/go/internal/providers"
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

	// Create router
	routerConfig := router.DefaultConfig()
	r := router.New(routerConfig)

	// Create instance manager
	// Prefer an explicitly pinned worker image for reproducible warm restarts.
	workerImage := strings.TrimSpace(os.Getenv("INFERA_WORKER_IMAGE"))
	if workerImage == "" {
		log.Warn("INFERA_WORKER_IMAGE is not set; non-mock provisioning will fail until a pinned worker image tag or digest is configured")
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
	gw.SetAuthHandler(authHandler)
	instanceMgr.SetWorkspaceProviderConfigResolver(func(workspaceID string, providerType providers.ProviderType) (*providers.ProviderConfig, error) {
		apiKey, apiSecret, endpoint, err := authStore.ResolveWorkspaceProviderConfig(workspaceID, string(providerType))
		if err != nil {
			return nil, err
		}
		return &providers.ProviderConfig{
			Type:      providerType,
			APIKey:    apiKey,
			APISecret: apiSecret,
			Endpoint:  endpoint,
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
