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

var executorRegistryABI abi.ABI

func init() {
	const abiJSON = `[{
		"name": "getActiveExecutors",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [{
			"name": "",
			"type": "tuple[]",
			"components": [
				{"name": "executorAddress", "type": "address"},
				{"name": "successfulLiquidations", "type": "uint256"},
				{"name": "failedAttempts", "type": "uint256"},
				{"name": "profitabilityScore", "type": "uint256"},
				{"name": "supportedCollaterals", "type": "bytes32[]"},
				{"name": "isActive", "type": "bool"},
				{"name": "lastExecutionTime", "type": "uint64"},
				{"name": "totalOEVCaptured", "type": "uint256"},
				{"name": "stakeAmount", "type": "uint256"}
			]
		}]
	}]`
	var err error
	executorRegistryABI, err = abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(fmt.Sprintf("execution: ExecutorRegistry ABI: %v", err))
	}
}

// FetchActiveExecutors calls ExecutorRegistry.getActiveExecutors(). Returns nil on empty registry or error (caller uses placeholder).
func FetchActiveExecutors(ctx context.Context, client *evmwrap.Client, cfg *config.Config) ([]*contracts.Executor, error) {
	if cfg == nil || cfg.ExecutorRegistryAddress == "" {
		return nil, nil
	}
	results, err := client.Read(ctx, cfg.ExecutorRegistryAddress, "getActiveExecutors")
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

// positionIDToUint64 parses "jelly_N" to N. Returns 0 if parse fails.
func positionIDToUint64(positionID string) uint64 {
	s := strings.TrimPrefix(positionID, "jelly_")
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}
