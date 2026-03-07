package execution

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

var (
	executorRegistryABI abi.ABI
	auctionHouseABI     abi.ABI
)

func init() {
	var err error
	executorRegistryABI, err = abi.JSON(strings.NewReader(`[
		{"name": "getActiveExecutors", "type": "function", "stateMutability": "view", "inputs": [], "outputs": [{"name": "", "type": "tuple[]", "components": [{"name": "executorAddress", "type": "address"}, {"name": "successfulLiquidations", "type": "uint256"}, {"name": "failedAttempts", "type": "uint256"}, {"name": "profitabilityScore", "type": "uint256"}, {"name": "supportedCollaterals", "type": "bytes32[]"}, {"name": "isActive", "type": "bool"}, {"name": "lastExecutionTime", "type": "uint64"}, {"name": "totalOEVCaptured", "type": "uint256"}, {"name": "stakeAmount", "type": "uint256"}]}]},
		{"name": "updateExecutorStats", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "executor", "type": "address"}, {"name": "successfulLiquidations", "type": "uint256"}, {"name": "failedAttempts", "type": "uint256"}, {"name": "totalOEVCaptured", "type": "uint256"}], "outputs": []}
	]`))
	if err != nil {
		panic(fmt.Sprintf("execution: ExecutorRegistry ABI: %v", err))
	}
	auctionHouseABI, err = abi.JSON(strings.NewReader(`[
		{"name": "startAuction", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "positionId", "type": "uint256"}, {"name": "deadlineBlock", "type": "uint64"}], "outputs": []},
		{"name": "getBestBid", "type": "function", "stateMutability": "view", "inputs": [{"name": "positionId", "type": "uint256"}], "outputs": [{"name": "", "type": "address"}, {"name": "", "type": "uint256"}]},
		{"name": "recordWinningBid", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "positionId", "type": "uint256"}, {"name": "executor", "type": "address"}, {"name": "totalBid", "type": "uint256"}, {"name": "upfrontAmount", "type": "uint256"}], "outputs": []},
		{"name": "settleSuccess", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "positionId", "type": "uint256"}], "outputs": []},
		{"name": "settleFailure", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "positionId", "type": "uint256"}], "outputs": []}
	]`))
	if err != nil {
		panic(fmt.Sprintf("execution: LiquidationAuctionHouse ABI: %v", err))
	}
}

// FetchActiveExecutors calls ExecutorRegistry.getActiveExecutors(). Returns nil on empty registry or error (caller uses placeholder).
func FetchActiveExecutors(ctx context.Context, client *evmwrap.Client, cfg *config.Config) ([]*contracts.Executor, error) {
	if cfg == nil || cfg.ExecutorRegistryAddress == "" {
		return nil, nil
	}
	callData, err := executorRegistryABI.Pack("getActiveExecutors")
	if err != nil {
		return nil, nil
	}
	data, err := client.Read(ctx, cfg.ExecutorRegistryAddress, callData)
	if err != nil || len(data) == 0 {
		return nil, nil
	}
	results, err := executorRegistryABI.Unpack("getActiveExecutors", data)
	if err != nil || len(results) == 0 {
		return nil, nil
	}
	rawSlice, ok := results[0].([]struct {
		ExecutorAddress        common.Address `abi:"executorAddress"`
		SuccessfulLiquidations *big.Int       `abi:"successfulLiquidations"`
		FailedAttempts         *big.Int       `abi:"failedAttempts"`
		ProfitabilityScore     *big.Int       `abi:"profitabilityScore"`
		IsActive               bool           `abi:"isActive"`
		StakeAmount            *big.Int       `abi:"stakeAmount"`
	})
	if !ok {
		return nil, nil
	}
	out := make([]*contracts.Executor, 0, len(rawSlice))
	for i := range rawSlice {
		e := &rawSlice[i]
		if !e.IsActive {
			continue
		}
		successRate := 0.0
		if e.SuccessfulLiquidations != nil && e.FailedAttempts != nil {
			total := new(big.Int).Add(e.SuccessfulLiquidations, e.FailedAttempts)
			if total.Sign() > 0 {
				successRate, _ = new(big.Float).Quo(
					new(big.Float).SetInt(e.SuccessfulLiquidations),
					new(big.Float).SetInt(total),
				).Float64()
			}
		}
		stake := uint64(0)
		if e.StakeAmount != nil {
			stake = e.StakeAmount.Uint64()
		}
		out = append(out, &contracts.Executor{
			Address:     e.ExecutorAddress.Hex(),
			StakeAmount: stake,
			SuccessRate: successRate,
			IsActive:    true,
		})
	}
	return out, nil
}

// SelectBestExecutorByStakeAndSuccess returns the executor with highest stake * successRate.
func SelectBestExecutorByStakeAndSuccess(executors []*contracts.Executor) *contracts.Executor {
	if len(executors) == 0 {
		return nil
	}
	best := executors[0]
	bestScore := float64(best.StakeAmount) * best.SuccessRate
	for _, e := range executors[1:] {
		if s := float64(e.StakeAmount) * e.SuccessRate; s > bestScore {
			best, bestScore = e, s
		}
	}
	return best
}

// StartAuction starts an auction for the given position with a deadline at deadlineBlock.
// Must be called by the jelly engine (configured as jellyEngine on the contract).
// Call this before FetchBestBid when using EXECUTION_STRATEGY=AUCTION.
func StartAuction(
	ctx context.Context,
	client *evmwrap.Client,
	cfg *config.Config,
	positionID uint64,
	deadlineBlock uint64,
) error {
	if cfg == nil || cfg.LiquidationAuctionHouseAddress == "" {
		return nil
	}
	if client == nil {
		return nil
	}
	positionIDBig := new(big.Int).SetUint64(positionID)
	deadlineBig := new(big.Int).SetUint64(deadlineBlock)
	callData, err := auctionHouseABI.Pack("startAuction", positionIDBig, deadlineBig)
	if err != nil {
		return err
	}
	_, err = client.Write(ctx, cfg.LiquidationAuctionHouseAddress, callData)
	return err
}

// FetchBestBid reads the best bid for a given numeric position ID from the
// LiquidationAuctionHouse contract, if configured. Returns empty address and 0
// if no auction house is configured or if no bids are found.
func FetchBestBid(
	ctx context.Context,
	client *evmwrap.Client,
	cfg *config.Config,
	positionID uint64,
) (string, uint64, error) {
	if cfg == nil || cfg.LiquidationAuctionHouseAddress == "" {
		return "", 0, nil
	}
	if client == nil {
		return "", 0, nil
	}

	callData, err := auctionHouseABI.Pack("getBestBid", new(big.Int).SetUint64(positionID))
	if err != nil {
		return "", 0, err
	}
	data, err := client.Read(ctx, cfg.LiquidationAuctionHouseAddress, callData)
	if err != nil {
		return "", 0, err
	}
	results, err := auctionHouseABI.Unpack("getBestBid", data)
	if err != nil || len(results) < 2 {
		return "", 0, err
	}

	bestAddr, okAddr := results[0].(common.Address)
	bestBidInt, okBid := results[1].(*big.Int)
	if !okAddr || !okBid || bestBidInt == nil {
		return "", 0, nil
	}
	if bestBidInt.Sign() <= 0 {
		return "", 0, nil
	}

	return bestAddr.Hex(), bestBidInt.Uint64(), nil
}

// UpdateExecutorStats calls ExecutorRegistry.updateExecutorStats to adjust
// success/failure counters and total OEV captured for a given executor.
// It is a thin helper around the on-chain contract and is intended to be used
// by execution/distribution workflows when a liquidation attempt succeeds or fails.
func UpdateExecutorStats(
	ctx context.Context,
	client *evmwrap.Client,
	cfg *config.Config,
	executorAddress string,
	successDelta uint64,
	failDelta uint64,
	oevDelta uint64,
) error {
	if cfg == nil || cfg.ExecutorRegistryAddress == "" {
		return nil
	}
	if client == nil {
		return nil
	}

	args := struct {
		Executor              common.Address
		SuccessfulLiquidations *big.Int
		FailedAttempts         *big.Int
		TotalOEVCaptured       *big.Int
	}{
		Executor:              common.HexToAddress(executorAddress),
		SuccessfulLiquidations: new(big.Int).SetUint64(successDelta),
		FailedAttempts:         new(big.Int).SetUint64(failDelta),
		TotalOEVCaptured:       new(big.Int).SetUint64(oevDelta),
	}
	callData, err := executorRegistryABI.Pack("updateExecutorStats", args.Executor, args.SuccessfulLiquidations, args.FailedAttempts, args.TotalOEVCaptured)
	if err != nil {
		return err
	}
	_, err = client.Write(ctx, cfg.ExecutorRegistryAddress, callData)
	return err
}

// RecordWinningBid records the winning auction parameters on-chain in the LiquidationAuctionHouse
// contract, if configured. positionID is the jelly-style string ID (e.g., "jelly_1").
func RecordWinningBid(
	ctx context.Context,
	client *evmwrap.Client,
	cfg *config.Config,
	positionID string,
	executorAddress string,
	totalBid uint64,
	upfront uint64,
) error {
	if cfg == nil || cfg.LiquidationAuctionHouseAddress == "" {
		return nil
	}
	if client == nil {
		return nil
	}

	positionNumeric := positionIDToUint64(positionID)
	if positionNumeric == 0 {
		// If parsing fails, skip recording rather than erroring hard.
		return nil
	}

	args := struct {
		PositionId    *big.Int
		Executor      common.Address
		TotalBid      *big.Int
		UpfrontAmount *big.Int
	}{
		PositionId:    new(big.Int).SetUint64(positionNumeric),
		Executor:      common.HexToAddress(executorAddress),
		TotalBid:      new(big.Int).SetUint64(totalBid),
		UpfrontAmount: new(big.Int).SetUint64(upfront),
	}
	callData, err := auctionHouseABI.Pack("recordWinningBid", args.PositionId, args.Executor, args.TotalBid, args.UpfrontAmount)
	if err != nil {
		return err
	}
	_, err = client.Write(ctx, cfg.LiquidationAuctionHouseAddress, callData)
	return err
}

// positionIDToUint64 parses "jelly_N" to N. Returns 0 if parse fails.
func positionIDToUint64(positionID string) uint64 {
	s := strings.TrimPrefix(positionID, "jelly_")
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}
