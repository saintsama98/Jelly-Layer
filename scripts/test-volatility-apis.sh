#!/usr/bin/env bash
# Test that volatility APIs (CoinGecko, Binance, and optionally CoinCap) are reachable.
# Run from project root. Uses network.

set -e
cd "$(dirname "$0")/.."

echo "=== Testing volatility APIs ==="
echo ""

# 1. CoinGecko (ETH)
echo "1. CoinGecko (ETH)..."
code=$(curl -sS -o /tmp/cg.json -w "%{http_code}" --connect-timeout 10 \
  "https://api.coingecko.com/api/v3/coins/ethereum/market_chart?vs_currency=usd&days=1" 2>/dev/null || echo "000")
if [[ "$code" == "200" ]]; then
  count=$(grep -o '"prices":\[' /tmp/cg.json 2>/dev/null | wc -l)
  echo "   OK (HTTP $code)"
else
  echo "   FAIL (HTTP $code or connection error)"
fi
echo ""

# 2. Binance (ETHUSDT)
echo "2. Binance (ETHUSDT)..."
code=$(curl -sS -o /tmp/bn.json -w "%{http_code}" --connect-timeout 10 \
  "https://api.binance.com/api/v3/klines?symbol=ETHUSDT&interval=5m&limit=10" 2>/dev/null || echo "000")
if [[ "$code" == "200" ]]; then
  echo "   OK (HTTP $code)"
else
  echo "   FAIL (HTTP $code or connection error)"
fi
echo ""

# 3. CoinCap (ETH, optional fallback)
echo "3. CoinCap (ETH)..."
endMs=$(($(date +%s) * 1000))
startMs=$((endMs - 86400000))
code=$(curl -sS -o /tmp/cc.json -w "%{http_code}" --connect-timeout 10 \
  "https://api.coincap.io/v2/assets/ethereum/history?interval=m5&start=$startMs&end=$endMs" 2>/dev/null || echo "000")
if [[ "$code" == "200" ]]; then
  echo "   OK (HTTP $code)"
else
  echo "   SKIP or FAIL (HTTP $code) - optional fallback"
fi
echo ""

# 4. Go integration test (full CalculateVolatility path)
echo "4. Go volatility integration test (CoinGecko + Binance + CoinCap)..."
if COINGECKO_INTEGRATION=1 go test ./internal/capabilities/volatility -run TestCalculateVolatility_RealAPI -v -count=1 2>&1 | tee /tmp/voltest.log; then
  echo "   OK"
else
  echo "   FAIL (see /tmp/voltest.log)"
  exit 1
fi
echo ""
echo "=== All volatility API checks done ==="
