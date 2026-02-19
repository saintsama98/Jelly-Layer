package execution

import (
	"fmt"

	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm/bindings"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	pqueue "github.com/jelly-layer-cre/jelly-engine/internal/contracts/generated/priority_queue"
)

// ExecutionResult is the output type for the execution handler.
type ExecutionResult struct {
	Executed int
	Failed   int
}

// HandleExecution is the callback for Handler 3: Queue Updated → Execution Routing.
//
// Trigger: QueueUpdated event from the PriorityQueue contract.
func HandleExecution(
	cfg *config.Config,
	runtime cre.Runtime,
	payload *bindings.DecodedLog[pqueue.QueueUpdatedDecoded],
) (*ExecutionResult, error) {
	logger := runtime.Logger()

	logger.Info("Execution triggered",
		"queueSize", payload.Data.NewSize,
		"block", payload.Log.BlockNumber,
	)

	// --- Get EVM client ---
	evmClientRaw, err := runtime.EVMClient(cfg.ChainSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to get EVM client: %w", err)
	}
	evmClient, ok := evmClientRaw.(evm.Client)
	if !ok {
		return nil, fmt.Errorf("invalid EVM client type")
	}

	// Wrap for convenience methods
	client := evmwrap.NewClientFromSDK(evmClient)

	// Read prioritized queue
	queue, err := readPrioritizedQueue(client, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read queue: %w", err)
	}

	executed := 0
	failed := 0

	// Process each position in queue
	selector := NewLiquidatorSelector("STAKER_POOL")
	coordinator := NewTimingCoordinator(cfg.ExecutionDelayBlocks, cfg.ExecutionWindowSize)

	for _, position := range queue {
		// Select liquidator
		executor, err := selector.SelectExecutor(client, position)
		if err != nil {
			logger.Info("Failed to select executor", "positionId", position.Position.PositionID, "error", err)
			failed++
			continue
		}

		// Coordinate execution timing
		executionPlan, err := coordinateTiming(client, coordinator, position, executor)
		if err != nil {
			failed++
			continue
		}

		// Pre-execution validation
		valid, err := validateExecution(client, executionPlan)
		if !valid || err != nil {
			failed++
			continue
		}

		// Execute liquidation
		err = executeLiquidation(client, cfg, executionPlan)
		if err != nil {
			failed++
			continue
		}

		executed++
	}

	logger.Info("Execution complete", "executed", executed, "failed", failed)

	return &ExecutionResult{
		Executed: executed,
		Failed:   failed,
	}, nil
}

// readPrioritizedQueue reads the prioritized queue from the PriorityQueue contract.
func readPrioritizedQueue(client *evmwrap.Client, cfg *config.Config) ([]*contracts.ScoredPosition, error) {
	// TODO: Implement — client.Read(ctx, cfg.PriorityQueueAddress, "getQueue")
	_ = client
	_ = cfg
	return []*contracts.ScoredPosition{}, nil
}

// coordinateTiming reserves an execution window for the position.
func coordinateTiming(
	client *evmwrap.Client,
	coordinator *TimingCoordinator,
	position *contracts.ScoredPosition,
	executor *contracts.Executor,
) (*contracts.ExecutionPlan, error) {
	// TODO: Implement — use coordinator to reserve window
	_ = client
	_ = coordinator
	return &contracts.ExecutionPlan{
		Position: position,
		Executor: executor,
	}, nil
}

// validateExecution validates the execution plan before submitting.
func validateExecution(client *evmwrap.Client, plan *contracts.ExecutionPlan) (bool, error) {
	// TODO: Implement — recheck health factor, confirm executor eligibility
	_ = client
	_ = plan
	return true, nil
}

// executeLiquidation submits the liquidation transaction via the LiquidationOrchestrator.
func executeLiquidation(client *evmwrap.Client, cfg *config.Config, plan *contracts.ExecutionPlan) error {
	// TODO: Implement — client.Write(ctx, cfg.LiquidationOrchestratorAddress, "executeLiquidation", ...)
	_ = client
	_ = cfg
	_ = plan
	return nil
}
