package execution

import (
	"context"

	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// ExecutionMonitor monitors execution status and handles retries.
type ExecutionMonitor struct {
	client *evmwrap.Client
}

// NewExecutionMonitor creates a new monitor.
func NewExecutionMonitor(client *evmwrap.Client) *ExecutionMonitor {
	return &ExecutionMonitor{
		client: client,
	}
}

// MonitorExecution monitors execution and handles retries.
func (em *ExecutionMonitor) MonitorExecution(
	ctx context.Context,
	plan *contracts.ExecutionPlan,
) (*contracts.ExecutionResult, error) {
	// Wait for target block
	err := em.waitForBlock(ctx, plan.Window.StartBlock)
	if err != nil {
		return nil, err
	}

	// Check execution status
	result, err := em.checkExecutionStatus(ctx, plan)
	if err != nil {
		return nil, err
	}

	// Handle retry if needed
	if !result.Success && em.isWithinDeadline(plan) {
		return em.retryExecution(ctx, plan)
	}

	return result, nil
}

// waitForBlock waits until target block is reached.
func (em *ExecutionMonitor) waitForBlock(ctx context.Context, targetBlock int64) error {
	// TODO: Implement — poll client.GetCurrentBlock until >= targetBlock
	_ = ctx
	_ = targetBlock
	return nil
}

// checkExecutionStatus checks if execution succeeded.
func (em *ExecutionMonitor) checkExecutionStatus(ctx context.Context, plan *contracts.ExecutionPlan) (*contracts.ExecutionResult, error) {
	// TODO: Implement — check on-chain execution status
	_ = ctx
	_ = plan
	return &contracts.ExecutionResult{}, nil
}

// isWithinDeadline checks if still within execution window.
func (em *ExecutionMonitor) isWithinDeadline(plan *contracts.ExecutionPlan) bool {
	// TODO: Implement — compare current block with plan.Window.EndBlock
	_ = plan
	return true
}

// retryExecution retries a failed execution.
func (em *ExecutionMonitor) retryExecution(ctx context.Context, plan *contracts.ExecutionPlan) (*contracts.ExecutionResult, error) {
	// TODO: Implement — retry with same or different executor
	_ = ctx
	_ = plan
	return &contracts.ExecutionResult{}, nil
}
