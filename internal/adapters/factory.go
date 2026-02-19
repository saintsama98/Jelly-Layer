package adapters

import (
	"fmt"

	"github.com/jelly-layer-cre/jelly-engine/internal/adapters/jellymock"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
)

// NewFromConfig constructs the correct adapter set based on the protocol
// configured in config.ProtocolType. Returns all three interfaces needed
// by the detection handler.
//
// To add a new protocol:
//  1. Create internal/adapters/<protocol>/ with implementations of
//     LendingPoolAdapter, OEVCalculator, and QueueWriter.
//  2. Add a case here.
//  3. Set PROTOCOL_TYPE=<protocol> in the environment.
func NewFromConfig(cfg *config.Config) (LendingPoolAdapter, OEVCalculator, QueueWriter, error) {
	switch cfg.ProtocolType {
	case "jelly_mock":
		lending := jellymock.NewLendingAdapter(cfg.LendingPoolAddress)
		oev := jellymock.NewOEVCalc(cfg.LiquidationBonus)
		queue := jellymock.NewQueueWriter(cfg.PriorityQueueAddress)
		return lending, oev, queue, nil

	// Future protocols:
	// case "aave_v3":
	//     return aavev3.NewLendingAdapter(...), aavev3.NewOEVCalc(...), aavev3.NewQueueWriter(...), nil
	// case "morpho_blue":
	//     return morpho.NewLendingAdapter(...), morpho.NewOEVCalc(...), morpho.NewQueueWriter(...), nil
	// case "compound_v3":
	//     return compound.NewLendingAdapter(...), compound.NewOEVCalc(...), compound.NewQueueWriter(...), nil

	default:
		return nil, nil, nil, fmt.Errorf("unsupported protocol type: %q — "+
			"supported: jelly_mock", cfg.ProtocolType)
	}
}
