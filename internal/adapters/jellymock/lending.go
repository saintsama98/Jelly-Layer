package jellymock

import (
	"fmt"
	"math/big"
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

	// --- Call the contract via CRE EVM client ---
	// We rely on the CRE SDK's CallContract API being available on the client.
	resp, err := (*client).CallContract(runtime, &evm.CallContractRequest{
		ContractAddress: a.PoolAddress.Bytes(),
		CallData:        callData,
	})
	if err != nil {
		return nil, fmt.Errorf("jellymock: failed to call getAllPositions: %w", err)
	}

	// --- ABI-decode the response ---
	results, err := getAllPositionsABI.Unpack("getAllPositions", resp.ReturnData)
	if err != nil {
		return nil, fmt.Errorf("jellymock: failed to unpack getAllPositions: %w", err)
	}

	// The result is a slice of anonymous structs matching the Solidity tuple.
	// go-ethereum ABI decoding returns anonymous struct types, so we assert
	// against the exact struct shape.
	rawPositions, ok := results[0].([]struct {
		Borrower         common.Address `abi:"borrower"`
		CollateralAmount *big.Int       `abi:"collateralAmount"`
		CollateralToken  common.Address `abi:"collateralToken"`
		DebtAmount       *big.Int       `abi:"debtAmount"`
		DebtToken        common.Address `abi:"debtToken"`
		HealthFactor     *big.Int       `abi:"healthFactor"`
		IsActive         bool           `abi:"isActive"`
	})
	if !ok {
		return nil, fmt.Errorf("jellymock: unexpected type from getAllPositions decode")
	}

	logger.Info("Fetched positions from JellyLendingPool",
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

	logger.Info("Filtered liquidatable positions",
		"liquidatable", len(liquidatable),
		"total", len(rawPositions),
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
	_ = client   // reserved for future use
	_ = runtime  // reserved for future use
	borrower := common.HexToAddress(position.UserAddress)
	callData, err := getAllPositionsABI.Pack("liquidate", borrower)
	if err != nil {
		return nil, fmt.Errorf("jellymock: failed to pack liquidate calldata: %w", err)
	}
	return callData, nil
}


