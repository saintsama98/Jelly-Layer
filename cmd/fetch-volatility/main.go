// fetch-volatility fetches real-time volatility for a token using the same logic as the
// workflow (CoinGecko, Binance, CoinCap) and prints the value to stdout. Use it to
// pre-fetch volatility when running under CRE workflow simulate, where WASM has no
// outbound network: export VOLATILITY_OVERRIDE_ETH=$(go run ./cmd/fetch-volatility ETH)
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jelly-layer-cre/jelly-engine/internal/capabilities/volatility"
)

func main() {
	token := "ETH"
	if len(os.Args) >= 2 && os.Args[1] != "" {
		token = os.Args[1]
	}
	ctx := context.Background()
	calc := volatility.NewCalculator()
	v, err := calc.CalculateVolatility(ctx, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch-volatility: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(v)
}
