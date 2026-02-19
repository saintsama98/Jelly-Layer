package prioritization

import (
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// MarketAdapter adapts prioritization based on market conditions
type MarketAdapter struct {
	highVolatilityThreshold float64 // 0.7
	lowVolatilityThreshold  float64 // 0.3
}

// NewMarketAdapter creates a new market adapter
func NewMarketAdapter() *MarketAdapter {
	return &MarketAdapter{
		highVolatilityThreshold: 0.7,
		lowVolatilityThreshold:  0.3,
	}
}

// AdaptPrioritization adjusts prioritization strategy based on volatility
func (ma *MarketAdapter) AdaptPrioritization(
	positions []*contracts.ScoredPosition,
	volatility float64,
) []*contracts.ScoredPosition {
	if volatility > ma.highVolatilityThreshold {
		// High volatility: prioritize by health factor (solvency protection)
		return sortByHealthFactor(positions)
	} else if volatility < ma.lowVolatilityThreshold {
		// Low volatility: optimize for OEV
		return sortByOEV(positions)
	}
	// Moderate volatility: use existing scores
	return positions
}

// sortByHealthFactor sorts positions by health factor severity
func sortByHealthFactor(positions []*contracts.ScoredPosition) []*contracts.ScoredPosition {
	// TODO: Implement sorting
	return positions
}

// sortByOEV sorts positions by OEV potential
func sortByOEV(positions []*contracts.ScoredPosition) []*contracts.ScoredPosition {
	// TODO: Implement sorting
	return positions
}

