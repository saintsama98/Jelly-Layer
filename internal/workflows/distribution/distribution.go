package distribution

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
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

var (
	auctionHouseABI   abi.ABI
	oevDistributorABI abi.ABI
)

func init() {
	var err error
	auctionHouseABI, err = abi.JSON(strings.NewReader(`[{"name": "auctions", "type": "function", "stateMutability": "view", "inputs": [{"name": "positionId", "type": "uint256"}], "outputs": [{"name": "positionId", "type": "uint256"}, {"name": "executor", "type": "address"}, {"name": "totalBid", "type": "uint256"}, {"name": "upfrontAmount", "type": "uint256"}, {"name": "startBlock", "type": "uint64"}, {"name": "deadlineBlock", "type": "uint64"}, {"name": "settled", "type": "bool"}, {"name": "success", "type": "bool"}]}]`))
	if err != nil {
		panic("distribution: auction house ABI: " + err.Error())
	}
	oevDistributorABI, err = abi.JSON(strings.NewReader(`[
		{"name": "captureOEV", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "liquidationId", "type": "uint256"}, {"name": "totalOEV", "type": "uint256"}], "outputs": []},
		{"name": "distribute", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "liquidationId", "type": "uint256"}], "outputs": []}
	]`))
	if err != nil {
		panic("distribution: OEVDistributor ABI: " + err.Error())
	}
}

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
	logger.Info("[Distribution] Triggered",
		"positionId", data.PositionId,
		"executor", data.Executor.Hex(),
		"capturedOEV", data.CapturedOEV,
		"block", payload.BlockNumber,
	)

	// --- EVM client for this chain (same pattern as detection/prioritization) ---
	evmClient := &evm.Client{ChainSelector: cfg.ChainSelector}
	client := evmwrap.NewClientFromSDK(evmClient, runtime)

	// --- Parse the liquidation event data ---
	blockTs := blockNumberToUint64(payload.BlockNumber)
	var positionID, capturedOEV uint64
	if data.PositionId != nil {
		positionID = data.PositionId.Uint64()
	}
	if data.CapturedOEV != nil {
		capturedOEV = data.CapturedOEV.Uint64()
	}
	liquidationEvent := &contracts.LiquidationEvent{
		LiquidationID: positionID,
		Executor:      data.Executor.Hex(),
		CapturedOEV:   capturedOEV,
		Timestamp:     blockTs,
	}

	// --- Calculate OEV captured ---
	logger.Info("[Distribution] Calculating OEV captured (realtime: event or auction)")
	oevCaptured, err := calculateOEVCaptured(client, cfg, liquidationEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate OEV: %w", err)
	}
	oevSource := "event"
	if cfg != nil && cfg.LiquidationAuctionHouseAddress != "" && oevCaptured != liquidationEvent.CapturedOEV {
		oevSource = "auction"
	}
	logger.Info("[Distribution] OEV captured (realtime)",
		"fromEvent", liquidationEvent.CapturedOEV,
		"finalUsed", oevCaptured,
		"source", oevSource,
	)

	// --- Calculate distribution splits ---
	calculator := NewDistributionCalculator(
		cfg.OEVProtocolSplit,
		cfg.OEVExecutorSplit,
		cfg.OEVValidatorSplit,
	)
	distribution := calculator.CalculateDistribution(oevCaptured)
	logger.Info("[Distribution] Splits", "protocol", distribution.ProtocolShare, "executor", distribution.ExecutorShare, "validator", distribution.ValidatorShare)

	// --- Distribute OEV via OEVDistributor contract ---
	if distribution.TotalOEV == 0 {
		logger.Info("[Distribution] Nothing to distribute (OEV=0), skipping contract")
	} else {
		logger.Info("[Distribution] Calling OEV distributor contract")
	}
	err = distributeOEV(client, cfg, liquidationEvent.LiquidationID, distribution)
	if err != nil {
		return nil, fmt.Errorf("failed to distribute OEV: %w", err)
	}

	// --- Record distribution on-chain ---
	logger.Info("[Distribution] Recording distribution on-chain")
	err = recordDistribution(client, cfg, liquidationEvent.LiquidationID, distribution)
	if err != nil {
		return nil, fmt.Errorf("failed to record distribution: %w", err)
	}

	logger.Info("[Distribution] Done. OEV distributed",
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
//
// Preference order:
//   1. If a LiquidationAuctionHouse is configured, use its recorded totalBid
//      for the position as the OEV captured via the auction.
//   2. Fall back to the value emitted in the LiquidationExecuted event.
func calculateOEVCaptured(client *evmwrap.Client, cfg *config.Config, event *contracts.LiquidationEvent) (uint64, error) {
	// Try auction-based OEV first if configured.
	if client != nil && cfg != nil && cfg.LiquidationAuctionHouseAddress != "" {
		ctx := context.Background()
		// Solidity signature:
		//   function auctions(uint256 positionId) external view returns (
		//       uint256 positionId,
		//       address executor,
		//       uint256 totalBid,
		//       uint256 upfrontAmount,
		//       uint64 startBlock,
		//       uint64 deadlineBlock,
		//       bool settled,
		//       bool success
		//   );
		callData, err := auctionHouseABI.Pack("auctions", new(big.Int).SetUint64(event.LiquidationID))
		if err != nil {
			return event.CapturedOEV, nil
		}
		data, err := client.Read(ctx, cfg.LiquidationAuctionHouseAddress, callData)
		if err != nil {
			return event.CapturedOEV, nil
		}
		results, err := auctionHouseABI.Unpack("auctions", data)
		if err == nil && len(results) >= 3 {
			if totalBid, ok := results[2].(*big.Int); ok && totalBid != nil {
				return totalBid.Uint64(), nil
			}
		}
	}

	// Fallback: use the captured OEV value emitted in the event.
	return event.CapturedOEV, nil
}

// distributeOEV distributes OEV to recipients via the OEVDistributor contract.
// When TotalOEV is 0, the contract would revert on captureOEV; we skip the calls and succeed (nothing to distribute).
func distributeOEV(client *evmwrap.Client, cfg *config.Config, liquidationID uint64, distribution *contracts.Distribution) error {
	if client == nil || cfg == nil {
		return fmt.Errorf("missing client or config")
	}
	if cfg.OEVDistributorAddress == "" {
		return fmt.Errorf("OEVDistributor address not configured")
	}
	if distribution == nil || distribution.TotalOEV == 0 {
		// Nothing to distribute; contract would revert on captureOEV(..., 0).
		return nil
	}

	ctx := context.Background()

	// First, record the total OEV captured for this liquidation.
	callData, err := oevDistributorABI.Pack("captureOEV", new(big.Int).SetUint64(liquidationID), new(big.Int).SetUint64(distribution.TotalOEV))
	if err != nil {
		return fmt.Errorf("pack captureOEV: %w", err)
	}
	_, err = client.Write(ctx, cfg.OEVDistributorAddress, callData)
	if err != nil {
		return fmt.Errorf("captureOEV call failed: %w", err)
	}

	// Then, trigger distribution according to the on-chain split logic
	// (50% protocol / 50% executor / 0% validator in the current design).
	callData, err = oevDistributorABI.Pack("distribute", new(big.Int).SetUint64(liquidationID))
	if err != nil {
		return fmt.Errorf("pack distribute: %w", err)
	}
	_, err = client.Write(ctx, cfg.OEVDistributorAddress, callData)
	if err != nil {
		return fmt.Errorf("distribute call failed: %w", err)
	}

	return nil
}

// recordDistribution records the distribution on-chain for transparency.
func recordDistribution(client *evmwrap.Client, cfg *config.Config, liquidationID uint64, distribution *contracts.Distribution) error {
	// The OEVDistributor already persists the distribution in its state and
	// emits events via captureOEV/distribute, so there is nothing additional
	// to record here for now.
	_ = client
	_ = cfg
	_ = liquidationID
	_ = distribution
	return nil
}
