// Infera Gateway - HTTP API server
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/infera/infera/go/internal/gateway"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/providers/mock"
	_ "github.com/infera/infera/go/internal/providers/runpod"
	_ "github.com/infera/infera/go/internal/providers/vastai"
	"github.com/infera/infera/go/internal/router"
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
	gw := gateway.New(gatewayConfig, r, instanceMgr)

	// Handle shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")

		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		defer shutdownCancel()

		if err := gw.Stop(shutdownCtx); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}

		r.Stop()
		cancel()
	}()

	// Start gateway
	log.Printf("Gateway listening on :%d", *httpPort)
	log.Printf("Registered providers: %v", instanceMgr.ListProviders())
	if err := gw.Start(); err != nil {
		log.Fatalf("Gateway error: %v", err)
	}
}
