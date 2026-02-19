package adapters

import (
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// OEVCalculator abstracts OEV (Oracle Extractable Value) estimation.
// Different protocols have different liquidation bonus structures and
// OEV capture mechanisms, so the calculation is protocol-specific.
type OEVCalculator interface {
	// CalculateOEVPotential estimates how much OEV can be captured from each
	// liquidatable position. The implementation factors in:
	//   - Liquidation bonus/incentive for the specific protocol
	//   - Price spread between off-chain oracle and on-chain price
	//   - Estimated gas costs
	//   - Protocol-specific fee structures
	CalculateOEVPotential(
		client *evm.Client,
		runtime cre.Runtime,
		positions []*contracts.Position,
		priceUpdate *contracts.PriceUpdate,
	) ([]*contracts.PositionWithOEV, error)
}

