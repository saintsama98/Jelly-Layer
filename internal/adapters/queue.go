package adapters

import (
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// QueueWriter abstracts the priority queue contract for submitting detected
// positions. This allows the detection handler to remain agnostic of the
// specific queue contract implementation.
type QueueWriter interface {
	// SubmitPositions writes the detected liquidatable positions to the
	// on-chain priority queue. The implementation handles ABI encoding and
	// the contract write transaction.
	SubmitPositions(
		client *evm.Client,
		runtime cre.Runtime,
		positions []*contracts.PositionWithOEV,
	) error
}

