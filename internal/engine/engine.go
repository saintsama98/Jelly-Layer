package engine

import (
	"log/slog"
	"os"

	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/config"
)

// Run is the CRE entry-point.  It is called by the CRE Runner with the
// deserialized config, a logger, and a secrets provider.
//
// Usage from main.go:
//
//	runner.Run(engine.Run)
func Run(
	cfg *config.Config,
	logger *slog.Logger,
	secrets cre.SecretsProvider,
) (cre.Workflow[*config.Config], error) {
	return initWorkflow(cfg, logger)
}

// RunLocal is a convenience wrapper for local development / testing.
// It constructs a logger and calls Run directly (no CRE runner needed).
func RunLocal() (cre.Workflow[*config.Config], error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return Run(cfg, logger, nil)
}
