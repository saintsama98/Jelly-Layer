package execution

import (
	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
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
) (*contracts.Executor, error) {
	switch ls.strategy {
	case "STAKER_POOL":
		return ls.selectFromStakerPool(client, position)
	case "AUCTION":
		return ls.selectFromAuction(client, position)
	case "ROUND_ROBIN":
		return ls.selectRoundRobin(client, position)
	default:
		return ls.selectFromStakerPool(client, position)
	}
}

// selectFromStakerPool selects the executor with highest stake × success rate.
func (ls *LiquidatorSelector) selectFromStakerPool(client *evmwrap.Client, position *contracts.ScoredPosition) (*contracts.Executor, error) {
	// TODO: Implement — read from ExecutorRegistry, rank by stake × success rate
	_ = client
	_ = position
	return &contracts.Executor{}, nil
}

// selectFromAuction selects the executor who bid the highest.
func (ls *LiquidatorSelector) selectFromAuction(client *evmwrap.Client, position *contracts.ScoredPosition) (*contracts.Executor, error) {
	// TODO: Implement auction-based selection
	_ = client
	_ = position
	return &contracts.Executor{}, nil
}

// selectRoundRobin selects executors in rotation.
func (ls *LiquidatorSelector) selectRoundRobin(client *evmwrap.Client, position *contracts.ScoredPosition) (*contracts.Executor, error) {
	// TODO: Implement round-robin selection
	_ = client
	_ = position
	return &contracts.Executor{}, nil
}
