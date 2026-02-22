package volatility

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"

	httpclient "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/http"
)

// HTTPGetter performs GET requests and returns body and status code.
// Implemented by http.Client; can be mocked in tests.
type HTTPGetter interface {
	GetWithStatus(ctx context.Context, url string) ([]byte, int, error)
}

// Calculator calculates market volatility from historical price data.
type Calculator struct {
	httpGetter HTTPGetter
}

// NewCalculator creates a new volatility calculator using the default HTTP client.
func NewCalculator() *Calculator {
	return &Calculator{
		httpGetter: httpclient.NewClient(),
	}
}

// NewCalculatorWithGetter creates a calculator that uses the given getter (for tests or custom clients).
func NewCalculatorWithGetter(getter HTTPGetter) *Calculator {
	return &Calculator{httpGetter: getter}
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

// coingeckoMarketChart is the response from CoinGecko /coins/{id}/market_chart.
// Prices are [timestamp_ms, price] pairs.
type coingeckoMarketChart struct {
	Prices [][2]float64 `json:"prices"`
}

// symbolToCoinGeckoID maps common token symbols to CoinGecko coin IDs.
// CoinGecko uses lowercase IDs (e.g. "ethereum", "bitcoin").
var symbolToCoinGeckoID = map[string]string{
	"ETH":   "ethereum",
	"WETH":  "ethereum",
	"BTC":   "bitcoin",
	"WBTC":  "wrapped-bitcoin",
	"USDC":  "usd-coin",
	"USDT":  "tether",
	"DAI":   "dai",
	"LINK":  "chainlink",
	"UNI":   "uniswap",
	"AAVE":  "aave",
	"MKR":   "maker",
	"CRV":   "curve-dao-token",
	"SNX":   "havven",
	"COMP":  "compound-governance-token",
	"MATIC": "matic-network",
	"POL":   "matic-network",
	"ARB":   "arbitrum",
	"OP":    "optimism",
	"AVAX":  "avalanche-2",
	"SOL":   "solana",
}

const (
	coingeckoBaseURL = "https://api.coingecko.com/api/v3"
	marketChartDays  = 1
)

// fetchRecentPrices fetches recent prices from CoinGecko market_chart API.
// tokenSymbol is the ticker (e.g. "ETH", "BTC"). Unmapped symbols are lowercased and used as CoinGecko id.
func (c *Calculator) fetchRecentPrices(ctx context.Context, tokenSymbol string) ([]float64, error) {
	coinID := symbolToCoinGeckoID[strings.ToUpper(tokenSymbol)]
	if coinID == "" {
		coinID = url.PathEscape(strings.ToLower(tokenSymbol))
	}
	apiURL := fmt.Sprintf("%s/coins/%s/market_chart?vs_currency=usd&days=%d", coingeckoBaseURL, coinID, marketChartDays)

	body, statusCode, err := c.httpGetter.GetWithStatus(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetch prices: %w", err)
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("fetch prices: API returned status %d", statusCode)
	}

	var chart coingeckoMarketChart
	if err := json.Unmarshal(body, &chart); err != nil {
		return nil, fmt.Errorf("fetch prices: decode response: %w", err)
	}
	if len(chart.Prices) == 0 {
		return nil, nil
	}

	// Sort by timestamp (CoinGecko may not guarantee order)
	sort.Slice(chart.Prices, func(i, j int) bool { return chart.Prices[i][0] < chart.Prices[j][0] })

	prices := make([]float64, len(chart.Prices))
	for i, p := range chart.Prices {
		prices[i] = p[1]
	}
	return prices, nil
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
