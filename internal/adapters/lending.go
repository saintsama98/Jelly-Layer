package adapters

import (
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// LendingPoolAdapter abstracts any lending protocol (Aave V3, Morpho, Compound V3, etc.)
// for the detection handler. Each protocol implements this interface so that the
// detection workflow remains protocol-agnostic.
//
// Implementations live in internal/adapters/<protocol>/.
type LendingPoolAdapter interface {
	// GetLiquidatablePositions returns all positions that are liquidatable
	// given the new oracle price. Each adapter handles its own:
	//   - How to enumerate users/positions (events, subgraph, on-chain iteration)
	//   - How to calculate health factor (protocol-specific formula)
	//   - What "liquidatable" means for that protocol
	GetLiquidatablePositions(
		client *evm.Client,
		runtime cre.Runtime,
		priceUpdate *contracts.PriceUpdate,
	) ([]*contracts.Position, error)

	// GetLiquidationCalldata returns the protocol-specific ABI-encoded calldata
	// needed to execute a liquidation for a given position. This is stored in
	// PriorityQueue.LiquidationPosition.executionData so that the execution
	// handler (Handler 3) can submit it without knowing the protocol details.
	GetLiquidationCalldata(
		client *evm.Client,
		runtime cre.Runtime,
		position *contracts.Position,
	) ([]byte, error)

	// ProtocolName returns the protocol identifier (e.g. "aave_v3", "morpho_blue",
	// "compound_v3", "jelly_mock"). Used for logging and metrics.
	ProtocolName() string
}
