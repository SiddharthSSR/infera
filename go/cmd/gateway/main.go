// Infera Gateway - HTTP API server
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/gateway"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/providers/mock"
	_ "github.com/infera/infera/go/internal/providers/runpod"
	// vastai is stubbed — not registered until implemented
	// _ "github.com/infera/infera/go/internal/providers/vastai"
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
	vastaiKey := flag.String("vastai-key", os.Getenv("VASTAI_API_KEY"), "Vast.ai API key")
	flag.Parse()

	log.Info("Starting Infera Gateway...")

	// Create router
	routerConfig := router.DefaultConfig()
	r := router.New(routerConfig)

	// Create instance manager
	// Get worker image from env or use default
	workerImage := os.Getenv("INFERA_WORKER_IMAGE")
	if workerImage == "" {
		workerImage = "infera/worker:latest"
	}

	instanceMgr := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
		WorkerImage:     workerImage,
		GatewayAddress:  "localhost:8080",
		CostDBPath:      "data/costs.db",
	})

	// Register mock provider (always available for testing)
	instanceMgr.RegisterProvider(mock.New())

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

	// Vast.ai provider is stubbed — registration disabled until implemented.
	// When ready, uncomment the import and this block.
	_ = vastaiKey

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
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Warn("failed to create data directory", slog.String("error", err.Error()))
	}
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

	gw.SetAuthHandler(auth.NewHandler(authStore))

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
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s\n", fullKey); err != nil {
		return err
	}
	return f.Sync()
}
