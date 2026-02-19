package prioritization

import (
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// CascadingAnalyzer checks for cascading liquidation risk.
// If liquidating a position would cause additional positions to become
// undercollateralized (cascading effect), it should be deferred.
type CascadingAnalyzer struct {
	maxPriceImpact    float64
	maxLiquidationCap uint64
}

// NewCascadingAnalyzer creates a new analyzer.
func NewCascadingAnalyzer(maxPriceImpact float64, maxLiquidationCap uint64) *CascadingAnalyzer {
	return &CascadingAnalyzer{
		maxPriceImpact:    maxPriceImpact,
		maxLiquidationCap: maxLiquidationCap,
	}
}

// CheckCascadingRisk checks if liquidating a position would cause cascading liquidations.
func (ca *CascadingAnalyzer) CheckCascadingRisk(
	position *contracts.ScoredPosition,
	executionQueue []*contracts.ScoredPosition,
	marketState *contracts.MarketState,
) (bool, string) {
	// Rule 1: Same collateral token check
	if hasCollateralToken(executionQueue, position.Position.CollateralToken) {
		return true, "same_collateral_token"
	}

	// Rule 2: Price impact calculation
	priceImpact := calculatePriceImpact(position.Position.CollateralValue, marketState.Liquidity)
	if priceImpact > ca.maxPriceImpact {
		return true, "price_impact_too_high"
	}

	// Rule 3: Total volume check
	totalVolume := calculateTotalVolume(executionQueue) + position.Position.CollateralValue
	if totalVolume > ca.maxLiquidationCap {
		return true, "exceeds_market_capacity"
	}

	return false, ""
}

// hasCollateralToken checks if a collateral token already exists in the queue.
func hasCollateralToken(queue []*contracts.ScoredPosition, token string) bool {
	for _, pos := range queue {
		if pos.Position.CollateralToken == token {
			return true
		}
	}
	return false
}

// calculatePriceImpact calculates estimated price impact as a percentage.
func calculatePriceImpact(collateralValue, marketLiquidity uint64) float64 {
	if marketLiquidity == 0 {
		return 1.0 // 100% impact if no liquidity
	}
	return float64(collateralValue) / float64(marketLiquidity)
}

// calculateTotalVolume calculates total liquidation volume in the queue.
func calculateTotalVolume(queue []*contracts.ScoredPosition) uint64 {
	var total uint64
	for _, pos := range queue {
		total += pos.Position.CollateralValue
	}
	return total
}
