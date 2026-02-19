package detection

import (
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/adapters"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// AnswerUpdatedEventSig is keccak256("AnswerUpdated(int256,uint256,uint256)").
// Used in the log trigger filter.
var AnswerUpdatedEventSig = crypto.Keccak256Hash([]byte("AnswerUpdated(int256,uint256,uint256)"))

// DetectionResult is the output type for the detection handler.
type DetectionResult struct {
	PositionsFound int
}

// DetectionHandler holds the protocol-agnostic adapters injected at init time.
// The handler itself contains zero protocol-specific logic — all of that lives
// in the adapter implementations.
type DetectionHandler struct {
	Lending adapters.LendingPoolAdapter
	OEV     adapters.OEVCalculator
	Queue   adapters.QueueWriter
}

// NewDetectionHandler creates a handler wired to the given adapter set.
func NewDetectionHandler(
	lending adapters.LendingPoolAdapter,
	oev adapters.OEVCalculator,
	queue adapters.QueueWriter,
) *DetectionHandler {
	return &DetectionHandler{
		Lending: lending,
		OEV:     oev,
		Queue:   queue,
	}
}

// HandleOracleUpdate is the callback for Handler 1: Oracle Price Update → Detection.
//
// Trigger: AnswerUpdated event from the Chainlink Aggregator contract.
// The CRE runtime delivers the raw *evm.Log; we decode it here.
//
// This function is protocol-agnostic. It:
//  1. Decodes the oracle event (same for any Chainlink feed).
//  2. Delegates position scanning to the LendingPoolAdapter.
//  3. Delegates OEV calculation to the OEVCalculator.
//  4. Delegates queue submission to the QueueWriter.
func (h *DetectionHandler) HandleOracleUpdate(
	cfg *config.Config,
	runtime cre.Runtime,
	payload *evm.Log,
) (*DetectionResult, error) {
	logger := runtime.Logger()

	// --- Decode the AnswerUpdated event from the raw log ---
	// AnswerUpdated(int256 indexed current, uint256 indexed roundId, uint256 updatedAt)
	// Topics[0] = event sig, Topics[1] = current (indexed), Topics[2] = roundId (indexed)
	// Data = abi.encode(updatedAt)
	priceUpdate, err := decodePriceUpdate(payload)
	if err != nil {
		return nil, err
	}

	logger.Info("Oracle price update detected",
		"price", priceUpdate.Price,
		"roundId", priceUpdate.RoundID,
		"blockNumber", payload.BlockNumber,
		"protocol", h.Lending.ProtocolName(),
	)

	// --- Create EVM client for contract reads/writes ---
	evmClient := &evm.Client{ChainSelector: cfg.ChainSelector}

	// --- Scan all positions from lending pool (adapter-specific) ---
	positions, err := h.Lending.GetLiquidatablePositions(evmClient, runtime, priceUpdate)
	if err != nil {
		return nil, err
	}

	if len(positions) == 0 {
		logger.Info("No liquidatable positions found",
			"protocol", h.Lending.ProtocolName(),
		)
		return &DetectionResult{PositionsFound: 0}, nil
	}

	// --- Calculate OEV potential for each position (adapter-specific) ---
	positionsWithOEV, err := h.OEV.CalculateOEVPotential(evmClient, runtime, positions, priceUpdate)
	if err != nil {
		return nil, err
	}

	// --- Write to PriorityQueue contract (adapter-specific) ---
	err = h.Queue.SubmitPositions(evmClient, runtime, positionsWithOEV)
	if err != nil {
		return nil, err
	}

	logger.Info("Submitted liquidatable positions to priority queue",
		"count", len(positionsWithOEV),
		"protocol", h.Lending.ProtocolName(),
	)

	return &DetectionResult{PositionsFound: len(positionsWithOEV)}, nil
}

// decodePriceUpdate extracts the PriceUpdate from a raw AnswerUpdated log.
// This is Chainlink-specific but protocol-agnostic (all Chainlink feeds
// emit the same event).
func decodePriceUpdate(payload *evm.Log) (*contracts.PriceUpdate, error) {
	var currentPrice *big.Int
	var roundID *big.Int
	if len(payload.Topics) >= 3 {
		currentPrice = new(big.Int).SetBytes(payload.Topics[1])
		roundID = new(big.Int).SetBytes(payload.Topics[2])
	}

	var updatedAt uint64
	if len(payload.Data) >= 32 {
		updatedAt = new(big.Int).SetBytes(payload.Data[:32]).Uint64()
	}

	return &contracts.PriceUpdate{
		Price:     currentPrice.Uint64(),
		RoundID:   roundID.Uint64(),
		Timestamp: updatedAt,
	}, nil
}
