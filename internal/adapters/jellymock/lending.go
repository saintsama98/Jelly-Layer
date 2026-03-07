package jellymock

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// getAllPositionsABI is the ABI for JellyLendingPool.getAllPositions().
// Returns: Position[] where Position = (address, uint256, address, uint256, address, uint256, bool)
var getAllPositionsABI abi.ABI

func init() {
	const abiJSON = `[{
		"name": "getAllPositions",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [{
			"name": "",
			"type": "tuple[]",
			"components": [
				{"name": "borrower", "type": "address"},
				{"name": "collateralAmount", "type": "uint256"},
				{"name": "collateralToken", "type": "address"},
				{"name": "debtAmount", "type": "uint256"},
				{"name": "debtToken", "type": "address"},
				{"name": "healthFactor", "type": "uint256"},
				{"name": "isActive", "type": "bool"}
			]
		}]
	},
	{
		"name": "liquidate",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "borrower", "type": "address"}],
		"outputs": []
	}]`
	var err error
	getAllPositionsABI, err = abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(fmt.Sprintf("failed to parse JellyLendingPool ABI: %v", err))
	}
}

// positionTuple matches the Solidity tuple decoded by getAllPositions (for encoding empty response).
type positionTuple struct {
	Borrower         common.Address `abi:"borrower"`
	CollateralAmount *big.Int       `abi:"collateralAmount"`
	CollateralToken  common.Address `abi:"collateralToken"`
	DebtAmount       *big.Int       `abi:"debtAmount"`
	DebtToken        common.Address `abi:"debtToken"`
	HealthFactor     *big.Int       `abi:"healthFactor"`
	IsActive         bool           `abi:"isActive"`
}

// decodePositionsFromReflection converts a decoded slice (any struct type with matching fields)
// into []positionTuple so we can handle whatever type go-ethereum's ABI decoder returns.
func decodePositionsFromReflection(v interface{}) ([]positionTuple, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, fmt.Errorf("not a slice (kind=%v)", rv.Kind())
	}
	n := rv.Len()
	out := make([]positionTuple, 0, n)
	for i := 0; i < n; i++ {
		elem := rv.Index(i)
		if elem.Kind() == reflect.Interface {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			return nil, fmt.Errorf("element %d is not a struct", i)
		}
		var p positionTuple
		// Field order in Solidity: borrower, collateralAmount, collateralToken, debtAmount, debtToken, healthFactor, isActive
		for j := 0; j < elem.NumField() && j < 7; j++ {
			f := elem.Field(j)
			if !f.CanInterface() {
				continue
			}
			switch j {
			case 0:
				if addr, ok := f.Interface().(common.Address); ok {
					p.Borrower = addr
				}
			case 1:
				if b, ok := f.Interface().(*big.Int); ok {
					p.CollateralAmount = b
				}
			case 2:
				if addr, ok := f.Interface().(common.Address); ok {
					p.CollateralToken = addr
				}
			case 3:
				if b, ok := f.Interface().(*big.Int); ok {
					p.DebtAmount = b
				}
			case 4:
				if addr, ok := f.Interface().(common.Address); ok {
					p.DebtToken = addr
				}
			case 5:
				if b, ok := f.Interface().(*big.Int); ok {
					p.HealthFactor = b
				}
			case 6:
				if b, ok := f.Interface().(bool); ok {
					p.IsActive = b
				}
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// EncodeEmptyGetAllPositionsResponse returns ABI-encoded empty position array for tests/simulation.
func EncodeEmptyGetAllPositionsResponse() ([]byte, error) {
	m, ok := getAllPositionsABI.Methods["getAllPositions"]
	if !ok {
		return nil, fmt.Errorf("getAllPositions not found")
	}
	return m.Outputs.PackValues([]interface{}{[]positionTuple{}})
}

// LendingAdapter is the JellyMock implementation of the generic LendingPoolAdapter
// interface. It knows how to talk to the JellyLendingPool mock contract.
type LendingAdapter struct {
	PoolAddress common.Address
}

// NewLendingAdapter creates a new JellyMock lending adapter.
func NewLendingAdapter(poolAddress string) *LendingAdapter {
	return &LendingAdapter{
		PoolAddress: common.HexToAddress(poolAddress),
	}
}

// ProtocolName returns the protocol identifier.
func (a *LendingAdapter) ProtocolName() string {
	return "jelly_mock"
}

// GetLiquidatablePositions calls JellyLendingPool.getAllPositions(), recalculates
// health factors using the new oracle price, and returns positions where HF < 1.0.
//
// NOTE: The type of client (*evm.Client) comes from the generic adapter interface.
// We treat it as an opaque handle compatible with the CRE SDK EVM client and do
// not rely on its concrete implementation details here.
func (a *LendingAdapter) GetLiquidatablePositions(
	client *evm.Client,
	runtime cre.Runtime,
	priceUpdate *contracts.PriceUpdate,
) ([]*contracts.Position, error) {
	logger := runtime.Logger()

	// If no client is provided (e.g. during tests), return an empty set.
	if client == nil {
		logger.Warn("jellymock: EVM client is nil, skipping on-chain scan")
		return []*contracts.Position{}, nil
	}

	// --- ABI-encode the call to getAllPositions() ---
	callData, err := getAllPositionsABI.Pack("getAllPositions")
	if err != nil {
		return nil, fmt.Errorf("jellymock: failed to pack getAllPositions: %w", err)
	}

	// --- Call the contract via CRE EVM client (CallMsg + Promise.Await, reply.Data) ---
	req := &evm.CallContractRequest{
		Call: &evm.CallMsg{
			To:   a.PoolAddress.Bytes(),
			Data: callData,
		},
		BlockNumber: nil,
	}
	reply, err := client.CallContract(runtime, req).Await()
	if err != nil {
		return nil, fmt.Errorf("jellymock: failed to call getAllPositions: %w", err)
	}
	if reply == nil {
		return nil, fmt.Errorf("jellymock: nil CallContractReply")
	}

	// --- ABI-decode the response (reply.Data, not ReturnData) ---
	results, err := getAllPositionsABI.Unpack("getAllPositions", reply.Data)
	if err != nil {
		return nil, fmt.Errorf("jellymock: failed to unpack getAllPositions: %w", err)
	}

	// The result is a slice of structs. go-ethereum ABI can return a slice of anonymous structs
	// that doesn't match our named type; try direct assertion, then inline struct, then reflection.
	var rawPositions []positionTuple
	if typed, ok := results[0].([]positionTuple); ok {
		rawPositions = typed
	} else if rawInline, ok := results[0].([]struct {
		Borrower         common.Address `abi:"borrower"`
		CollateralAmount *big.Int       `abi:"collateralAmount"`
		CollateralToken  common.Address `abi:"collateralToken"`
		DebtAmount       *big.Int       `abi:"debtAmount"`
		DebtToken        common.Address `abi:"debtToken"`
		HealthFactor     *big.Int       `abi:"healthFactor"`
		IsActive         bool           `abi:"isActive"`
	}); ok {
		rawPositions = make([]positionTuple, len(rawInline))
		for i := range rawInline {
			rawPositions[i] = positionTuple{
				Borrower:         rawInline[i].Borrower,
				CollateralAmount: rawInline[i].CollateralAmount,
				CollateralToken:  rawInline[i].CollateralToken,
				DebtAmount:       rawInline[i].DebtAmount,
				DebtToken:        rawInline[i].DebtToken,
				HealthFactor:     rawInline[i].HealthFactor,
				IsActive:         rawInline[i].IsActive,
			}
		}
	} else {
		decoded, err := decodePositionsFromReflection(results[0])
		if err != nil {
			logger.Info("getAllPositions decode fallback failed, treating as no positions", "err", err, "type", reflect.TypeOf(results[0]).String())
			return []*contracts.Position{}, nil
		}
		rawPositions = decoded
	}

	logger.Info("[Detection] Lending pool API: getAllPositions returned",
		"total", len(rawPositions),
		"pool", a.PoolAddress.Hex(),
	)

	// --- Recalculate health factors with new oracle price and filter ---
	oneE18 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	newPrice := new(big.Int).SetUint64(priceUpdate.Price)
	var liquidatable []*contracts.Position

	for i, raw := range rawPositions {
		if !raw.IsActive {
			continue
		}
		if raw.DebtAmount == nil || raw.DebtAmount.Sign() == 0 {
			continue // no debt = not liquidatable
		}

		// Recalculate: newHF = (collateralAmount * newPrice * 1e18) / (debtAmount * 1e18)
		// Simplified — in production you'd factor in collateral/debt token decimals
		// and the oracle's own decimal precision.
		numerator := new(big.Int).Mul(raw.CollateralAmount, newPrice)
		numerator.Mul(numerator, oneE18)
		denominator := new(big.Int).Mul(raw.DebtAmount, oneE18)
		newHF := new(big.Int).Div(numerator, denominator)

		// Filter: healthFactor < 1e18 means undercollateralized
		if newHF.Cmp(oneE18) >= 0 {
			continue
		}

		// Convert big.Int health factor to float64 (HF / 1e18)
		hfFloat, _ := new(big.Float).Quo(
			new(big.Float).SetInt(newHF),
			new(big.Float).SetInt(oneE18),
		).Float64()

		pos := &contracts.Position{
			PositionID:      fmt.Sprintf("jelly_%d", i),
			UserAddress:     raw.Borrower.Hex(),
			HealthFactor:    hfFloat,
			CollateralValue: raw.CollateralAmount.Uint64(),
			DebtValue:       raw.DebtAmount.Uint64(),
			CollateralToken: raw.CollateralToken.Hex(),
			DebtToken:       raw.DebtToken.Hex(),
		}

		liquidatable = append(liquidatable, pos)
	}

	logger.Info("[Detection] Liquidatable after HF filter",
		"liquidatable", len(liquidatable),
		"totalPositions", len(rawPositions),
		"protocol", a.ProtocolName(),
	)

	return liquidatable, nil
}

// GetLiquidationCalldata returns ABI-encoded calldata for JellyLendingPool.liquidate(borrower).
// This calldata is stored in PriorityQueue and later used by the execution handler.
func (a *LendingAdapter) GetLiquidationCalldata(
	client *evm.Client,
	runtime cre.Runtime,
	position *contracts.Position,
) ([]byte, error) {
	_ = client  // reserved for future use
	_ = runtime // reserved for future use
	borrower := common.HexToAddress(position.UserAddress)
	callData, err := getAllPositionsABI.Pack("liquidate", borrower)
	if err != nil {
		return nil, fmt.Errorf("jellymock: failed to pack liquidate calldata: %w", err)
	}
	return callData, nil
}
