package distribution

import (
	"fmt"

	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm/bindings"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	orchestrator "github.com/jelly-layer-cre/jelly-engine/internal/contracts/generated/liquidation_orchestrator"
)

// DistributionResult is the output type for the distribution handler.
type DistributionResult struct {
	TotalOEV       uint64
	ProtocolShare  uint64
	ExecutorShare  uint64
	ValidatorShare uint64
}

// HandleDistribution is the callback for Handler 4: Liquidation Executed → OEV Distribution.
//
// Trigger: LiquidationExecuted event from the LiquidationOrchestrator contract.
func HandleDistribution(
	cfg *config.Config,
	runtime cre.Runtime,
	payload *bindings.DecodedLog[orchestrator.LiquidationExecutedDecoded],
) (*DistributionResult, error) {
	logger := runtime.Logger()

	logger.Info("Distribution triggered",
		"positionId", payload.Data.PositionId,
		"executor", payload.Data.Executor.Hex(),
		"capturedOEV", payload.Data.CapturedOEV,
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

	client := evmwrap.NewClientFromSDK(evmClient)

	// --- Parse the liquidation event data ---
	liquidationEvent := &contracts.LiquidationEvent{
		LiquidationID: payload.Data.PositionId.Uint64(),
		Executor:      payload.Data.Executor.Hex(),
		CapturedOEV:   payload.Data.CapturedOEV.Uint64(),
		Timestamp:     payload.Log.BlockNumber.Uint64(),
	}

	// --- Calculate OEV captured ---
	oevCaptured, err := calculateOEVCaptured(client, liquidationEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate OEV: %w", err)
	}

	// --- Calculate distribution splits ---
	calculator := NewDistributionCalculator(
		cfg.OEVProtocolSplit,
		cfg.OEVExecutorSplit,
		cfg.OEVValidatorSplit,
	)
	distribution := calculator.CalculateDistribution(oevCaptured)

	// --- Distribute OEV via OEVDistributor contract ---
	err = distributeOEV(client, cfg, liquidationEvent.LiquidationID, distribution)
	if err != nil {
		return nil, fmt.Errorf("failed to distribute OEV: %w", err)
	}

	// --- Record distribution on-chain ---
	err = recordDistribution(client, cfg, liquidationEvent.LiquidationID, distribution)
	if err != nil {
		return nil, fmt.Errorf("failed to record distribution: %w", err)
	}

	logger.Info("OEV distributed",
		"total", distribution.TotalOEV,
		"protocol", distribution.ProtocolShare,
		"executor", distribution.ExecutorShare,
		"validator", distribution.ValidatorShare,
	)

	return &DistributionResult{
		TotalOEV:       distribution.TotalOEV,
		ProtocolShare:  distribution.ProtocolShare,
		ExecutorShare:  distribution.ExecutorShare,
		ValidatorShare: distribution.ValidatorShare,
	}, nil
}

// calculateOEVCaptured calculates the actual OEV captured from the liquidation.
func calculateOEVCaptured(client *evmwrap.Client, event *contracts.LiquidationEvent) (uint64, error) {
	// TODO: Implement — compare pre/post liquidation state to compute actual value extracted
	_ = client
	return event.CapturedOEV, nil
}

// distributeOEV distributes OEV to recipients via the OEVDistributor contract.
func distributeOEV(client *evmwrap.Client, cfg *config.Config, liquidationID uint64, distribution *contracts.Distribution) error {
	// TODO: Implement — client.Write(ctx, cfg.OEVDistributorAddress, "distribute", ...)
	_ = client
	_ = cfg
	_ = liquidationID
	_ = distribution
	return nil
}

// recordDistribution records the distribution on-chain for transparency.
func recordDistribution(client *evmwrap.Client, cfg *config.Config, liquidationID uint64, distribution *contracts.Distribution) error {
	// TODO: Implement — client.Write(ctx, cfg.OEVDistributorAddress, "recordDistribution", ...)
	_ = client
	_ = cfg
	_ = liquidationID
	_ = distribution
	return nil
}
