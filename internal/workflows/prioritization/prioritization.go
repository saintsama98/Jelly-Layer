package prioritization

import (
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

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

	// --- Calculate priority scores ---
	scorer := NewScorer()
	volatility := 0.5 // TODO: fetch real volatility via HTTP capability
	scoredPositions := make([]*contracts.ScoredPosition, 0, len(positions))
	for _, pos := range positions {
		score := scorer.CalculateScore(pos, volatility)
		scoredPositions = append(scoredPositions, &contracts.ScoredPosition{
			Position: &contracts.PositionWithOEV{Position: *pos},
			Score:    score,
		})
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
	finalQueue := adapter.AdaptPrioritization(filteredPositions, volatility)

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

// readPositionsFromQueue reads positions from PriorityQueue contract.
func readPositionsFromQueue(client *evm.Client, runtime cre.Runtime, cfg *config.Config) ([]*contracts.Position, error) {
	// TODO: Implement — client.CallContract(runtime, &evm.CallContractRequest{...})
	_ = client
	_ = runtime
	_ = cfg
	return []*contracts.Position{}, nil
}

// updatePriorityQueue writes the reordered queue back to the contract.
func updatePriorityQueue(
	client *evm.Client,
	runtime cre.Runtime,
	cfg *config.Config,
	queue []*contracts.ScoredPosition,
	deferred []*contracts.ScoredPosition,
) error {
	// TODO: Implement — client.CallContract or WriteReport
	_ = client
	_ = runtime
	_ = cfg
	_ = queue
	_ = deferred
	return nil
}
