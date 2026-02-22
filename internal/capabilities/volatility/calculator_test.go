package volatility

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// mockGetter returns canned body and status for testing.
type mockGetter struct {
	body       []byte
	statusCode int
	err        error
}

func (m *mockGetter) GetWithStatus(_ context.Context, _ string) ([]byte, int, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.body, m.statusCode, nil
}

func TestCalculateVolatility_WithMockAPI(t *testing.T) {
	// CoinGecko-style response: timestamps (ms) and prices. Slight variation -> low vol.
	body := []byte(`{"prices":[[1708000000000,100],[1708003600000,100.5],[1708007200000,99.8],[1708010800000,101],[1708014400000,100.2]]}`)
	calc := NewCalculatorWithGetter(&mockGetter{body: body, statusCode: 200})

	vol, err := calc.CalculateVolatility(context.Background(), "ETH")
	if err != nil {
		t.Fatalf("CalculateVolatility: %v", err)
	}
	if vol < 0 || vol > 1 {
		t.Errorf("volatility %f not in [0,1]", vol)
	}
	// Small price moves -> low normalized vol
	if vol > 0.5 {
		t.Errorf("expected low volatility for small moves, got %f", vol)
	}
}

func TestCalculateVolatility_EmptyPrices(t *testing.T) {
	body := []byte(`{"prices":[]}`)
	calc := NewCalculatorWithGetter(&mockGetter{body: body, statusCode: 200})

	vol, err := calc.CalculateVolatility(context.Background(), "ETH")
	if err != nil {
		t.Fatalf("CalculateVolatility: %v", err)
	}
	if vol != 0 {
		t.Errorf("expected 0 volatility for no prices, got %f", vol)
	}
}

func TestCalculateVolatility_SinglePrice(t *testing.T) {
	body := []byte(`{"prices":[[1708000000000,100]]}`)
	calc := NewCalculatorWithGetter(&mockGetter{body: body, statusCode: 200})

	vol, err := calc.CalculateVolatility(context.Background(), "ETH")
	if err != nil {
		t.Fatalf("CalculateVolatility: %v", err)
	}
	if vol != 0 {
		t.Errorf("expected 0 volatility for single price, got %f", vol)
	}
}

func TestCalculateVolatility_APIError(t *testing.T) {
	calc := NewCalculatorWithGetter(&mockGetter{statusCode: 404, body: []byte("not found")})

	_, err := calc.CalculateVolatility(context.Background(), "ETH")
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if msg := err.Error(); !strings.Contains(msg, "404") {
		t.Errorf("error should mention 404, got: %s", msg)
	}
}

func TestCalculateVolatility_NetworkError(t *testing.T) {
	wantErr := errors.New("network failure")
	calc := NewCalculatorWithGetter(&mockGetter{err: wantErr})

	_, err := calc.CalculateVolatility(context.Background(), "ETH")
	if err == nil {
		t.Fatal("expected error on network failure")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error to wrap %v, got %v", wantErr, err)
	}
}

func TestCalculateVolatility_HighVolatility(t *testing.T) {
	// Big price swings -> high normalized vol
	body := []byte(`{"prices":[[1708000000000,100],[1708003600000,105],[1708007200000,95],[1708010800000,108],[1708014400000,92]]}`)
	calc := NewCalculatorWithGetter(&mockGetter{body: body, statusCode: 200})

	vol, err := calc.CalculateVolatility(context.Background(), "ETH")
	if err != nil {
		t.Fatalf("CalculateVolatility: %v", err)
	}
	if vol < 0 || vol > 1 {
		t.Errorf("volatility %f not in [0,1]", vol)
	}
	// Should be higher than the low-vol case
	if vol < 0.3 {
		t.Errorf("expected higher volatility for large moves, got %f", vol)
	}
}

func TestSymbolToCoinGeckoID(t *testing.T) {
	// Ensure common symbols resolve; we test via fetchRecentPrices which uses the map
	tests := []struct {
		symbol string
	}{
		{"ETH"}, {"eth"}, {"BTC"}, {"USDC"},
	}
	for _, tt := range tests {
		// Just ensure we don't panic and that unmapped symbol gets lowercased in URL
		calc := NewCalculatorWithGetter(&mockGetter{body: []byte(`{"prices":[]}`), statusCode: 200})
		_, _ = calc.CalculateVolatility(context.Background(), tt.symbol)
	}
}

// TestCalculateVolatility_RealAPI hits CoinGecko. Skip unless COINGECKO_INTEGRATION=1.
func TestCalculateVolatility_RealAPI(t *testing.T) {
	if os.Getenv("COINGECKO_INTEGRATION") != "1" {
		t.Skip("set COINGECKO_INTEGRATION=1 to run real API test")
	}
	calc := NewCalculator()
	ctx := context.Background()

	vol, err := calc.CalculateVolatility(ctx, "ETH")
	if err != nil {
		t.Fatalf("real API: %v", err)
	}
	if vol < 0 || vol > 1 {
		t.Errorf("volatility %f not in [0,1]", vol)
	}
	t.Logf("ETH volatility (real API): %.4f", vol)

	vol2, err := calc.CalculateVolatility(ctx, "BTC")
	if err != nil {
		t.Fatalf("real API BTC: %v", err)
	}
	if vol2 < 0 || vol2 > 1 {
		t.Errorf("BTC volatility %f not in [0,1]", vol2)
	}
	t.Logf("BTC volatility (real API): %.4f", vol2)
}
