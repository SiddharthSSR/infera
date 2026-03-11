package gateway

import (
	"log/slog"
	"os"
)

// NewLogger creates the gateway's structured logger.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}
