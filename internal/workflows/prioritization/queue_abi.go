package prioritization

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// priorityQueueABI holds ABIs for getPrioritizedQueue() and updateQueue().
var priorityQueueABI abi.ABI

func init() {
	const abiJSON = `[
		{
			"name": "getPrioritizedQueue",
			"type": "function",
			"stateMutability": "view",
			"inputs": [],
			"outputs": [{
				"name": "",
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
			}]
		},
		{
			"name": "updateQueue",
			"type": "function",
			"stateMutability": "nonpayable",
			"inputs": [
				{"name": "prioritizedIds", "type": "uint256[]"},
				{"name": "deferredIds", "type": "uint256[]"}
			],
			"outputs": []
		}
	]`
	var err error
	priorityQueueABI, err = abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(fmt.Sprintf("prioritization: failed to parse PriorityQueue ABI: %v", err))
	}
}

// solLiquidationPosition matches the contract's LiquidationPosition struct.
type solLiquidationPosition struct {
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

// positionIDFromString parses "jelly_0" -> 0, "jelly_123" -> 123. Returns 0 if parse fails.
func positionIDFromString(positionID string) uint64 {
	s := strings.TrimPrefix(positionID, "jelly_")
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

// positionIDToString formats numeric id to "jelly_N".
func positionIDToString(id uint64) string {
	return "jelly_" + strconv.FormatUint(id, 10)
}

// UrgencyLevelToString maps contract urgency 1-5 to string.
func UrgencyLevelToString(u uint8) string {
	switch u {
	case 5:
		return "CRITICAL"
	case 4:
		return "HIGH"
	case 3:
		return "MEDIUM"
	case 2:
		return "LOW"
	default:
		return "LOW"
	}
}

// solToPositionWithOEV converts a contract LiquidationPosition to PositionWithOEV (OEVPotential=0; CollateralToken/DebtToken not stored on contract).
func solToPositionWithOEV(sol *solLiquidationPosition, id uint64) *contracts.PositionWithOEV {
	oneE18 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	hfFloat := 0.0
	if sol.HealthFactor != nil && oneE18.Sign() != 0 {
		hfFloat, _ = new(big.Float).Quo(
			new(big.Float).SetInt(sol.HealthFactor),
			new(big.Float).SetInt(oneE18),
		).Float64()
	}
	collateral := uint64(0)
	if sol.CollateralValue != nil {
		collateral = sol.CollateralValue.Uint64()
	}
	debt := uint64(0)
	if sol.DebtValue != nil {
		debt = sol.DebtValue.Uint64()
	}
	return &contracts.PositionWithOEV{
		Position: contracts.Position{
			PositionID:      positionIDToString(id),
			UserAddress:     sol.Borrower.Hex(),
			HealthFactor:    hfFloat,
			CollateralValue: collateral,
			DebtValue:       debt,
			CollateralToken: "",
			DebtToken:       "",
		},
		OEVPotential:     0,
		EstimatedGasCost: 0,
	}
}
