package jellymock

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/smartcontractkit/chainlink-protos/cre/go/sdk"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// submitPositionsABI is the ABI for PriorityQueue.submitLiquidatablePositions().
var submitPositionsABI abi.ABI

func init() {
	const abiJSON = `[{
		"name": "submitLiquidatablePositions",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{
			"name": "positions",
			"type": "tuple[]",
			"components": [
				{"name": "positionId", "type": "uint256"},
				{"name": "borrower", "type": "address"},
				{"name": "healthFactor", "type": "uint256"},
				{"name": "collateralValue", "type": "uint256"},
				{"name": "debtValue", "type": "uint256"},
				{"name": "priorityScore", "type": "uint256"},
				{"name": "urgencyLevel", "type": "uint8"},
				{"name": "timestamp", "type": "uint64"},
				{"name": "executionData", "type": "bytes"}
			]
		}],
		"outputs": []
	}]`
	var err error
	submitPositionsABI, err = abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(fmt.Sprintf("failed to parse PriorityQueue ABI: %v", err))
	}
}

// QueueWriterImpl is the JellyMock implementation of the generic QueueWriter
// interface. It knows how to talk to the PriorityQueue mock contract.
type QueueWriterImpl struct {
	QueueAddress common.Address
}

// NewQueueWriter creates a new queue writer.
func NewQueueWriter(queueAddress string) *QueueWriterImpl {
	return &QueueWriterImpl{
		QueueAddress: common.HexToAddress(queueAddress),
	}
}

// SubmitPositions writes detected positions to the PriorityQueue contract.
//
// NOTE: As with the lending adapter, we treat *evm.Client as an opaque handle
// compatible with the CRE SDK EVM client. We only assume CallContract exists.
func (w *QueueWriterImpl) SubmitPositions(
	client *evm.Client,
	runtime cre.Runtime,
	positions []*contracts.PositionWithOEV,
) error {
	logger := runtime.Logger()

	if client == nil {
		logger.Warn("jellymock: EVM client is nil, skipping queue submission")
		return nil
	}

	// --- Map Go types → Solidity struct format ---
	type solPosition struct {
		PositionId      *big.Int       `abi:"positionId"`
		Borrower        common.Address `abi:"borrower"`
		HealthFactor    *big.Int       `abi:"healthFactor"`
		CollateralValue *big.Int       `abi:"collateralValue"`
		DebtValue       *big.Int       `abi:"debtValue"`
		PriorityScore   *big.Int       `abi:"priorityScore"`
		UrgencyLevel    uint8          `abi:"urgencyLevel"`
		Timestamp       uint64         `abi:"timestamp"`
		ExecutionData   []byte         `abi:"executionData"`
	}

	oneE18 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	solPositions := make([]solPosition, 0, len(positions))

	for i, pos := range positions {
		// Convert health factor float64 → uint256 (multiply by 1e18)
		hfBig := new(big.Float).Mul(
			new(big.Float).SetFloat64(pos.HealthFactor),
			new(big.Float).SetInt(oneE18),
		)
		hfInt, _ := hfBig.Int(nil)

		// Determine urgency level based on health factor
		var urgency uint8
		switch {
		case pos.HealthFactor < 0.5:
			urgency = 5 // CRITICAL
		case pos.HealthFactor < 0.7:
			urgency = 4 // HIGH
		case pos.HealthFactor < 0.85:
			urgency = 3 // MEDIUM
		case pos.HealthFactor < 0.95:
			urgency = 2 // LOW
		default:
			urgency = 1
		}

		solPositions = append(solPositions, solPosition{
			PositionId:      big.NewInt(int64(i)),
			Borrower:        common.HexToAddress(pos.UserAddress),
			HealthFactor:    hfInt,
			CollateralValue: new(big.Int).SetUint64(pos.CollateralValue),
			DebtValue:       new(big.Int).SetUint64(pos.DebtValue),
			PriorityScore:   big.NewInt(0), // scored later by Handler 2 (Prioritization)
			UrgencyLevel:    urgency,
			Timestamp:       0, // contract uses block.timestamp
			ExecutionData:   []byte{},
		})
	}

	// --- ABI-encode the call ---
	callData, err := submitPositionsABI.Pack("submitLiquidatablePositions", solPositions)
	if err != nil {
		return fmt.Errorf("failed to pack submitLiquidatablePositions: %w", err)
	}

	// --- Send the transaction via CRE EVM WriteReport (receiver = contract, report = calldata) ---
	report, err := cre.X_GeneratedCodeOnly_WrapReport(&sdk.ReportResponse{
		RawReport: callData,
	})
	if err != nil {
		return fmt.Errorf("failed to wrap report for queue submit: %w", err)
	}
	req := &evm.WriteCreReportRequest{
		Receiver:  w.QueueAddress.Bytes(),
		Report:    report,
		GasConfig: nil,
	}
	reply, err := client.WriteReport(runtime, req).Await()
	if err != nil {
		return fmt.Errorf("failed to submit positions to priority queue: %w", err)
	}

	txHashHex := ""
	if reply != nil && len(reply.TxHash) >= 32 {
		txHashHex = common.BytesToHash(reply.TxHash).Hex()
	}
	logger.Info("[Detection] PriorityQueue API: submitLiquidatablePositions success",
		"count", len(solPositions),
		"contract", w.QueueAddress.Hex(),
		"txHash", txHashHex,
	)
	if txHashHex != "" {
		logger.Info("[Detection] Next: run Prioritization with --evm-tx-hash " + txHashHex)
	}

	return nil
}


