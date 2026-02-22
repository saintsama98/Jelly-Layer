package execution

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/prioritization"
)

// QueueUpdatedEventSig is keccak256("QueueUpdated(uint256,uint256,uint256)").
var QueueUpdatedEventSig = crypto.Keccak256Hash([]byte("QueueUpdated(uint256,uint256,uint256)"))

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
	payload *evm.Log,
) (*ExecutionResult, error) {
	logger := runtime.Logger()

	queueId, positionCount, deferredCount := decodeQueueUpdated(payload)
	logger.Info("Execution triggered",
		"queueId", queueId,
		"positionCount", positionCount,
		"deferredCount", deferredCount,
		"block", payload.BlockNumber,
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

	// Wrap for convenience methods (selector, coordinator, etc.)
	client := evmwrap.NewClientFromSDK(evmClient)

	// Read prioritized queue from contract (same order as after Handler 2)
	queue, err := prioritization.ReadPrioritizedQueueAsScored(&evmClient, runtime, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read queue: %w", err)
	}

	executed := 0
	failed := 0

	// Process each position in queue
	selector := NewLiquidatorSelector("STAKER_POOL")
	coordinator := NewTimingCoordinator(cfg.ExecutionDelayBlocks, cfg.ExecutionWindowSize)

	monitor := NewExecutionMonitor(client)
	ctx := context.Background()

	for _, position := range queue {
		// Select liquidator from ExecutorRegistry (or placeholder if registry empty/unset)
		executor, err := selector.SelectExecutor(client, position, cfg)
		if err != nil {
			logger.Info("Failed to select executor", "positionId", position.Position.PositionID, "error", err)
			failed++
			continue
		}

		// Reserve execution window (delay + window); plan gets StartBlock/EndBlock
		executionPlan, err := coordinateTiming(client, coordinator, position, executor)
		if err != nil {
			failed++
			continue
		}

		// Wait until execution window opens (delay blocks)
		if executionPlan.Window != nil && executionPlan.Window.StartBlock > 0 {
			waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err = monitor.waitForBlock(waitCtx, executionPlan.Window.StartBlock)
			cancel()
			if err != nil {
				logger.Info("Wait for execution window failed", "positionId", position.Position.PositionID, "error", err)
				failed++
				continue
			}
		}

		// Pre-execution validation (plan, executor active, still within window)
		valid, err := validateExecution(client, monitor, executionPlan)
		if !valid || err != nil {
			failed++
			continue
		}

		// Execute liquidation via LiquidationOrchestrator
		err = executeLiquidation(ctx, client, cfg, executionPlan)
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

// decodeQueueUpdated decodes QueueUpdated(uint256 indexed queueId, uint256 positionCount, uint256 deferredCount).
func decodeQueueUpdated(log *evm.Log) (queueId, positionCount, deferredCount *big.Int) {
	queueId = new(big.Int)
	if len(log.Topics) >= 2 {
		queueId.SetBytes(log.Topics[1])
	}
	positionCount = new(big.Int)
	deferredCount = new(big.Int)
	if len(log.Data) >= 64 {
		positionCount.SetBytes(log.Data[:32])
		deferredCount.SetBytes(log.Data[32:64])
	}
	return queueId, positionCount, deferredCount
}

// coordinateTiming reserves an execution window for the position (delay + window)
// and attaches it to the execution plan. ReserveWindow is called on-chain when implemented.
func coordinateTiming(
	client *evmwrap.Client,
	coordinator *TimingCoordinator,
	position *contracts.ScoredPosition,
	executor *contracts.Executor,
) (*contracts.ExecutionPlan, error) {
	ctx := context.Background()
	window, err := coordinator.ReserveExecutionWindow(ctx, client, position, executor)
	if err != nil {
		return nil, err
	}
	return &contracts.ExecutionPlan{
		Position: position,
		Executor: executor,
		Window:   window,
	}, nil
}

// validateExecution validates the execution plan before submitting: plan and executor exist, executor active, still within execution window.
func validateExecution(client *evmwrap.Client, monitor *ExecutionMonitor, plan *contracts.ExecutionPlan) (bool, error) {
	if plan == nil || plan.Position == nil || plan.Executor == nil {
		return false, nil
	}
	if !plan.Executor.IsActive {
		return false, nil
	}
	if plan.Window != nil {
		if !monitor.isWithinDeadline(plan) {
			return false, nil
		}
	}
	return true, nil
}

// executeLiquidation submits the liquidation transaction via LiquidationOrchestrator.executeLiquidation(LiquidationParams).
// Contract expects a single struct (positionId, executor, debtAmount, collateralAmount).
func executeLiquidation(ctx context.Context, client *evmwrap.Client, cfg *config.Config, plan *contracts.ExecutionPlan) error {
	if cfg == nil || cfg.LiquidationOrchestratorAddress == "" {
		return nil // no orchestrator configured; skip (POC)
	}
	if plan == nil || plan.Position == nil || plan.Executor == nil {
		return nil
	}
	pos := plan.Position.Position // PositionWithOEV embeds Position
	params := struct {
		PositionId       *big.Int
		Executor         common.Address
		DebtAmount       *big.Int
		CollateralAmount *big.Int
	}{
		PositionId:       new(big.Int).SetUint64(positionIDToUint64(pos.PositionID)),
		Executor:         common.HexToAddress(plan.Executor.Address),
		DebtAmount:       new(big.Int).SetUint64(pos.DebtValue),
		CollateralAmount: new(big.Int).SetUint64(pos.CollateralValue),
	}
	_, err := client.Write(ctx, cfg.LiquidationOrchestratorAddress, "executeLiquidation", params)
	return err
}
