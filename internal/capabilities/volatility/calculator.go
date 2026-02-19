package volatility

import (
	"context"
	"math"

	httpclient "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/http"
)

// Calculator calculates market volatility from historical price data.
type Calculator struct {
	httpClient *httpclient.Client
}

// NewCalculator creates a new volatility calculator.
func NewCalculator() *Calculator {
	return &Calculator{
		httpClient: httpclient.NewClient(),
	}
}

// CalculateVolatility calculates a normalized volatility index (0-1) for a token.
func (c *Calculator) CalculateVolatility(ctx context.Context, tokenSymbol string) (float64, error) {
	// Fetch recent prices
	prices, err := c.fetchRecentPrices(ctx, tokenSymbol)
	if err != nil {
		return 0, err
	}

	if len(prices) < 2 {
		return 0, nil
	}

	// Calculate log returns
	returns := make([]float64, 0, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		ret := math.Log(prices[i] / prices[i-1])
		returns = append(returns, ret)
	}

	// Calculate standard deviation
	mean := calculateMean(returns)
	variance := calculateVariance(returns, mean)
	sigma := math.Sqrt(variance)

	// Normalize to 0-1 range
	minVol := 0.005 // 0.5%
	maxVol := 0.05  // 5%
	volIndex := (sigma - minVol) / (maxVol - minVol)

	// Clamp to [0, 1]
	if volIndex < 0 {
		volIndex = 0
	}
	if volIndex > 1 {
		volIndex = 1
	}

	return volIndex, nil
}

// fetchRecentPrices fetches recent prices from an external API.
func (c *Calculator) fetchRecentPrices(ctx context.Context, tokenSymbol string) ([]float64, error) {
	// TODO: Implement price fetching via HTTP capability
	// Example: GET https://api.coingecko.com/api/v3/coins/{id}/market_chart?vs_currency=usd&days=1
	_ = ctx
	_ = tokenSymbol
	return []float64{}, nil
}

// calculateMean calculates mean of values.
func calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// calculateVariance calculates sample variance.
func calculateVariance(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sumSqDiff := 0.0
	for _, v := range values {
		diff := v - mean
		sumSqDiff += diff * diff
	}
	return sumSqDiff / float64(len(values)-1)
}
