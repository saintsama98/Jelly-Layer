package volatility

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
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

//volatility normalization

// CalculateVolatility calculates a normalized volatility index (0-1) for a token.
func (c *Calculator) CalculateVolatility(ctx context.Context, tokenSymbol string) (float64, error) {
	var vols []float64
	hasNonErrorSource := false

	// CoinGecko source
	if prices, err := c.fetchRecentPricesCoinGecko(ctx, tokenSymbol); err != nil {
		// If all sources fail with errors, we return an error below.
	} else if prices != nil {
		hasNonErrorSource = true
		if len(prices) >= 2 {
			vols = append(vols, calculateVolIndexFromPrices(prices))
		}
	}

	// Binance source
	if prices, err := c.fetchRecentPricesBinance(ctx, tokenSymbol); err != nil {
		// Ignore individual source failures; other sources may still succeed.
	} else if prices != nil {
		hasNonErrorSource = true
		if len(prices) >= 2 {
			vols = append(vols, calculateVolIndexFromPrices(prices))
		}
	}

	if len(vols) > 0 {
		return aggregateVolatilities(vols), nil
	}
	if hasNonErrorSource {
		// At least one source responded but did not have enough data.
		// Preserve existing behavior: treat as "no volatility data" -> 0.
		return 0, nil
	}

	// All sources failed with errors.
	return 0, fmt.Errorf("all volatility sources failed")
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

// symbolToBinanceSymbol maps common token symbols to Binance trading pairs against USDT.
var symbolToBinanceSymbol = map[string]string{
	"ETH":   "ETHUSDT",
	"WETH":  "ETHUSDT",
	"BTC":   "BTCUSDT",
	"WBTC":  "BTCUSDT",
	"USDC":  "USDCUSDT",
	"USDT":  "USDTUSDT",
	"DAI":   "DAIUSDT",
	"LINK":  "LINKUSDT",
	"UNI":   "UNIUSDT",
	"AAVE":  "AAVEUSDT",
	"MKR":   "MKRUSDT",
	"CRV":   "CRVUSDT",
	"SNX":   "SNXUSDT",
	"COMP":  "COMPUSDT",
	"MATIC": "MATICUSDT",
	"POL":   "MATICUSDT",
	"ARB":   "ARBUSDT",
	"OP":    "OPUSDT",
	"AVAX":  "AVAXUSDT",
	"SOL":   "SOLUSDT",
}

const (
	coingeckoBaseURL = "https://api.coingecko.com/api/v3"
	binanceBaseURL   = "https://api.binance.com"

	marketChartDays = 1

	// 5m candles * 288 ≈ 24h
	binanceKlinesLimit = 288
)

// fetchRecentPricesCoinGecko fetches recent prices from CoinGecko market_chart API.
// tokenSymbol is the ticker (e.g. "ETH", "BTC"). Unmapped symbols are lowercased and used as CoinGecko id.
func (c *Calculator) fetchRecentPricesCoinGecko(ctx context.Context, tokenSymbol string) ([]float64, error) {
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
		return []float64{}, nil
	}

	// Sort by timestamp (CoinGecko may not guarantee order)
	sort.Slice(chart.Prices, func(i, j int) bool { return chart.Prices[i][0] < chart.Prices[j][0] })

	prices := make([]float64, len(chart.Prices))
	for i, p := range chart.Prices {
		prices[i] = p[1]
	}
	return prices, nil
}

// fetchRecentPricesBinance fetches recent close prices from Binance klines API.
// If the symbol is not mapped to a Binance pair, it returns (nil, nil) so callers can ignore this source.
func (c *Calculator) fetchRecentPricesBinance(ctx context.Context, tokenSymbol string) ([]float64, error) {
	symbol := symbolToBinanceSymbol[strings.ToUpper(tokenSymbol)]
	if symbol == "" {
		return nil, nil
	}

	apiURL := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=5m&limit=%d", binanceBaseURL, symbol, binanceKlinesLimit)

	body, statusCode, err := c.httpGetter.GetWithStatus(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetch binance prices: %w", err)
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("fetch binance prices: API returned status %d", statusCode)
	}

	var raw [][]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("fetch binance prices: decode response: %w", err)
	}
	if len(raw) == 0 {
		return []float64{}, nil
	}

	prices := make([]float64, 0, len(raw))
	for _, k := range raw {
		if len(k) < 5 {
			continue
		}
		closeStr, ok := k[4].(string)
		if !ok {
			continue
		}
		p, err := strconv.ParseFloat(closeStr, 64)
		if err != nil {
			continue
		}
		prices = append(prices, p)
	}
	if len(prices) == 0 {
		return []float64{}, nil
	}
	return prices, nil
}

// calculateVolIndexFromPrices converts a price series into a normalized volatility index.
func calculateVolIndexFromPrices(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}

	returns := make([]float64, 0, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		ret := math.Log(prices[i] / prices[i-1])
		returns = append(returns, ret)
	}

	mean := calculateMean(returns)
	variance := calculateVariance(returns, mean)
	sigma := math.Sqrt(variance)

	minVol := 0.005 // 0.5%
	maxVol := 0.05  // 5%
	volIndex := (sigma - minVol) / (maxVol - minVol)

	if volIndex < 0 {
		volIndex = 0
	}
	if volIndex > 1 {
		volIndex = 1
	}
	return volIndex
}

// aggregateVolatilities aggregates multiple volatility indices into a single value.
// Uses median aggregation for robustness to outliers.
func aggregateVolatilities(vols []float64) float64 {
	if len(vols) == 0 {
		return 0
	}
	sorted := make([]float64, len(vols))
	copy(sorted, vols)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
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
