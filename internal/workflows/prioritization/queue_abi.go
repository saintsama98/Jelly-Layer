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

// priorityQueueABI holds ABIs for getPrioritizedQueue(), updateQueue(), and submitLiquidatablePositions (for decoding tx input).
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
		},
		{
			"name": "getPosition",
			"type": "function",
			"stateMutability": "view",
			"inputs": [{"name": "positionId", "type": "uint256"}],
			"outputs": [{
				"name": "",
				"type": "tuple",
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
		}
	]`
	var err error
	priorityQueueABI, err = abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(fmt.Sprintf("prioritization: failed to parse PriorityQueue ABI: %v", err))
	}
}

// DecodeGetPositionReply decodes getPosition(uint256) return value (single LiquidationPosition tuple) from raw ABI data.
func DecodeGetPositionReply(data []byte) (*solLiquidationPosition, error) {
	if len(data) < 32*9 {
		return nil, fmt.Errorf("getPosition reply too short")
	}
	sol := &solLiquidationPosition{
		PositionId:      new(big.Int).SetBytes(data[0:32]),
		Borrower:        common.BytesToAddress(data[44:64]),   // word 2: address right-padded
		HealthFactor:    new(big.Int).SetBytes(data[64:96]),
		CollateralValue: new(big.Int).SetBytes(data[96:128]),
		DebtValue:       new(big.Int).SetBytes(data[128:160]),
		PriorityScore:   new(big.Int).SetBytes(data[160:192]),
		UrgencyLevel:    data[192+31],
		Timestamp:       new(big.Int).SetBytes(data[224:256]).Uint64(),
	}
	execOffset := new(big.Int).SetBytes(data[256:288]).Uint64()
	if uint64(len(data)) >= execOffset+32 {
		execLen := new(big.Int).SetBytes(data[execOffset : execOffset+32]).Uint64()
		if uint64(len(data)) >= execOffset+32+execLen {
			sol.ExecutionData = append([]byte(nil), data[execOffset+32:execOffset+32+execLen]...)
		}
	}
	return sol, nil
}

// DecodeUpdateQueueCalldata decodes updateQueue(prioritizedIds, deferredIds) from tx data and returns prioritizedIds.
// Uses manual decode to avoid ABI unpack slice-boundary issues with dynamic arrays.
func DecodeUpdateQueueCalldata(data []byte) (prioritizedIds []*big.Int, err error) {
	if len(data) < 4+64 {
		return nil, fmt.Errorf("tx data too short")
	}
	payload := data[4:]
	off1 := new(big.Int).SetBytes(payload[0:32]).Uint64()
	var length uint64
	var start uint64
	if uint64(len(payload)) >= off1+32 {
		length = new(big.Int).SetBytes(payload[off1 : off1+32]).Uint64()
		start = off1 + 32
	} else if uint64(len(payload)) >= 32 {
		// Fallback: no offset word, payload is length then ids
		length = new(big.Int).SetBytes(payload[0:32]).Uint64()
		start = 32
	} else {
		return nil, fmt.Errorf("payload too short for prioritizedIds")
	}
	if length > 256 {
		// May be mis-decoded (e.g. offset used as length); try single id at start if payload has room
		if uint64(len(payload)) >= start+32 {
			length = 1
		} else {
			return nil, fmt.Errorf("invalid prioritizedIds length")
		}
	}
	if uint64(len(payload)) < start+length*32 {
		return nil, fmt.Errorf("payload too short for %d ids", length)
	}
	out := make([]*big.Int, 0, length)
	for i := uint64(0); i < length; i++ {
		out = append(out, new(big.Int).SetBytes(payload[start+i*32:start+(i+1)*32]))
	}
	return out, nil
}

// DecodePositionsFromSubmitTxData decodes submitLiquidatablePositions(positions) calldata and returns positions.
// Used when queue at latest is empty but we have the submit tx (e.g. simulate without historical state).
func DecodePositionsFromSubmitTxData(data []byte) ([]*contracts.PositionWithOEV, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("tx data too short")
	}
	m, ok := priorityQueueABI.Methods["submitLiquidatablePositions"]
	if !ok {
		return nil, fmt.Errorf("submitLiquidatablePositions not in ABI")
	}
	// 1) Standard: selector (4) + args (tuple[]).
	if len(data) >= 4 {
		payload := data[4:]
		if args, err := m.Inputs.Unpack(payload); err == nil && len(args) > 0 {
			if out, err := decodeQueueFromReflection(args[0]); err == nil {
				return out, nil
			}
		}
		if results, err := priorityQueueABI.Unpack("getPrioritizedQueue", payload); err == nil && len(results) > 0 {
			if out, err := decodeQueueFromReflection(results[0]); err == nil {
				return out, nil
			}
		}
	}
	// 2) Manual ABI decode: payload = offset(32) + length(32) + tuples; each tuple = 9 words, last is offset to bytes.
	if len(data) >= 4+64 {
		out, err := decodeSubmitTxPayloadManual(data[4:])
		if err == nil && len(out) > 0 {
			return out, nil
		}
		// Surface error for debugging (caller may log)
		return nil, fmt.Errorf("decode tx data (len=%d): manual=%v", len(data), err)
	}
	return nil, fmt.Errorf("could not decode positions from tx data (len=%d)", len(data))
}

// decodeSubmitTxPayloadManual decodes ABI-encoded tuple[] (same as submitLiquidatablePositions single arg) by hand.
// Layout: word0=offset to array, word1=length; array data at offset: each tuple 9 words (last is offset to executionData bytes).
func decodeSubmitTxPayloadManual(payload []byte) ([]*contracts.PositionWithOEV, error) {
	if len(payload) < 64 {
		return nil, fmt.Errorf("payload too short")
	}
	word0 := new(big.Int).SetBytes(payload[0:32]).Uint64()
	word1 := new(big.Int).SetBytes(payload[32:64]).Uint64()
	var dataStart uint64
	var length uint64
	// Standard ABI: word0 = offset to array, word1 = length
	if word0 >= 32 && word0 < uint64(len(payload)) && word1 > 0 && word1 <= 256 {
		dataStart = word0 + 32
		length = word1
	}
	if length == 0 || uint64(len(payload)) < dataStart+length*32*9 {
		// Fallback: single position at 64 (offset 32 + length 32, tuple at 64) — common for submit with 1 position
		if uint64(len(payload)) >= 64+32*9 {
			dataStart = 64
			length = 1
		} else if word0 >= 1 && word0 <= 100 && uint64(len(payload)) >= 32+word0*32*9 {
			length = word0
			dataStart = 32
		}
	}
	if length == 0 {
		return nil, fmt.Errorf("empty array")
	}
	if length > 256 {
		return nil, fmt.Errorf("invalid array length")
	}
	if uint64(len(payload)) < dataStart+length*32*9 {
		return nil, fmt.Errorf("payload too short for array")
	}
	out := make([]*contracts.PositionWithOEV, 0, length)
	for i := uint64(0); i < length; i++ {
		tupleStart := dataStart + i*32*9 // each tuple: 9 words
		if tupleStart+32*9 > uint64(len(payload)) {
			break
		}
		sol := &solLiquidationPosition{
			PositionId:      new(big.Int).SetBytes(payload[tupleStart : tupleStart+32]),
			Borrower:        common.BytesToAddress(payload[tupleStart+12 : tupleStart+32]), // address right-padded to 32
			HealthFactor:    new(big.Int).SetBytes(payload[tupleStart+32*2 : tupleStart+32*3]),
			CollateralValue: new(big.Int).SetBytes(payload[tupleStart+32*3 : tupleStart+32*4]),
			DebtValue:       new(big.Int).SetBytes(payload[tupleStart+32*4 : tupleStart+32*5]),
			PriorityScore:   new(big.Int).SetBytes(payload[tupleStart+32*5 : tupleStart+32*6]),
			UrgencyLevel:    payload[tupleStart+32*6+31],
			Timestamp:       new(big.Int).SetBytes(payload[tupleStart+32*7 : tupleStart+32*8]).Uint64(),
		}
		execOffset := new(big.Int).SetBytes(payload[tupleStart+32*8 : tupleStart+32*9]).Uint64()
		if uint64(len(payload)) >= execOffset+32 {
			execLen := new(big.Int).SetBytes(payload[execOffset : execOffset+32]).Uint64()
			if uint64(len(payload)) >= execOffset+32+execLen {
				sol.ExecutionData = append([]byte(nil), payload[execOffset+32:execOffset+32+execLen]...)
			}
		}
		id := uint64(0)
		if sol.PositionId != nil {
			id = sol.PositionId.Uint64()
		}
		out = append(out, solToPositionWithOEV(sol, id))
	}
	return out, nil
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
