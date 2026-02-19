package jellymock

import (
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

const (
	// estimatedGasCost is a rough estimate for liquidation gas in wei-equivalent.
	// In a production integration this would be fetched from a gas oracle or
	// estimated via eth_estimateGas.
	estimatedGasCost uint64 = 50_000_000_000_000_000 // ~0.05 ETH
)

// OEVCalc is the JellyMock implementation of the generic OEVCalculator interface.
//
// OEV formula (simplified for POC):
//
//	OEV = (collateralValue × liquidationBonus) − estimatedGasCost
//
// The key insight: OEV exists because the CRE workflow sees the oracle price
// update BEFORE the on-chain price is updated. The liquidation bonus on the
// collateral minus execution costs is the extractable value.
type OEVCalc struct {
	LiquidationBonus float64 // e.g. 0.05 = 5%
}

// NewOEVCalc creates a new JellyMock OEV calculator.
func NewOEVCalc(liquidationBonus float64) *OEVCalc {
	return &OEVCalc{
		LiquidationBonus: liquidationBonus,
	}
}

// CalculateOEVPotential estimates OEV for each liquidatable position.
func (c *OEVCalc) CalculateOEVPotential(
	client *evm.Client,
	runtime cre.Runtime,
	positions []*contracts.Position,
	priceUpdate *contracts.PriceUpdate,
) ([]*contracts.PositionWithOEV, error) {
	logger := runtime.Logger()
	_ = client      // reserved for future on-chain reads (e.g. gas price oracle)
	_ = priceUpdate // reserved for more advanced OEV models

	result := make([]*contracts.PositionWithOEV, 0, len(positions))

	for _, pos := range positions {
		// Liquidation bonus = collateral * bonus percentage
		// This is the "reward" a liquidator gets for repaying the debt.
		liquidationReward := uint64(float64(pos.CollateralValue) * c.LiquidationBonus)

		// OEV = liquidation reward - gas cost
		var oevPotential uint64
		if liquidationReward > estimatedGasCost {
			oevPotential = liquidationReward - estimatedGasCost
		} else {
			oevPotential = 0 // not profitable, but still important for protocol safety
		}

		posWithOEV := &contracts.PositionWithOEV{
			Position:         *pos,
			OEVPotential:     oevPotential,
			EstimatedGasCost: estimatedGasCost,
		}

		logger.Debug("OEV calculated (jellymock)",
			"positionId", pos.PositionID,
			"collateral", pos.CollateralValue,
			"reward", liquidationReward,
			"gasCost", estimatedGasCost,
			"oev", oevPotential,
			"protocol", "jelly_mock",
		)

		result = append(result, posWithOEV)
	}

	return result, nil
}
