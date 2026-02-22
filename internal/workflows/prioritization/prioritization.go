package prioritization

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/capabilities/volatility"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// PrioritiesSubmittedEventSig is keccak256("PrioritiesSubmitted(uint256,uint256)").
var PrioritiesSubmittedEventSig = crypto.Keccak256Hash([]byte("PrioritiesSubmitted(uint256,uint256)"))

// PrioritizationResult is the output type for the prioritization handler.
type PrioritizationResult struct {
	Processed int
	Deferred  int
}

// HandlePrioritization is the callback for Handler 2: Priorities Submitted → Prioritization.
//
// Trigger: PrioritiesSubmitted event from the PriorityQueue contract.
func HandlePrioritization(
	cfg *config.Config,
	runtime cre.Runtime,
	payload *evm.Log,
) (*PrioritizationResult, error) {
	logger := runtime.Logger()

	// Decode PrioritiesSubmitted(uint256 batchId, uint256 count)
	// Non-indexed params are in Data
	var batchID, count *big.Int
	if len(payload.Data) >= 64 {
		batchID = new(big.Int).SetBytes(payload.Data[:32])
		count = new(big.Int).SetBytes(payload.Data[32:64])
	}

	logger.Info("Prioritization triggered",
		"batchId", batchID,
		"count", count,
		"blockNumber", payload.BlockNumber,
	)

	// --- Create EVM client ---
	evmClient := &evm.Client{ChainSelector: cfg.ChainSelector}

	// --- Read positions from PriorityQueue contract ---
	positions, err := readPositionsFromQueue(evmClient, runtime, cfg)
	if err != nil {
		return nil, err
	}

	// --- Fetch real-time volatility per collateral token and score positions ---
	ctx := context.Background()
	volCalculator := volatility.NewCalculator()
	volByToken := fetchVolatilityByToken(ctx, volCalculator, positionsToPositions(positions), logger)

	scorer := NewScorer()
	scoredPositions := make([]*contracts.ScoredPosition, 0, len(positions))
	var volSum float64
	for _, pos := range positions {
		vol := volByToken[pos.CollateralToken]
		volSum += vol
		score := scorer.CalculateScore(pos, vol)
		scoredPositions = append(scoredPositions, &contracts.ScoredPosition{
			Position: pos,
			Score:    score,
		})
	}

	// Mean volatility across positions for market-adapter strategy
	meanVolatility := 0.5
	if len(positions) > 0 {
		meanVolatility = volSum / float64(len(positions))
	}

	// --- Apply anti-cascading logic ---
	analyzer := NewCascadingAnalyzer(cfg.MaxPriceImpact, cfg.MaxLiquidationCapacity)
	var filteredPositions []*contracts.ScoredPosition
	var deferredPositions []*contracts.ScoredPosition

	for _, pos := range scoredPositions {
		isCascading, reason := analyzer.CheckCascadingRisk(
			pos, filteredPositions, &contracts.MarketState{},
		)
		if isCascading {
			logger.Info("Position deferred due to cascading risk",
				"positionId", pos.Position.PositionID,
				"reason", reason,
			)
			deferredPositions = append(deferredPositions, pos)
		} else {
			filteredPositions = append(filteredPositions, pos)
		}
	}

	// --- Adapt to market conditions ---
	adapter := NewMarketAdapter()
	finalQueue := adapter.AdaptPrioritization(filteredPositions, meanVolatility)

	// --- Write updated queue to contract ---
	err = updatePriorityQueue(evmClient, runtime, cfg, finalQueue, deferredPositions)
	if err != nil {
		return nil, err
	}

	logger.Info("Prioritization complete",
		"processed", len(finalQueue),
		"deferred", len(deferredPositions),
	)

	return &PrioritizationResult{
		Processed: len(finalQueue),
		Deferred:  len(deferredPositions),
	}, nil
}

const defaultVolatility = 0.5

// volatilityLogger is used to log volatility fetch failures; satisfied by runtime.Logger().
type volatilityLogger interface {
	Warn(msg string, keyvals ...any)
}

// positionsToPositions returns the inner Position slice from a slice of PositionWithOEV.
func positionsToPositions(positions []*contracts.PositionWithOEV) []*contracts.Position {
	out := make([]*contracts.Position, len(positions))
	for i, p := range positions {
		if p != nil {
			p2 := p.Position
			out[i] = &p2
		}
	}
	return out
}

// fetchVolatilityByToken returns a map of collateral token symbol -> volatility (0-1).
// Uses the volatility calculator for each unique token; on error or empty data uses defaultVolatility.
func fetchVolatilityByToken(
	ctx context.Context,
	calc *volatility.Calculator,
	positions []*contracts.Position,
	logger volatilityLogger,
) map[string]float64 {
	// Unique collateral tokens
	seen := make(map[string]struct{})
	for _, pos := range positions {
		if pos.CollateralToken != "" {
			seen[pos.CollateralToken] = struct{}{}
		}
	}
	out := make(map[string]float64, len(seen)+1)
	out[""] = defaultVolatility
	for token := range seen {
		vol, err := calc.CalculateVolatility(ctx, token)
		if err != nil {
			logger.Warn("volatility fetch failed, using default", "token", token, "error", err)
			out[token] = defaultVolatility
			continue
		}
		out[token] = vol
	}
	return out
}

// readPositionsFromQueue reads positions from PriorityQueue contract via getPrioritizedQueue().
func readPositionsFromQueue(client *evm.Client, runtime cre.Runtime, cfg *config.Config) ([]*contracts.PositionWithOEV, error) {
	if client == nil {
		return []*contracts.PositionWithOEV{}, nil
	}
	queueAddr := common.HexToAddress(cfg.PriorityQueueAddress)
	callData, err := priorityQueueABI.Pack("getPrioritizedQueue")
	if err != nil {
		return nil, fmt.Errorf("pack getPrioritizedQueue: %w", err)
	}
	resp, err := (*client).CallContract(runtime, &evm.CallContractRequest{
		ContractAddress: queueAddr.Bytes(),
		CallData:        callData,
	})
	if err != nil {
		return nil, fmt.Errorf("call getPrioritizedQueue: %w", err)
	}
	results, err := priorityQueueABI.Unpack("getPrioritizedQueue", resp.ReturnData)
	if err != nil {
		return nil, fmt.Errorf("unpack getPrioritizedQueue: %w", err)
	}
	rawSlice, ok := results[0].([]struct {
		PositionId      *big.Int       `abi:"positionId"`
		Borrower        common.Address `abi:"borrower"`
		HealthFactor    *big.Int       `abi:"healthFactor"`
		CollateralValue *big.Int       `abi:"collateralValue"`
		DebtValue       *big.Int       `abi:"debtValue"`
		PriorityScore   *big.Int       `abi:"priorityScore"`
		UrgencyLevel    uint8          `abi:"urgencyLevel"`
		Timestamp       uint64         `abi:"timestamp"`
		ExecutionData   []byte         `abi:"executionData"`
	})
	if !ok {
		return nil, fmt.Errorf("unexpected type from getPrioritizedQueue")
	}
	out := make([]*contracts.PositionWithOEV, 0, len(rawSlice))
	for i := range rawSlice {
		sol := &solLiquidationPosition{
			PositionId:      rawSlice[i].PositionId,
			Borrower:        rawSlice[i].Borrower,
			HealthFactor:    rawSlice[i].HealthFactor,
			CollateralValue: rawSlice[i].CollateralValue,
			DebtValue:       rawSlice[i].DebtValue,
			PriorityScore:   rawSlice[i].PriorityScore,
			UrgencyLevel:    rawSlice[i].UrgencyLevel,
			Timestamp:       rawSlice[i].Timestamp,
			ExecutionData:   rawSlice[i].ExecutionData,
		}
		id := uint64(0)
		if rawSlice[i].PositionId != nil {
			id = rawSlice[i].PositionId.Uint64()
		}
		out = append(out, solToPositionWithOEV(sol, id))
	}
	return out, nil
}

// updatePriorityQueue writes the reordered queue back to the contract via updateQueue(prioritizedIds, deferredIds).
func updatePriorityQueue(
	client *evm.Client,
	runtime cre.Runtime,
	cfg *config.Config,
	queue []*contracts.ScoredPosition,
	deferred []*contracts.ScoredPosition,
) error {
	if client == nil {
		return nil
	}
	prioritizedIds := make([]*big.Int, 0, len(queue))
	for _, sp := range queue {
		if sp != nil && sp.Position != nil {
			prioritizedIds = append(prioritizedIds, big.NewInt(int64(positionIDFromString(sp.Position.PositionID))))
		}
	}
	deferredIds := make([]*big.Int, 0, len(deferred))
	for _, sp := range deferred {
		if sp != nil && sp.Position != nil {
			deferredIds = append(deferredIds, big.NewInt(int64(positionIDFromString(sp.Position.PositionID))))
		}
	}
	queueAddr := common.HexToAddress(cfg.PriorityQueueAddress)
	callData, err := priorityQueueABI.Pack("updateQueue", prioritizedIds, deferredIds)
	if err != nil {
		return fmt.Errorf("pack updateQueue: %w", err)
	}
	_, err = (*client).CallContract(runtime, &evm.CallContractRequest{
		ContractAddress: queueAddr.Bytes(),
		CallData:        callData,
	})
	if err != nil {
		return fmt.Errorf("call updateQueue: %w", err)
	}
	return nil
}

// ReadPrioritizedQueueAsScored reads the queue via getPrioritizedQueue and returns ScoredPosition slice
// (for Handler 3 execution). Uses priorityScore and urgencyLevel from the contract.
func ReadPrioritizedQueueAsScored(client *evm.Client, runtime cre.Runtime, cfg *config.Config) ([]*contracts.ScoredPosition, error) {
	if client == nil {
		return []*contracts.ScoredPosition{}, nil
	}
	queueAddr := common.HexToAddress(cfg.PriorityQueueAddress)
	callData, err := priorityQueueABI.Pack("getPrioritizedQueue")
	if err != nil {
		return nil, fmt.Errorf("pack getPrioritizedQueue: %w", err)
	}
	resp, err := (*client).CallContract(runtime, &evm.CallContractRequest{
		ContractAddress: queueAddr.Bytes(),
		CallData:        callData,
	})
	if err != nil {
		return nil, fmt.Errorf("call getPrioritizedQueue: %w", err)
	}
	results, err := priorityQueueABI.Unpack("getPrioritizedQueue", resp.ReturnData)
	if err != nil {
		return nil, fmt.Errorf("unpack getPrioritizedQueue: %w", err)
	}
	rawSlice, ok := results[0].([]struct {
		PositionId      *big.Int       `abi:"positionId"`
		Borrower        common.Address `abi:"borrower"`
		HealthFactor    *big.Int       `abi:"healthFactor"`
		CollateralValue *big.Int       `abi:"collateralValue"`
		DebtValue       *big.Int       `abi:"debtValue"`
		PriorityScore   *big.Int       `abi:"priorityScore"`
		UrgencyLevel    uint8          `abi:"urgencyLevel"`
		Timestamp       uint64         `abi:"timestamp"`
		ExecutionData   []byte         `abi:"executionData"`
	})
	if !ok {
		return nil, fmt.Errorf("unexpected type from getPrioritizedQueue")
	}
	out := make([]*contracts.ScoredPosition, 0, len(rawSlice))
	for i := range rawSlice {
		sol := &solLiquidationPosition{
			PositionId:      rawSlice[i].PositionId,
			Borrower:        rawSlice[i].Borrower,
			HealthFactor:    rawSlice[i].HealthFactor,
			CollateralValue: rawSlice[i].CollateralValue,
			DebtValue:       rawSlice[i].DebtValue,
			PriorityScore:   rawSlice[i].PriorityScore,
			UrgencyLevel:    rawSlice[i].UrgencyLevel,
			Timestamp:       rawSlice[i].Timestamp,
			ExecutionData:   rawSlice[i].ExecutionData,
		}
		id := uint64(0)
		if rawSlice[i].PositionId != nil {
			id = rawSlice[i].PositionId.Uint64()
		}
		pos := solToPositionWithOEV(sol, id)
		score := 0.0
		if rawSlice[i].PriorityScore != nil {
			score = float64(rawSlice[i].PriorityScore.Uint64()) / 10000.0
		}
		out = append(out, &contracts.ScoredPosition{
			Position:     pos,
			Score:        score,
			UrgencyLevel: UrgencyLevelToString(rawSlice[i].UrgencyLevel),
		})
	}
	return out, nil
}
