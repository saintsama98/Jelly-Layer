package prioritization

import (
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// Scorer calculates priority scores for positions
type Scorer struct {
	config *ScoringConfig
}

// ScoringConfig holds scoring parameters
type ScoringConfig struct {
	HFWeight   float64 // 0.40
	OEVWeight  float64 // 0.30
	RiskWeight float64 // 0.20
	TimeWeight float64 // 0.10
}

// NewScorer creates a new scorer instance
func NewScorer() *Scorer {
	return &Scorer{
		config: &ScoringConfig{
			HFWeight:   0.40,
			OEVWeight:  0.30,
			RiskWeight: 0.20,
			TimeWeight: 0.10,
		},
	}
}

// CalculateScore calculates priority score for a position.
func (s *Scorer) CalculateScore(position *contracts.PositionWithOEV, volatility float64) float64 {
	if position == nil {
		return 0
	}
	p := &position.Position
	// Component 1: Health Factor Severity (0-40 points)
	hfSeverity := (1.0 - p.HealthFactor) * 40.0

	// Component 2: OEV Potential (0-30 points)
	oevScore := normalizeOEV(position.OEVPotential) * 30.0

	// Component 3: Systemic Risk (0-20 points)
	systemicRisk := normalizeRisk(p.DebtValue, p.ProtocolTVL) * 20.0

	// Component 4: Time Decay (0-10 points)
	timeDecay := calculateTimeDecay(p.TimeSinceUndercollateralized) * 10.0

	// Market condition adaptation
	var baseScore float64
	if volatility > 0.7 {
		// High volatility: prioritize health factor
		baseScore = hfSeverity*0.8 + oevScore*0.2
	} else if volatility < 0.3 {
		// Low volatility: optimize OEV
		baseScore = hfSeverity*0.2 + oevScore*0.8
	} else {
		// Moderate volatility: balanced
		baseScore = hfSeverity*s.config.HFWeight +
			oevScore*s.config.OEVWeight +
			systemicRisk*s.config.RiskWeight +
			timeDecay*s.config.TimeWeight
	}

	return baseScore
}

// normalizeOEV normalizes OEV potential to 0-1 range (cap at 1.0).
// Uses a fixed scale so typical wei-denominated OEV values map to [0, 1].
func normalizeOEV(oevPotential uint64) float64 {
	const scale = 1e15
	if oevPotential >= scale {
		return 1.0
	}
	if oevPotential == 0 {
		return 0
	}
	return float64(oevPotential) / float64(scale)
}

// normalizeRisk normalizes systemic risk to 0-1 range
func normalizeRisk(debtValue, protocolTVL uint64) float64 {
	if protocolTVL == 0 {
		return 0
	}
	return float64(debtValue) / float64(protocolTVL)
}

// calculateTimeDecay calculates time decay score
func calculateTimeDecay(hoursSinceUndercollateralized float64) float64 {
	maxHours := 4.0
	if hoursSinceUndercollateralized >= maxHours {
		return 0
	}
	return 1.0 - (hoursSinceUndercollateralized / maxHours)
}
