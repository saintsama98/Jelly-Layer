package execution

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
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

var orchestratorABI abi.ABI

func init() {
	var err error
	orchestratorABI, err = abi.JSON(strings.NewReader(`[{
		"name": "executeLiquidation",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{
			"name": "params",
			"type": "tuple",
			"components": [
				{"name": "positionId", "type": "uint256"},
				{"name": "executor", "type": "address"},
				{"name": "debtAmount", "type": "uint256"},
				{"name": "collateralAmount", "type": "uint256"},
				{"name": "borrower", "type": "address"},
				{"name": "oevPotential", "type": "uint256"},
				{"name": "estimatedGasCost", "type": "uint256"}
			]
		}],
		"outputs": []
	}]`))
	if err != nil {
		panic("execution: orchestrator ABI: " + err.Error())
	}
}

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
	logger.Info("[Execution] Triggered",
		"queueId", queueId,
		"positionCount", positionCount,
		"deferredCount", deferredCount,
		"block", payload.BlockNumber,
	)

	// --- EVM client for this chain (same pattern as detection/prioritization) ---
	evmClient := &evm.Client{ChainSelector: cfg.ChainSelector}
	client := evmwrap.NewClientFromSDK(evmClient, runtime)

	// Read prioritized queue from contract (same order as after Handler 2)
	logger.Info("[Execution] Reading queue (getPrioritizedQueue)")
	queue, err := prioritization.ReadPrioritizedQueueAsScored(evmClient, runtime, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read queue: %w", err)
	}
	if len(queue) == 0 && len(payload.TxHash) >= 32 {
		// Fallback: build queue from updateQueue tx (prioritizedIds + getPosition per id)
		if fromTx, _ := prioritization.QueueFromUpdateQueueTx(evmClient, runtime, cfg, payload.TxHash); len(fromTx) > 0 {
			queue = fromTx
			logger.Info("[Execution] Using queue from updateQueue tx", "positions", len(queue))
		}
	}
	logger.Info("[Execution] Queue size", "positions", len(queue))

	executed := 0
	failed := 0

	// Process each position in queue
	strategy := cfg.ExecutionStrategy
	if strategy == "" {
		// Default to staker pool if not explicitly configured.
		strategy = "STAKER_POOL"
	}
	selector := NewLiquidatorSelector(strategy)
	coordinator := NewTimingCoordinator(cfg.ExecutionDelayBlocks, cfg.ExecutionWindowSize)

	monitor := NewExecutionMonitor(client)
	ctx := context.Background()

	for _, position := range queue {
		pid := position.Position.PositionID
		logger.Info("[Execution] Processing position", "positionId", pid)

		// Select liquidator from ExecutorRegistry (or placeholder if registry empty/unset)
		executor, err := selector.SelectExecutor(client, position, cfg)
		if err != nil {
			logger.Info("[Execution] Select executor failed", "positionId", pid, "error", err)
			failed++
			continue
		}
		logger.Info("[Execution] Executor selected", "positionId", pid, "executor", executor.Address)

		// Reserve execution window (delay + window); plan gets StartBlock/EndBlock
		executionPlan, err := coordinateTiming(client, coordinator, position, executor)
		if err != nil {
			logger.Info("[Execution] coordinateTiming failed", "positionId", pid, "error", err)
			failed++
			continue
		}

		// Wait until execution window opens (delay blocks)
		if executionPlan.Window != nil && executionPlan.Window.StartBlock > 0 {
			logger.Info("[Execution] Execution window (realtime)", "positionId", pid, "startBlock", executionPlan.Window.StartBlock, "endBlock", executionPlan.Window.EndBlock)
			logger.Info("[Execution] Waiting for execution window", "positionId", pid, "startBlock", executionPlan.Window.StartBlock)
			waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err = monitor.waitForBlock(waitCtx, executionPlan.Window.StartBlock)
			cancel()
			if err != nil {
				logger.Info("[Execution] Wait for window failed", "positionId", pid, "error", err)
				failed++
				continue
			}
		}

		// Pre-execution validation (plan, executor active, still within window)
		valid, err := validateExecution(client, monitor, executionPlan)
		if !valid || err != nil {
			logger.Info("[Execution] Validation failed", "positionId", pid, "valid", valid, "error", err)
			failed++
			continue
		}

		// Execute liquidation via LiquidationOrchestrator
		logger.Info("[Execution] Calling LiquidationOrchestrator.executeLiquidation", "positionId", pid)
		liqTxHash, err := executeLiquidation(ctx, client, cfg, executionPlan)
		if err != nil {
			logger.Info("[Execution] executeLiquidation failed", "positionId", pid, "error", err)
			// Mark auction failure, if applicable.
			_ = settleAuction(ctx, client, cfg, executionPlan, false)
			// Record a failed attempt for the selected executor.
			if executionPlan.Executor != nil {
				_ = UpdateExecutorStats(ctx, client, cfg, executionPlan.Executor.Address, 0, 1, 0)
			}
			failed++
			continue
		}

		// Mark auction success, if applicable.
		_ = settleAuction(ctx, client, cfg, executionPlan, true)
		executed++
		logger.Info("[Execution] Liquidation succeeded", "positionId", pid)
		if liqTxHash != "" {
			logger.Info("[Execution] Next: run Distribution with --evm-tx-hash "+liqTxHash, "evm-tx-hash", liqTxHash)
		}
	}

	logger.Info("[Execution] Done", "executed", executed, "failed", failed)

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
// Contract expects a single struct matching LiquidationOrchestrator.LiquidationParams:
// (positionId, executor, debtAmount, collateralAmount, borrower, oevPotential, estimatedGasCost).
// Returns the transaction hash (same tx emits LiquidationExecuted for Distribution trigger).
func executeLiquidation(ctx context.Context, client *evmwrap.Client, cfg *config.Config, plan *contracts.ExecutionPlan) (txHash string, err error) {
	if cfg == nil || cfg.LiquidationOrchestratorAddress == "" {
		return "", nil // no orchestrator configured; skip (POC)
	}
	if plan == nil || plan.Position == nil || plan.Executor == nil {
		return "", nil
	}
	pos := plan.Position.Position // PositionWithOEV embeds Position

	// Build params in the exact field order of the Solidity struct (abi tags must match ABI component names).
	params := struct {
		PositionId       *big.Int       `abi:"positionId"`
		Executor         common.Address `abi:"executor"`
		DebtAmount       *big.Int       `abi:"debtAmount"`
		CollateralAmount *big.Int       `abi:"collateralAmount"`
		Borrower         common.Address `abi:"borrower"`
		OEVPotential     *big.Int       `abi:"oevPotential"`
		EstimatedGasCost *big.Int       `abi:"estimatedGasCost"`
	}{
		PositionId:       new(big.Int).SetUint64(positionIDToUint64(pos.PositionID)),
		Executor:         common.HexToAddress(plan.Executor.Address),
		DebtAmount:       new(big.Int).SetUint64(pos.DebtValue),
		CollateralAmount: new(big.Int).SetUint64(pos.CollateralValue),
		Borrower:         common.HexToAddress(pos.UserAddress),
		OEVPotential:     new(big.Int).SetUint64(pos.OEVPotential),
		EstimatedGasCost: new(big.Int).SetUint64(pos.EstimatedGasCost),
	}
	callData, err := orchestratorABI.Pack("executeLiquidation", params)
	if err != nil {
		return "", err
	}
	txHash, err = client.Write(ctx, cfg.LiquidationOrchestratorAddress, callData)
	if err != nil {
		return "", err
	}
	return txHash, nil
}

// settleAuction notifies the LiquidationAuctionHouse of execution outcome for the
// given plan's position/executor, when auction-based selection is enabled.
func settleAuction(
	ctx context.Context,
	client *evmwrap.Client,
	cfg *config.Config,
	plan *contracts.ExecutionPlan,
	success bool,
) error {
	if cfg == nil || cfg.LiquidationAuctionHouseAddress == "" {
		return nil
	}
	if plan == nil || plan.Position == nil || plan.Position.Position == nil {
		return nil
	}
	positionNumeric := positionIDToUint64(plan.Position.Position.PositionID)
	if positionNumeric == 0 {
		return nil
	}

	method := "settleFailure"
	if success {
		method = "settleSuccess"
	}
	positionIDBig := new(big.Int).SetUint64(positionNumeric)
	callData, err := auctionHouseABI.Pack(method, positionIDBig)
	if err != nil {
		return err
	}
	_, err = client.Write(ctx, cfg.LiquidationAuctionHouseAddress, callData)
	return err
}
