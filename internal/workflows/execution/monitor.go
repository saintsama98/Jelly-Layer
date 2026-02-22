package execution

import (
	"context"
	"time"

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

// waitForBlock waits until chain tip is >= targetBlock (polls GetCurrentBlock).
// Respects ctx cancellation. Polls every 1s; returns nil when current >= targetBlock.
func (em *ExecutionMonitor) waitForBlock(ctx context.Context, targetBlock int64) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		current, err := em.client.GetCurrentBlock(ctx)
		if err != nil {
			return err
		}
		if current >= targetBlock {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
			// poll again
		}
	}
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
	if plan == nil || plan.Window == nil {
		return true
	}
	ctx := context.Background()
	current, err := em.client.GetCurrentBlock(ctx)
	if err != nil {
		return false
	}
	return current <= plan.Window.EndBlock
}

// retryExecution retries a failed execution.
func (em *ExecutionMonitor) retryExecution(ctx context.Context, plan *contracts.ExecutionPlan) (*contracts.ExecutionResult, error) {
	// TODO: Implement — retry with same or different executor
	_ = ctx
	_ = plan
	return &contracts.ExecutionResult{}, nil
}
