package distribution

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/execution"
)

// LiquidationExecutedEventSig is keccak256("LiquidationExecuted(uint256,address,uint256,bytes32)").
var LiquidationExecutedEventSig = crypto.Keccak256Hash([]byte("LiquidationExecuted(uint256,address,uint256,bytes32)"))

// DistributionResult is the output type for the distribution handler.
type DistributionResult struct {
	TotalOEV       uint64
	ProtocolShare  uint64
	ExecutorShare  uint64
	ValidatorShare uint64
}

// decodedLiquidationExecuted holds decoded event fields for Handler 4.
type decodedLiquidationExecuted struct {
	PositionId  *big.Int
	Executor    common.Address
	CapturedOEV *big.Int
	TxHash      [32]byte
}

// blockNumberToUint64 converts evm.Log's BlockNumber (type may be *big.Int or *pb.BigInt) to uint64.
func blockNumberToUint64(blockNum interface{}) uint64 {
	if blockNum == nil {
		return 0
	}
	if bi, ok := blockNum.(*big.Int); ok && bi != nil {
		return bi.Uint64()
	}
	// CRE SDK may use *pb.BigInt; try to get bytes and convert
	type withBytes interface{ GetBytes() []byte }
	if b, ok := blockNum.(withBytes); ok {
		return new(big.Int).SetBytes(b.GetBytes()).Uint64()
	}
	return 0
}

// decodeLiquidationExecuted decodes LiquidationExecuted(uint256 indexed positionId, address indexed executor, uint256 capturedOEV, bytes32 txHash).
func decodeLiquidationExecuted(log *evm.Log) decodedLiquidationExecuted {
	var d decodedLiquidationExecuted
	d.PositionId = new(big.Int)
	if len(log.Topics) >= 2 {
		d.PositionId.SetBytes(log.Topics[1])
	}
	if len(log.Topics) >= 3 {
		d.Executor = common.BytesToAddress(log.Topics[2])
	}
	if len(log.Data) >= 64 {
		d.CapturedOEV = new(big.Int).SetBytes(log.Data[:32])
		copy(d.TxHash[:], log.Data[32:64])
	}
	return d
}

// HandleDistribution is the callback for Handler 4: Liquidation Executed → OEV Distribution.
//
// Trigger: LiquidationExecuted event from the LiquidationOrchestrator contract.
func HandleDistribution(
	cfg *config.Config,
	runtime cre.Runtime,
	payload *evm.Log,
) (*DistributionResult, error) {
	logger := runtime.Logger()

	data := decodeLiquidationExecuted(payload)
	logger.Info("Distribution triggered",
		"positionId", data.PositionId,
		"executor", data.Executor.Hex(),
		"capturedOEV", data.CapturedOEV,
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

	client := evmwrap.NewClientFromSDK(evmClient)

	// --- Parse the liquidation event data ---
	blockTs := blockNumberToUint64(payload.BlockNumber)
	liquidationEvent := &contracts.LiquidationEvent{
		LiquidationID: data.PositionId.Uint64(),
		Executor:      data.Executor.Hex(),
		CapturedOEV:   data.CapturedOEV.Uint64(),
		Timestamp:     blockTs,
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

	// --- Update executor reputation and stats in ExecutorRegistry ---
	// One successful liquidation with the computed OEV captured.
	ctx := context.Background()
	_ = execution.UpdateExecutorStats(ctx, client, cfg, liquidationEvent.Executor, 1, 0, oevCaptured)

	return &DistributionResult{
		TotalOEV:       distribution.TotalOEV,
		ProtocolShare:  distribution.ProtocolShare,
		ExecutorShare:  distribution.ExecutorShare,
		ValidatorShare: distribution.ValidatorShare,
	}, nil
}

// calculateOEVCaptured calculates the actual OEV captured from the liquidation.
func calculateOEVCaptured(client *evmwrap.Client, event *contracts.LiquidationEvent) (uint64, error) {
	// Prefer auction-based OEV if an auction house is configured. We treat the
	// total bid amount for the position as the OEV captured via the auction.
	if client != nil {
		// We don't have direct access to config here, so callers that want
		// auction-based OEV should pass it through via a higher-level helper.
		// For now, fall back to the value emitted in the event.
	}
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
