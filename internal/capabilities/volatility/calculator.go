package volatility

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	httpclient "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/http"
)

// EnvVolatilityOverridePrefix is the env var prefix for per-token overrides: VOLATILITY_OVERRIDE_ETH=0.42
const EnvVolatilityOverridePrefix = "VOLATILITY_OVERRIDE_"

// EnvVolatilityOverride is the env var for a single override applied to any token when no per-token override is set.
const EnvVolatilityOverride = "VOLATILITY_OVERRIDE"

// HTTPGetter performs GET requests and returns body and status code.
// Implemented by http.Client; can be mocked in tests.
type HTTPGetter interface {
	GetWithStatus(ctx context.Context, url string) ([]byte, int, error)
}

// VolatilityLogger is used to log API calls and results when set. Satisfied by runtime.Logger().
type VolatilityLogger interface {
	Info(msg string, keyvals ...any)
	Warn(msg string, keyvals ...any)
}

// Calculator calculates market volatility from historical price data.
type Calculator struct {
	httpGetter HTTPGetter
	logger     VolatilityLogger // optional; when set, logs API calls and results
}

// NewCalculator creates a new volatility calculator using the default HTTP client.
func NewCalculator() *Calculator {
	return &Calculator{
		httpGetter: httpclient.NewClient(),
	}
}

// NewCalculatorWithLogger creates a calculator that logs volatility API calls (for debugging).
func NewCalculatorWithLogger(logger VolatilityLogger) *Calculator {
	return &Calculator{
		httpGetter: httpclient.NewClient(),
		logger:     logger,
	}
}

// NewCalculatorWithGetter creates a calculator that uses the given getter (for tests or custom clients).
func NewCalculatorWithGetter(getter HTTPGetter) *Calculator {
	return &Calculator{httpGetter: getter}
}

// NewCalculatorWithGetterAndLogger creates a calculator with a custom getter and logger (e.g. for CRE HTTP capability).
func NewCalculatorWithGetterAndLogger(getter HTTPGetter, logger VolatilityLogger) *Calculator {
	return &Calculator{httpGetter: getter, logger: logger}
}

//volatility normalization

// CalculateVolatility calculates a normalized volatility index (0-1) for a token.
// When running in CRE workflow simulate (WASM), outbound HTTP is typically unavailable;
// set VOLATILITY_OVERRIDE_<TOKEN> or VOLATILITY_OVERRIDE to inject a pre-fetched value (e.g. from scripts/fetch-volatility.sh).
func (c *Calculator) CalculateVolatility(ctx context.Context, tokenSymbol string) (float64, error) {
	// Allow override from env so simulate (no network) can use pre-fetched volatility from the host.
	if v := getVolatilityOverride(tokenSymbol); v >= 0 {
		if c.logger != nil {
			c.logger.Info("[Volatility] Using override (no network in simulate)", "token", tokenSymbol, "vol", v)
		}
		return v, nil
	}

	var vols []float64
	hasNonErrorSource := false

	if c.logger != nil {
		c.logger.Info("[Volatility] Fetching", "token", tokenSymbol)
	}

	// CoinGecko source
	if prices, err := c.fetchRecentPricesCoinGecko(ctx, tokenSymbol); err != nil {
		if c.logger != nil {
			c.logger.Warn("[Volatility] CoinGecko failed", "token", tokenSymbol, "error", err)
		}
	} else if prices != nil {
		hasNonErrorSource = true
		if len(prices) >= 2 {
			vol := calculateVolIndexFromPrices(prices)
			vols = append(vols, vol)
			if c.logger != nil {
				c.logger.Info("[Volatility] CoinGecko OK", "token", tokenSymbol, "prices", len(prices), "vol", vol)
			}
		}
	}

	// Binance source
	if prices, err := c.fetchRecentPricesBinance(ctx, tokenSymbol); err != nil {
		if c.logger != nil {
			c.logger.Warn("[Volatility] Binance failed", "token", tokenSymbol, "error", err)
		}
	} else if prices != nil {
		hasNonErrorSource = true
		if len(prices) >= 2 {
			vol := calculateVolIndexFromPrices(prices)
			vols = append(vols, vol)
			if c.logger != nil {
				c.logger.Info("[Volatility] Binance OK", "token", tokenSymbol, "prices", len(prices), "vol", vol)
			}
		}
	}

	// CoinCap source (fallback)
	if prices, err := c.fetchRecentPricesCoinCap(ctx, tokenSymbol); err != nil {
		if c.logger != nil {
			c.logger.Warn("[Volatility] CoinCap failed", "token", tokenSymbol, "error", err)
		}
	} else if prices != nil {
		hasNonErrorSource = true
		if len(prices) >= 2 {
			vol := calculateVolIndexFromPrices(prices)
			vols = append(vols, vol)
			if c.logger != nil {
				c.logger.Info("[Volatility] CoinCap OK", "token", tokenSymbol, "prices", len(prices), "vol", vol)
			}
		}
	}

	if len(vols) > 0 {
		result := aggregateVolatilities(vols)
		if c.logger != nil {
			c.logger.Info("[Volatility] Result", "token", tokenSymbol, "vol", result, "sources", len(vols))
		}
		return result, nil
	}
	if hasNonErrorSource {
		if c.logger != nil {
			c.logger.Info("[Volatility] No enough price data", "token", tokenSymbol, "vol", 0)
		}
		return 0, nil
	}

	if c.logger != nil {
		c.logger.Warn("[Volatility] All sources failed", "token", tokenSymbol)
	}
	return 0, fmt.Errorf("all volatility sources failed")
}

// getVolatilityOverride returns a non-negative value if VOLATILITY_OVERRIDE_<TOKEN> or VOLATILITY_OVERRIDE is set and parseable; otherwise -1.
func getVolatilityOverride(tokenSymbol string) float64 {
	for _, key := range []string{EnvVolatilityOverridePrefix + strings.ToUpper(tokenSymbol), EnvVolatilityOverride} {
		if v := os.Getenv(key); v != "" {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				continue
			}
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			return f
		}
	}
	return -1
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
	coincapBaseURL   = "https://api.coincap.io/v2"

	marketChartDays = 1

	// 5m candles * 288 ≈ 24h
	binanceKlinesLimit = 288
)

// symbolToCoinCapID maps token symbols to CoinCap asset IDs (same as CoinGecko-style ids).
var symbolToCoinCapID = map[string]string{
	"ETH": "ethereum", "WETH": "ethereum", "BTC": "bitcoin", "WBTC": "bitcoin",
	"LINK": "chainlink", "UNI": "uniswap", "AAVE": "aave", "MATIC": "polygon", "POL": "polygon",
	"AVAX": "avalanche", "SOL": "solana", "ARB": "arbitrum", "OP": "optimism",
}

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

// coincapHistoryPoint is one point from CoinCap /assets/{id}/history.
type coincapHistoryPoint struct {
	PriceUsd string `json:"priceUsd"`
	Time     string `json:"time"`
}

// coincapHistoryResponse is the response from CoinCap history API.
type coincapHistoryResponse struct {
	Data []coincapHistoryPoint `json:"data"`
}

// fetchRecentPricesCoinCap fetches recent 5m prices from CoinCap (fallback source).
func (c *Calculator) fetchRecentPricesCoinCap(ctx context.Context, tokenSymbol string) ([]float64, error) {
	assetID := symbolToCoinCapID[strings.ToUpper(tokenSymbol)]
	if assetID == "" {
		return nil, nil
	}
	endMs := time.Now().UnixMilli()
	startMs := endMs - (24 * 60 * 60 * 1000)
	apiURL := fmt.Sprintf("%s/assets/%s/history?interval=m5&start=%d&end=%d", coincapBaseURL, url.PathEscape(assetID), startMs, endMs)

	body, statusCode, err := c.httpGetter.GetWithStatus(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetch coincap: %w", err)
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("fetch coincap: API returned status %d", statusCode)
	}

	var resp coincapHistoryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("fetch coincap: decode: %w", err)
	}
	if len(resp.Data) == 0 {
		return []float64{}, nil
	}

	prices := make([]float64, 0, len(resp.Data))
	for _, p := range resp.Data {
		v, err := strconv.ParseFloat(p.PriceUsd, 64)
		if err != nil {
			continue
		}
		prices = append(prices, v)
	}
	if len(prices) < 2 {
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
