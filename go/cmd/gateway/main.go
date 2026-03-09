// Infera Gateway - HTTP API server
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	_ "github.com/infera/infera/go/internal/providers/vastai"
	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/internal/vault"
)

func main() {
	// Parse flags
	httpPort := flag.Int("port", 8080, "HTTP port")
	runpodKey := flag.String("runpod-key", os.Getenv("RUNPOD_API_KEY"), "RunPod API key")
	vastaiKey := flag.String("vastai-key", os.Getenv("VASTAI_API_KEY"), "Vast.ai API key")
	flag.Parse()

	log.Println("Starting Infera Gateway...")

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
			log.Printf("Warning: Failed to create RunPod provider: %v", err)
		} else {
			instanceMgr.RegisterProvider(runpodProvider)
			log.Println("RunPod provider registered")
		}
	}

	// Register Vast.ai if API key provided
	if *vastaiKey != "" {
		vastaiProvider, err := providers.CreateProvider(providers.ProviderConfig{
			Type:   providers.ProviderVastAI,
			APIKey: *vastaiKey,
		})
		if err != nil {
			log.Printf("Warning: Failed to create Vast.ai provider: %v", err)
		} else {
			instanceMgr.RegisterProvider(vastaiProvider)
			log.Println("Vast.ai provider registered")
		}
	}

	// Create gateway
	gatewayConfig := gateway.DefaultConfig()
	gatewayConfig.HTTPPort = *httpPort
	gatewayConfig.WorkerSharedToken = strings.TrimSpace(os.Getenv("INFERA_WORKER_SHARED_TOKEN"))
	if gatewayConfig.WorkerSharedToken == "" {
		log.Fatal("INFERA_WORKER_SHARED_TOKEN is required and cannot be empty")
	}
	if allowedOrigins := parseAllowedOrigins(os.Getenv("INFERA_ALLOWED_ORIGINS")); len(allowedOrigins) > 0 {
		gatewayConfig.AllowedOrigins = allowedOrigins
	}
	gw := gateway.New(gatewayConfig, r, instanceMgr)

	// Initialize vault (model registry)
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Printf("Warning: Failed to create data directory: %v", err)
	}
	vaultStore, err := vault.NewStore("data/vault.db")
	if err != nil {
		log.Fatalf("Failed to initialize vault: %v", err)
	}
	defer vaultStore.Close()

	if err := vault.SeedDefaultModels(vaultStore); err != nil {
		log.Printf("Warning: Failed to seed vault: %v", err)
	}

	gw.SetVaultHandler(vault.NewHandler(vaultStore))

	// Initialize auth (API key authentication)
	authStore, err := auth.NewStore("data/auth.db")
	if err != nil {
		log.Fatalf("Failed to initialize auth store: %v", err)
	}
	defer authStore.Close()

	// Bootstrap admin key from env or auto-generate on first run
	keyCount, _ := authStore.Count()
	if keyCount == 0 {
		adminKey := os.Getenv("INFERA_ADMIN_KEY")
		if adminKey != "" {
			// Use provided admin key
			if _, err := authStore.CreateKeyFromRaw(adminKey, "Bootstrap Admin", "admin"); err != nil {
				log.Fatalf("Failed to store bootstrap admin key from INFERA_ADMIN_KEY: %v", err)
			} else {
				log.Println("Admin key configured from INFERA_ADMIN_KEY")
			}
		} else {
			// Auto-generate admin key
			fullKey, record, err := authStore.CreateKey("Auto Admin", "admin")
			if err != nil {
				log.Fatalf("Failed to generate admin key: %v", err)
			} else {
				if err := persistBootstrapAdminKey("data/bootstrap_admin_key.txt", fullKey); err != nil {
					log.Fatalf("Failed to persist bootstrap admin key: %v", err)
				}
				log.Println("Auto-generated admin API key created.")
				log.Printf("Key prefix: %s", record.KeyPrefix)
				log.Println("Plaintext key stored at data/bootstrap_admin_key.txt with 0600 permissions.")
			}
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
					log.Printf("Warning: Failed to refresh instances: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		<-sigChan
		log.Println("Shutting down...")

		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
		defer shutdownCancel()

		if err := gw.Stop(shutdownCtx); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}

		r.Stop()
		cancel()
		log.Println("Shutdown complete")
	}()

	// Start gateway
	log.Printf("Gateway listening on :%d", *httpPort)
	log.Printf("Registered providers: %v", instanceMgr.ListProviders())
	if err := gw.Start(); err != nil {
		log.Fatalf("Gateway error: %v", err)
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
