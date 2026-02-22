package prioritization

import (
	"sort"

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

// sortByHealthFactor sorts positions by health factor ascending (most underwater first).
func sortByHealthFactor(positions []*contracts.ScoredPosition) []*contracts.ScoredPosition {
	out := make([]*contracts.ScoredPosition, len(positions))
	copy(out, positions)
	sort.Slice(out, func(i, j int) bool {
		if out[i] == nil || out[i].Position == nil {
			return false
		}
		if out[j] == nil || out[j].Position == nil {
			return true
		}
		return out[i].Position.HealthFactor < out[j].Position.HealthFactor
	})
	return out
}

// sortByOEV sorts positions by OEV potential descending (highest OEV first).
func sortByOEV(positions []*contracts.ScoredPosition) []*contracts.ScoredPosition {
	out := make([]*contracts.ScoredPosition, len(positions))
	copy(out, positions)
	sort.Slice(out, func(i, j int) bool {
		if out[i] == nil || out[i].Position == nil {
			return false
		}
		if out[j] == nil || out[j].Position == nil {
			return true
		}
		return out[i].Position.OEVPotential > out[j].Position.OEVPotential
	})
	return out
}

