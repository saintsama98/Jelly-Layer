// Simulation test runs the full pipeline (detection → prioritization → execution → distribution)
// with a mocked EVM capability and synthetic log payloads. No RPC or CRE runner required.
//
// Run: go test ./internal/engine -run TestSimulateFullPipeline -v
package engine

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/smartcontractkit/chainlink-protos/cre/go/values/pb"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	evmmock "github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm/mock"
	"github.com/smartcontractkit/cre-sdk-go/cre/testutils"

	"github.com/jelly-layer-cre/jelly-engine/internal/adapters"
	"github.com/jelly-layer-cre/jelly-engine/internal/adapters/jellymock"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/detection"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/distribution"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/execution"
	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/prioritization"
)

// TestSimulateFullPipeline runs all four handlers in sequence with stubbed EVM:
// 1. Detection: synthetic AnswerUpdated log; mock returns no liquidatable positions.
// 2. Prioritization: synthetic PrioritiesSubmitted log; mock returns empty queue.
// 3. Execution: synthetic QueueUpdated log; mock returns empty queue (nothing to execute).
// 4. Distribution: synthetic LiquidationExecuted log; mock returns success for reads/writes.
func TestSimulateFullPipeline(t *testing.T) {
	// Load config (uses env; set minimal if .env not present)
	setMinimalEnv()
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	rt := testutils.NewRuntime(t, nil)
	chainSelector := cfg.ChainSelector
	cap, err := evmmock.NewClientCapability(chainSelector, t)
	if err != nil {
		t.Fatalf("NewClientCapability: %v", err)
	}

	// Stub CallContract: return ABI-encoded empty position array for getAllPositions,
	// and empty queue for getPrioritizedQueue. Other methods return empty/success as needed.
	emptyPositions, err := jellymock.EncodeEmptyGetAllPositionsResponse()
	if err != nil {
		t.Fatalf("EncodeEmptyGetAllPositionsResponse: %v", err)
	}
	emptyQueue := mustEncodeEmptyQueue(t)
	getAllPositionsSel := crypto.Keccak256([]byte("getAllPositions()"))[:4]
	cap.CallContract = func(_ context.Context, input *evm.CallContractRequest) (*evm.CallContractReply, error) {
		if input.Call == nil || len(input.Call.Data) < 4 {
			return &evm.CallContractReply{Data: emptyPositions}, nil
		}
		if len(input.Call.Data) >= 4 && string(input.Call.Data[:4]) == string(getAllPositionsSel) {
			return &evm.CallContractReply{Data: emptyPositions}, nil
		}
		return &evm.CallContractReply{Data: emptyQueue}, nil
	}
	cap.WriteReport = func(_ context.Context, _ *evm.WriteReportRequest) (*evm.WriteReportReply, error) {
		return &evm.WriteReportReply{TxStatus: evm.TxStatus_TX_STATUS_SUCCESS, TxHash: make([]byte, 32)}, nil
	}
	cap.HeaderByNumber = func(_ context.Context, _ *evm.HeaderByNumberRequest) (*evm.HeaderByNumberReply, error) {
		return &evm.HeaderByNumberReply{
			Header: &evm.Header{BlockNumber: pb.NewBigIntFromInt(big.NewInt(1000))},
		}, nil
	}

	lending, oevCalc, queueWriter, err := adapters.NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("adapters.NewFromConfig: %v", err)
	}
	detectionHandler := detection.NewDetectionHandler(lending, oevCalc, queueWriter)

	// 1. Detection: synthetic AnswerUpdated log
	answerUpdatedLog := makeAnswerUpdatedLog(big.NewInt(2000e8), big.NewInt(1), big.NewInt(1699000000))
	res, err := detectionHandler.HandleOracleUpdate(cfg, rt, answerUpdatedLog)
	if err != nil {
		t.Fatalf("HandleOracleUpdate: %v", err)
	}
	t.Logf("Detection: positions found=%d", res.PositionsFound)

	// 2. Prioritization: synthetic PrioritiesSubmitted log
	prioritiesLog := makePrioritiesSubmittedLog(big.NewInt(0), big.NewInt(0))
	priorRes, err := prioritization.HandlePrioritization(cfg, rt, prioritiesLog)
	if err != nil {
		t.Fatalf("HandlePrioritization: %v", err)
	}
	t.Logf("Prioritization: processed=%d deferred=%d", priorRes.Processed, priorRes.Deferred)

	// 3. Execution: synthetic QueueUpdated log
	queueUpdatedLog := makeQueueUpdatedLog(big.NewInt(0), big.NewInt(0), big.NewInt(0))
	execRes, err := execution.HandleExecution(cfg, rt, queueUpdatedLog)
	if err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	t.Logf("Execution: executed=%d failed=%d", execRes.Executed, execRes.Failed)

	// 4. Distribution: synthetic LiquidationExecuted log
	liqExecLog := makeLiquidationExecutedLog(big.NewInt(0), common.HexToAddress("0x0"), big.NewInt(0), [32]byte{})
	distRes, err := distribution.HandleDistribution(cfg, rt, liqExecLog)
	if err != nil {
		t.Fatalf("HandleDistribution: %v", err)
	}
	t.Logf("Distribution: totalOEV=%d protocol=%d executor=%d validator=%d",
		distRes.TotalOEV, distRes.ProtocolShare, distRes.ExecutorShare, distRes.ValidatorShare)
}

func setMinimalEnv() {
	if os.Getenv("CHAIN_SELECTOR") == "" {
		_ = os.Setenv("CHAIN_SELECTOR", "16015286601757825753")
	}
	if os.Getenv("LENDING_POOL_ADDRESS") == "" {
		_ = os.Setenv("LENDING_POOL_ADDRESS", "0xA9251fB57a7d273aAc0aA22d42031ffa5ebeADCe")
	}
	if os.Getenv("PRIORITY_QUEUE_ADDRESS") == "" {
		_ = os.Setenv("PRIORITY_QUEUE_ADDRESS", "0xCD8e68e0492F8cbDda3C860737Cdc604A0e4a0a2")
	}
	if os.Getenv("ORACLE_ADDRESS") == "" {
		_ = os.Setenv("ORACLE_ADDRESS", "0x694AA1769357215DE4FAC081bf1f309aDC325306")
	}
	if os.Getenv("EXECUTOR_REGISTRY_ADDRESS") == "" {
		_ = os.Setenv("EXECUTOR_REGISTRY_ADDRESS", "0x0EAbEd70Bcda3bcF3678ceC605F457B717D91bdD")
	}
	if os.Getenv("LIQUIDATION_ORCHESTRATOR_ADDRESS") == "" {
		_ = os.Setenv("LIQUIDATION_ORCHESTRATOR_ADDRESS", "0x61d687Ae63E87a96bFCE041634BAf66cdcA51c33")
	}
	if os.Getenv("OEV_DISTRIBUTOR_ADDRESS") == "" {
		_ = os.Setenv("OEV_DISTRIBUTOR_ADDRESS", "0xf3aCAfE8359a41eF07E3a1990Fb624e721A76F3d")
	}
}

// Minimal ABI-encoded empty dynamic array for getPrioritizedQueue / other view calls.
func mustEncodeEmptyQueue(t *testing.T) []byte {
	t.Helper()
	return common.Hex2Bytes("0000000000000000000000000000000000000000000000000000000000000020" +
		"0000000000000000000000000000000000000000000000000000000000000000")
}

func makeAnswerUpdatedLog(current, roundID, updatedAt *big.Int) *evm.Log {
	return &evm.Log{
		Address:     common.HexToAddress("0x694AA1769357215DE4FAC081bf1f309aDC325306").Bytes(),
		Topics:      [][]byte{detection.AnswerUpdatedEventSig.Bytes(), pad32(current.Bytes()), pad32(roundID.Bytes())},
		Data:        pad32(updatedAt.Bytes()),
		BlockNumber: pb.NewBigIntFromInt(big.NewInt(1000)),
	}
}

// makePrioritiesSubmittedLog builds a log for PrioritiesSubmitted(uint256 indexed queueId, uint256 timestamp, uint256 positionCount).
func makePrioritiesSubmittedLog(queueID, count *big.Int) *evm.Log {
	topics := [][]byte{prioritization.PrioritiesSubmittedEventSig.Bytes()}
	if queueID != nil {
		topics = append(topics, pad32(queueID.Bytes()))
	}
	data := append(pad32(big.NewInt(0).Bytes()), pad32(count.Bytes())...) // timestamp=0, positionCount=count
	return &evm.Log{
		Address:     common.HexToAddress("0xCD8e68e0492F8cbDda3C860737Cdc604A0e4a0a2").Bytes(),
		Topics:      topics,
		Data:        data,
		BlockNumber: pb.NewBigIntFromInt(big.NewInt(1001)),
	}
}

func makeQueueUpdatedLog(queueID, positionCount, deferredCount *big.Int) *evm.Log {
	return &evm.Log{
		Address:     common.HexToAddress("0xCD8e68e0492F8cbDda3C860737Cdc604A0e4a0a2").Bytes(),
		Topics:      [][]byte{execution.QueueUpdatedEventSig.Bytes()},
		Data:        append(append(pad32(queueID.Bytes()), pad32(positionCount.Bytes())...), pad32(deferredCount.Bytes())...),
		BlockNumber: pb.NewBigIntFromInt(big.NewInt(1002)),
	}
}

func makeLiquidationExecutedLog(positionID *big.Int, executor common.Address, capturedOEV *big.Int, txHash [32]byte) *evm.Log {
	return &evm.Log{
		Address:     common.HexToAddress("0x61d687Ae63E87a96bFCE041634BAf66cdcA51c33").Bytes(),
		Topics:      [][]byte{distribution.LiquidationExecutedEventSig.Bytes(), pad32(positionID.Bytes()), pad32(executor.Bytes())},
		Data:        append(pad32(capturedOEV.Bytes()), txHash[:]...),
		BlockNumber: pb.NewBigIntFromInt(big.NewInt(1003)),
	}
}

func pad32(b []byte) []byte {
	if len(b) >= 32 {
		return b[:32]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}
