package engine

import (
	"fmt"
	"log/slog"

	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm/bindings"
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

	logger.Info("Protocol adapters initialized",
		"protocol", lending.ProtocolName(),
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
	}

	logger.Info("Workflow initialized with 4 handlers",
		"chain", cfg.ChainSelector,
		"protocol", lending.ProtocolName(),
	)

	return workflow, nil
}
