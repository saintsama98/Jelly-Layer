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

// ReserveExecutionWindow reserves an execution window for a position.
func (tc *TimingCoordinator) ReserveExecutionWindow(
	ctx context.Context,
	client *evmwrap.Client,
	position *contracts.ScoredPosition,
	executor *contracts.Executor,
) (*contracts.ExecutionWindow, error) {
	// Get current block
	currentBlock, err := client.GetCurrentBlock(ctx)
	if err != nil {
		return nil, err
	}

	// Calculate window
	window := &contracts.ExecutionWindow{
		PositionID: position.Position.PositionID,
		Executor:   executor.Address,
		StartBlock: currentBlock + tc.delayBlocks,
		EndBlock:   currentBlock + tc.delayBlocks + tc.windowSize,
	}

	// Reserve on-chain
	err = client.ReserveWindow(ctx, window)
	if err != nil {
		return nil, err
	}

	return window, nil
}
