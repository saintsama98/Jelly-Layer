package distribution

import (
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// DistributionCalculator calculates OEV distribution splits
type DistributionCalculator struct {
	protocolSplit  float64 // 0.40
	executorSplit  float64 // 0.50
	validatorSplit float64 // 0.10
}

// NewDistributionCalculator creates a new calculator
func NewDistributionCalculator(protocolSplit, executorSplit, validatorSplit float64) *DistributionCalculator {
	return &DistributionCalculator{
		protocolSplit:  protocolSplit,
		executorSplit:  executorSplit,
		validatorSplit: validatorSplit,
	}
}

// CalculateDistribution calculates splits for OEV
func (dc *DistributionCalculator) CalculateDistribution(oevCaptured uint64) *contracts.Distribution {
	return &contracts.Distribution{
		TotalOEV:       oevCaptured,
		ProtocolShare:  uint64(float64(oevCaptured) * dc.protocolSplit),
		ExecutorShare:  uint64(float64(oevCaptured) * dc.executorSplit),
		ValidatorShare: uint64(float64(oevCaptured) * dc.validatorSplit),
	}
}

