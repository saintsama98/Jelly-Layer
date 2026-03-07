#!/usr/bin/env bash
# Run full workflow simulation (Detection -> Prioritization -> Execution -> Distribution)
# for demo. Requires .env in project root and CRE auth. Output is logged for the demo video.

set -e
cd "$(dirname "$0")/.."
set -a
. ./.env 2>/dev/null || true
set +a

OUT="${FULL_DEMO_OUTPUT:-/tmp/jelly-full-demo.log}"
echo "=== Jelly Layer CRE – Full workflow demo ===" | tee "$OUT"
echo "Started at: $(date -Iseconds)" | tee -a "$OUT"
echo "" | tee -a "$OUT"

# Step 1: Detection (cron trigger)
echo ">>> Step 1: Detection (cron trigger, broadcast)" | tee -a "$OUT"
cre workflow simulate ./cmd/jelly-engine -e .env --target sepolia --non-interactive --trigger-index 4 --broadcast 2>&1 | tee -a "$OUT" || true
SUBMIT_TX=$(grep -E 'txHash=0x[0-9a-fA-F]{64}' "$OUT" | tail -1 | grep -oE '0x[0-9a-fA-F]{64}' | head -1)
echo "" | tee -a "$OUT"
echo "Submit tx (PrioritiesSubmitted): $SUBMIT_TX" | tee -a "$OUT"
if [[ -z "$SUBMIT_TX" ]]; then
  echo "ERROR: Could not get submit tx hash. Check Step 1 output." | tee -a "$OUT"
  exit 1
fi

# Pre-fetch volatility for simulate (WASM has no network; workflow uses VOLATILITY_OVERRIDE_* when set)
VOL_TOKEN="${VOLATILITY_DEFAULT_TOKEN:-ETH}"
VOL_VAL=""
if command -v go &>/dev/null; then
  VOL_VAL=$(go run ./cmd/fetch-volatility "$VOL_TOKEN" 2>/dev/null) || true
fi
if [[ -n "$VOL_VAL" ]]; then
  export "VOLATILITY_OVERRIDE_${VOL_TOKEN}=$VOL_VAL"
  echo "Pre-fetched volatility for $VOL_TOKEN: $VOL_VAL (used in Step 2)" | tee -a "$OUT"
else
  echo "Volatility: no override (simulate will use default 0.5 if APIs fail)" | tee -a "$OUT"
fi

# Step 2: Prioritization
echo "" | tee -a "$OUT"
echo ">>> Step 2: Prioritization (broadcast)" | tee -a "$OUT"
cre workflow simulate ./cmd/jelly-engine -e .env --target sepolia --non-interactive --trigger-index 1 --evm-tx-hash "$SUBMIT_TX" --evm-event-index 0 --broadcast 2>&1 | tee -a "$OUT" || true
UPDATE_TX=$(grep -E 'txHash=0x[0-9a-fA-F]{64}' "$OUT" | tail -1 | grep -oE '0x[0-9a-fA-F]{64}' | head -1)
echo "" | tee -a "$OUT"
echo "UpdateQueue tx: $UPDATE_TX" | tee -a "$OUT"
if [[ -z "$UPDATE_TX" ]]; then
  echo "ERROR: Could not get updateQueue tx hash." | tee -a "$OUT"
  exit 1
fi

# Step 3: Execution
echo "" | tee -a "$OUT"
echo ">>> Step 3: Execution (broadcast)" | tee -a "$OUT"
cre workflow simulate ./cmd/jelly-engine -e .env --target sepolia --non-interactive --trigger-index 2 --evm-tx-hash "$UPDATE_TX" --evm-event-index 0 --broadcast 2>&1 | tee -a "$OUT" || true
LIQ_TX=$(grep -E 'evm-tx-hash=0x[0-9a-fA-F]{64}' "$OUT" | tail -1 | grep -oE '0x[0-9a-fA-F]{64}' | head -1)
if [[ -z "$LIQ_TX" ]]; then
  LIQ_TX=$(grep -oE '0x[0-9a-fA-F]{64}' "$OUT" | tail -1)
fi
echo "" | tee -a "$OUT"
echo "Liquidation tx (LiquidationExecuted): $LIQ_TX" | tee -a "$OUT"

# Step 4: Distribution
echo "" | tee -a "$OUT"
echo ">>> Step 4: Distribution (broadcast)" | tee -a "$OUT"
if [[ -n "$LIQ_TX" ]]; then
  cre workflow simulate ./cmd/jelly-engine -e .env --target sepolia --non-interactive --trigger-index 3 --evm-tx-hash "$LIQ_TX" --evm-event-index 0 --broadcast 2>&1 | tee -a "$OUT" || true
else
  echo "Skipping Distribution (no liquidation tx)." | tee -a "$OUT"
fi

echo "" | tee -a "$OUT"
echo "=== Summary (realtime stats from this run) ===" | tee -a "$OUT"
grep -E '\[Detection\] (PositionsFound|OEV potential summary|Done\.)' "$OUT" | tail -5 | tee -a "$OUT"
grep -E '\[Prioritization\] (Mean volatility|Final queue summary|Done)' "$OUT" | tail -5 | tee -a "$OUT"
grep -E '\[Execution\] Done' "$OUT" | tail -1 | tee -a "$OUT"
grep -E '\[Distribution\] (OEV captured \(realtime\)|Done\.)' "$OUT" | tail -2 | tee -a "$OUT"
echo "" | tee -a "$OUT"
echo "=== Demo finished at $(date -Iseconds) ===" | tee -a "$OUT"
echo "Full log: $OUT" | tee -a "$OUT"

# Write all Volatility-related logs to a separate file for the demo
VOL_OUT="${OUT%.log}-volatility.log"
{
  echo "=== Volatility-related logs (from this run) ==="
  echo "Full log: $OUT"
  echo "Extracted at: $(date -Iseconds)"
  echo ""
  grep -E 'Volatility|\[Volatility\]' "$OUT" || true
  echo ""
  echo "=== End of volatility log ==="
} > "$VOL_OUT"
echo "Volatility log: $VOL_OUT" | tee -a "$OUT"
