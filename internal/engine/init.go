package engine

import (
	"fmt"
	"log/slog"
	"math/big"

	"github.com/smartcontractkit/chainlink-protos/cre/go/values/pb"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm/bindings"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/scheduler/cron"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/adapters"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/detection"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/distribution"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/execution"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/prioritization"
)

// initWorkflow creates the CRE workflow with all handlers using the real CRE SDK.
//
// Each handler is wired to an EVM log trigger via evm.LogTrigger(), which
// returns cre.Trigger[*evm.Log, *evm.Log].  The Handler function adapts the
// raw *evm.Log into the callback's payload type.
//
// For our use-case we listen for raw logs and decode them inside the handler
// callbacks using bindings.DecodedLog[T] + go-ethereum ABI decoding.
func initWorkflow(
	cfg *config.Config,
	logger *slog.Logger,
) (cre.Workflow[*config.Config], error) {

	// --- Build protocol adapters from config ---
	lending, oevCalc, queueWriter, err := adapters.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create protocol adapters: %w", err)
	}

	logger.Info("Workflow config",
		"protocol", lending.ProtocolName(),
		"chainSelector", cfg.ChainSelector,
		"oracle", cfg.OracleAddress,
		"priorityQueue", cfg.PriorityQueueAddress,
		"liquidationOrchestrator", cfg.LiquidationOrchestratorAddress,
	)

	// --- Create the detection handler with injected adapters ---
	detectionHandler := detection.NewDetectionHandler(lending, oevCalc, queueWriter)

	// --- Create EVM log triggers ---

	// Handler 1 trigger: Oracle AnswerUpdated event
	answerUpdatedTrigger := evm.LogTrigger(cfg.ChainSelector, &evm.FilterLogTriggerRequest{
		Addresses:  [][]byte{cfg.OracleAddressBytes()},
		Topics:     bindings.PadTopics(bindings.PrepareTopics(nil, detection.AnswerUpdatedEventSig[:])),
		Confidence: evm.ConfidenceLevel_CONFIDENCE_LEVEL_FINALIZED,
	})

	// Handler 2 trigger: PrioritiesSubmitted event
	prioritiesSubmittedTrigger := evm.LogTrigger(cfg.ChainSelector, &evm.FilterLogTriggerRequest{
		Addresses:  [][]byte{cfg.PriorityQueueAddressBytes()},
		Topics:     bindings.PadTopics(bindings.PrepareTopics(nil, prioritization.PrioritiesSubmittedEventSig[:])),
		Confidence: evm.ConfidenceLevel_CONFIDENCE_LEVEL_FINALIZED,
	})

	// Handler 3 trigger: QueueUpdated event
	queueUpdatedTrigger := evm.LogTrigger(cfg.ChainSelector, &evm.FilterLogTriggerRequest{
		Addresses:  [][]byte{cfg.PriorityQueueAddressBytes()},
		Topics:     bindings.PadTopics(bindings.PrepareTopics(nil, execution.QueueUpdatedEventSig[:])),
		Confidence: evm.ConfidenceLevel_CONFIDENCE_LEVEL_FINALIZED,
	})

	// Handler 4 trigger: LiquidationExecuted event
	liquidationExecutedTrigger := evm.LogTrigger(cfg.ChainSelector, &evm.FilterLogTriggerRequest{
		Addresses:  [][]byte{cfg.LiquidationOrchestratorAddressBytes()},
		Topics:     bindings.PadTopics(bindings.PrepareTopics(nil, distribution.LiquidationExecutedEventSig[:])),
		Confidence: evm.ConfidenceLevel_CONFIDENCE_LEVEL_FINALIZED,
	})

	// Handler 5: Cron trigger for demo/simulate — runs detection with synthetic AnswerUpdated log (no tx hash needed).
	cronTrigger := cron.Trigger(&cron.Config{Schedule: "0 */1 * * * *"}) // every minute; CRE simulate runs it once when selected

	// --- Build the workflow ---

	workflow := cre.Workflow[*config.Config]{
		// Handler 1: Oracle Price Update → Detection (protocol-agnostic via adapters)
		cre.Handler[*config.Config, *evm.Log, *evm.Log, *detection.DetectionResult](
			answerUpdatedTrigger,
			detectionHandler.HandleOracleUpdate,
		),
		// Handler 2: Priorities Submitted → Prioritization
		cre.Handler[*config.Config, *evm.Log, *evm.Log, *prioritization.PrioritizationResult](
			prioritiesSubmittedTrigger,
			prioritization.HandlePrioritization,
		),
		// Handler 3: Queue Updated → Execution Routing
		cre.Handler[*config.Config, *evm.Log, *evm.Log, *execution.ExecutionResult](
			queueUpdatedTrigger,
			execution.HandleExecution,
		),
		// Handler 4: Liquidation Executed → OEV Distribution
		cre.Handler[*config.Config, *evm.Log, *evm.Log, *distribution.DistributionResult](
			liquidationExecutedTrigger,
			distribution.HandleDistribution,
		),
		// Handler 5: Cron → Detection (demo/simulate; no EVM tx hash required)
		cre.Handler[*config.Config, *cron.Payload, *cron.Payload, *detection.DetectionResult](
			cronTrigger,
			handleCronDetection(cfg, detectionHandler),
		),
	}

	logger.Info("Workflow initialized with 5 handlers",
		"chain", cfg.ChainSelector,
		"protocol", lending.ProtocolName(),
	)

	return workflow, nil
}

// handleCronDetection returns a handler that runs detection with a synthetic AnswerUpdated log.
// Used for CRE workflow simulate so the demo can run without providing an EVM tx hash.
func handleCronDetection(cfg *config.Config, h *detection.DetectionHandler) func(*config.Config, cre.Runtime, *cron.Payload) (*detection.DetectionResult, error) {
	return func(c *config.Config, runtime cre.Runtime, _ *cron.Payload) (*detection.DetectionResult, error) {
		runtime.Logger().Info("Cron trigger: running detection with synthetic AnswerUpdated log (demo/simulate)")
		log := syntheticAnswerUpdatedLog(cfg.OracleAddressBytes(), 2000e8, 1, 1699000000, 1000)
		return h.HandleOracleUpdate(c, runtime, log)
	}
}

// syntheticAnswerUpdatedLog builds an evm.Log that decodes as AnswerUpdated(price, roundId, updatedAt).
func syntheticAnswerUpdatedLog(oracleAddr []byte, price uint64, roundID uint64, updatedAt uint64, blockNum int64) *evm.Log {
	priceBig := new(big.Int).SetUint64(price)
	roundBig := new(big.Int).SetUint64(roundID)
	updatedBig := new(big.Int).SetUint64(updatedAt)
	return &evm.Log{
		Address:     oracleAddr,
		Topics:      [][]byte{detection.AnswerUpdatedEventSig.Bytes(), pad32bytes(priceBig.Bytes()), pad32bytes(roundBig.Bytes())},
		Data:        pad32bytes(updatedBig.Bytes()),
		BlockNumber: pb.NewBigIntFromInt(big.NewInt(blockNum)),
	}
}

func pad32bytes(b []byte) []byte {
	if len(b) >= 32 {
		return b[:32]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}
