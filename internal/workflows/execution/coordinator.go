package execution

import (
	"context"

	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// TimingCoordinator coordinates execution timing to avoid conflicts.
type TimingCoordinator struct {
	delayBlocks int64
	windowSize  int64
}

// NewTimingCoordinator creates a new coordinator.
func NewTimingCoordinator(delayBlocks, windowSize int64) *TimingCoordinator {
	return &TimingCoordinator{
		delayBlocks: delayBlocks,
		windowSize:  windowSize,
	}
}

// ReserveExecutionWindow computes the execution window for a position (delay + window size).
// Timing is enforced by the DON: the workflow waits until StartBlock then submits the liquidation.
// No on-chain reservation is used; the contract trusts the DON as jellyEngine.
func (tc *TimingCoordinator) ReserveExecutionWindow(
	ctx context.Context,
	client *evmwrap.Client,
	position *contracts.ScoredPosition,
	executor *contracts.Executor,
) (*contracts.ExecutionWindow, error) {
	currentBlock, err := client.GetCurrentBlock(ctx)
	if err != nil {
		return nil, err
	}
	window := &contracts.ExecutionWindow{
		PositionID: position.Position.PositionID,
		Executor:   executor.Address,
		StartBlock: currentBlock + tc.delayBlocks,
		EndBlock:   currentBlock + tc.delayBlocks + tc.windowSize,
	}
	return window, nil
}
