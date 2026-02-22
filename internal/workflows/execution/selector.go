package execution

import (
	"context"

	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// LiquidatorSelector selects the best liquidator for a position.
type LiquidatorSelector struct {
	strategy string // "STAKER_POOL", "AUCTION", "ROUND_ROBIN"
}

// NewLiquidatorSelector creates a new selector.
func NewLiquidatorSelector(strategy string) *LiquidatorSelector {
	return &LiquidatorSelector{
		strategy: strategy,
	}
}

// SelectExecutor selects an executor based on the configured strategy.
func (ls *LiquidatorSelector) SelectExecutor(
	client *evmwrap.Client,
	position *contracts.ScoredPosition,
	cfg *config.Config,
) (*contracts.Executor, error) {
	switch ls.strategy {
	case "STAKER_POOL":
		return ls.selectFromStakerPool(client, position, cfg)
	case "AUCTION":
		return ls.selectFromAuction(client, position, cfg)
	case "ROUND_ROBIN":
		return ls.selectRoundRobin(client, position, cfg)
	default:
		return ls.selectFromStakerPool(client, position, cfg)
	}
}

// placeholderExecutorAddress is used when ExecutorRegistry is not yet wired.
const placeholderExecutorAddress = "0x0000000000000000000000000000000000000001"

// selectFromStakerPool selects the executor with highest stake × success rate from ExecutorRegistry.
func (ls *LiquidatorSelector) selectFromStakerPool(client *evmwrap.Client, position *contracts.ScoredPosition, cfg *config.Config) (*contracts.Executor, error) {
	ctx := context.Background()
	executors, err := FetchActiveExecutors(ctx, client, cfg)
	if err != nil || len(executors) == 0 {
		return &contracts.Executor{Address: placeholderExecutorAddress, IsActive: true}, nil
	}
	best := SelectBestExecutorByStakeAndSuccess(executors)
	if best != nil {
		return best, nil
	}
	return &contracts.Executor{Address: placeholderExecutorAddress, IsActive: true}, nil
}

// selectFromAuction selects the executor who bid the highest (not implemented; uses staker pool fallback).
func (ls *LiquidatorSelector) selectFromAuction(client *evmwrap.Client, position *contracts.ScoredPosition, cfg *config.Config) (*contracts.Executor, error) {
	return ls.selectFromStakerPool(client, position, cfg)
}

// selectRoundRobin selects executors in rotation (not implemented; uses staker pool fallback).
func (ls *LiquidatorSelector) selectRoundRobin(client *evmwrap.Client, position *contracts.ScoredPosition, cfg *config.Config) (*contracts.Executor, error) {
	return ls.selectFromStakerPool(client, position, cfg)
}
