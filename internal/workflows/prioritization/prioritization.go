package prioritization

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/smartcontractkit/chainlink-protos/cre/go/sdk"
	"github.com/smartcontractkit/chainlink-protos/cre/go/values/pb"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	crehttp "github.com/smartcontractkit/cre-sdk-go/capabilities/networking/http"
	"github.com/smartcontractkit/cre-sdk-go/cre"

	"github.com/jelly-layer-cre/jelly-engine/internal/capabilities/volatility"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// PrioritiesSubmittedEventSig is keccak256("PrioritiesSubmitted(uint256,uint256,uint256)").
// Contract: event PrioritiesSubmitted(uint256 indexed queueId, uint256 timestamp, uint256 positionCount)
var PrioritiesSubmittedEventSig = crypto.Keccak256Hash([]byte("PrioritiesSubmitted(uint256,uint256,uint256)"))

// PrioritizationResult is the output type for the prioritization handler.
type PrioritizationResult struct {
	Processed int
	Deferred  int
}

// HandlePrioritization is the callback for Handler 2: Priorities Submitted → Prioritization.
//
// Trigger: PrioritiesSubmitted event from the PriorityQueue contract.
func HandlePrioritization(
	cfg *config.Config,
	runtime cre.Runtime,
	payload *evm.Log,
) (*PrioritizationResult, error) {
	logger := runtime.Logger()

	// Decode PrioritiesSubmitted(uint256 indexed queueId, uint256 timestamp, uint256 positionCount)
	// queueId is in Topics[1]; Data = timestamp (32) + positionCount (32)
	var queueID, count *big.Int
	if len(payload.Topics) >= 2 && len(payload.Topics[1]) >= 32 {
		queueID = new(big.Int).SetBytes(payload.Topics[1][len(payload.Topics[1])-32:])
	}
	if len(payload.Data) >= 64 {
		count = new(big.Int).SetBytes(payload.Data[32:64]) // positionCount
	}

	logger.Info("[Prioritization] Triggered",
		"queueId", queueID,
		"positionCount", count,
		"blockNumber", payload.BlockNumber,
	)

	// --- Create EVM client ---
	evmClient := &evm.Client{ChainSelector: cfg.ChainSelector}

	// --- Read positions from PriorityQueue contract (at log block so we see queue right after submit) ---
	logger.Info("[Prioritization] Reading queue (getPrioritizedQueue)")
	positions, err := readPositionsFromQueue(evmClient, runtime, cfg, payload.BlockNumber, payload.TxHash)
	if err != nil {
		return nil, err
	}
	logger.Info("[Prioritization] Queue positions", "count", len(positions))

	// --- Fetch real-time volatility per collateral token and score positions ---
	logger.Info("[Prioritization] Scoring positions (volatility + cascading filter)")
	ctx := context.Background()
	volCalculator := volatility.NewCalculatorWithLogger(logger)
	defaultToken := cfg.VolatilityDefaultToken
	if defaultToken == "" {
		defaultToken = os.Getenv("VOLATILITY_DEFAULT_TOKEN")
	}
	volByToken := fetchVolatilityByToken(ctx, volCalculator, positionsToPositions(positions), logger, defaultToken, runtime, cfg)

	scorer := NewScorer()
	scoredPositions := make([]*contracts.ScoredPosition, 0, len(positions))
	var volSum float64
	for _, pos := range positions {
		vol := volByToken[pos.CollateralToken]
		volSum += vol
		score := scorer.CalculateScore(pos, vol)
		scoredPositions = append(scoredPositions, &contracts.ScoredPosition{
			Position: pos,
			Score:    score,
		})
		logger.Info("[Prioritization] Position score (realtime)",
			"positionId", pos.PositionID,
			"volatility", vol,
			"score", score,
			"healthFactor", pos.HealthFactor,
			"oevPotential", pos.OEVPotential,
		)
	}

	// Mean volatility across positions for market-adapter strategy
	meanVolatility := 0.5
	if len(positions) > 0 {
		meanVolatility = volSum / float64(len(positions))
	}
	strategyLabel := "moderate"
	if meanVolatility > 0.7 {
		strategyLabel = "high"
	} else if meanVolatility < 0.3 {
		strategyLabel = "low"
	}
	logger.Info("[Prioritization] Mean volatility (realtime)",
		"meanVolatility", meanVolatility,
		"strategy", strategyLabel,
		"positions", len(positions),
	)

	// --- Apply anti-cascading logic ---
	analyzer := NewCascadingAnalyzer(cfg.MaxPriceImpact, cfg.MaxLiquidationCapacity)
	var filteredPositions []*contracts.ScoredPosition
	var deferredPositions []*contracts.ScoredPosition
	marketState := &contracts.MarketState{}
	if marketState.Liquidity == 0 {
		marketState.Liquidity = uint64(1e18) // default when no real market data so single positions are not deferred for price_impact
	}

	for _, pos := range scoredPositions {
		isCascading, reason := analyzer.CheckCascadingRisk(
			pos, filteredPositions, marketState,
		)
		if isCascading {
			logger.Info("[Prioritization] Position deferred (cascading risk)",
				"positionId", pos.Position.PositionID,
				"reason", reason,
			)
			deferredPositions = append(deferredPositions, pos)
		} else {
			filteredPositions = append(filteredPositions, pos)
		}
	}

	// --- Adapt to market conditions ---
	adapter := NewMarketAdapter()
	finalQueue := adapter.AdaptPrioritization(filteredPositions, meanVolatility)

	for i, sp := range finalQueue {
		if sp != nil && sp.Position != nil {
			logger.Info("[Prioritization] Final queue order (realtime)",
				"rank", i+1,
				"positionId", sp.Position.PositionID,
				"score", sp.Score,
			)
		}
	}
	logger.Info("[Prioritization] Final queue summary (realtime)", "totalProcessed", len(finalQueue), "totalDeferred", len(deferredPositions))

	// --- Write updated queue to contract ---
	logger.Info("[Prioritization] Writing updateQueue to contract", "processed", len(finalQueue), "deferred", len(deferredPositions))
	err = updatePriorityQueue(evmClient, runtime, cfg, finalQueue, deferredPositions)
	if err != nil {
		return nil, err
	}

	logger.Info("[Prioritization] Done",
		"processed", len(finalQueue),
		"deferred", len(deferredPositions),
	)

	return &PrioritizationResult{
		Processed: len(finalQueue),
		Deferred:  len(deferredPositions),
	}, nil
}

const defaultVolatility = 0.5

// volatilityLogger is used to log volatility fetch and results; satisfied by runtime.Logger().
type volatilityLogger interface {
	Info(msg string, keyvals ...any)
	Warn(msg string, keyvals ...any)
}

// slogVolatilityLogger adapts *slog.Logger to volatility.VolatilityLogger.
type slogVolatilityLogger struct{ log *slog.Logger }

func (s *slogVolatilityLogger) Info(msg string, keyvals ...any)  { s.log.Info(msg, keyvals...) }
func (s *slogVolatilityLogger) Warn(msg string, keyvals ...any)  { s.log.Warn(msg, keyvals...) }

// sendRequesterGetter implements volatility.HTTPGetter using CRE's HTTP capability (host performs the request).
type sendRequesterGetter struct{ sr *crehttp.SendRequester }

func (g *sendRequesterGetter) GetWithStatus(ctx context.Context, url string) ([]byte, int, error) {
	_ = ctx // CRE SendRequest does not use context; host applies its own timeout
	resp, err := g.sr.SendRequest(&crehttp.Request{Url: url, Method: "GET"}).Await()
	if err != nil {
		return nil, 0, err
	}
	return resp.GetBody(), int(resp.GetStatusCode()), nil
}

// fetchVolatilityWithCREHTTP fetches volatility for one token using the CRE HTTP capability (works in simulate).
func fetchVolatilityWithCREHTTP(ctx context.Context, cfg *config.Config, runtime cre.Runtime, logger *slog.Logger, token string) (float64, error) {
	client := &crehttp.Client{}
	promise := crehttp.SendRequest(cfg, runtime, client,
		func(c *config.Config, log *slog.Logger, sendReq *crehttp.SendRequester) (float64, error) {
			getter := &sendRequesterGetter{sr: sendReq}
			volLog := &slogVolatilityLogger{log: log}
			calc := volatility.NewCalculatorWithGetterAndLogger(getter, volLog)
			return calc.CalculateVolatility(ctx, token)
		},
		cre.ConsensusMedianAggregation[float64](),
	)
	return promise.Await()
}

// positionsToPositions returns the inner Position slice from a slice of PositionWithOEV.
func positionsToPositions(positions []*contracts.PositionWithOEV) []*contracts.Position {
	out := make([]*contracts.Position, len(positions))
	for i, p := range positions {
		if p != nil {
			p2 := p.Position
			out[i] = &p2
		}
	}
	return out
}

// fetchVolatilityByToken returns a map of collateral token symbol -> volatility (0-1).
// When runtime is non-nil, uses CRE HTTP capability (works in simulate); otherwise uses calc with net/http.
// defaultToken: when positions have no collateral token, fetch real-time volatility for this symbol (e.g. "ETH").
func fetchVolatilityByToken(
	ctx context.Context,
	calc *volatility.Calculator,
	positions []*contracts.Position,
	logger volatilityLogger,
	defaultToken string,
	runtime cre.Runtime,
	cfg *config.Config,
) map[string]float64 {
	useCREHTTP := runtime != nil && cfg != nil

	seen := make(map[string]struct{})
	for _, pos := range positions {
		if pos.CollateralToken != "" {
			seen[pos.CollateralToken] = struct{}{}
		}
	}
	out := make(map[string]float64, len(seen)+2)
	out[""] = defaultVolatility

	if len(seen) == 0 {
		if defaultToken != "" {
			logger.Info("[Prioritization] Volatility: no collateral tokens in positions, fetching real-time for default token", "defaultToken", defaultToken)
			var vol float64
			var err error
			if useCREHTTP {
				vol, err = fetchVolatilityWithCREHTTP(ctx, cfg, runtime, runtime.Logger(), defaultToken)
			} else {
				vol, err = calc.CalculateVolatility(ctx, defaultToken)
			}
			if err != nil {
				logger.Warn("[Prioritization] Volatility: default-token fetch failed, using numeric default", "defaultToken", defaultToken, "error", err, "default", defaultVolatility)
			} else {
				out[""] = vol
				source := "CRE HTTP"
				if !useCREHTTP {
					source = "API"
				}
				logger.Info("[Prioritization] Volatility: result (for empty token)", "defaultToken", defaultToken, "vol", vol, "source", source)
			}
		} else {
			logger.Info("[Prioritization] Volatility: no collateral tokens in positions, using default", "default", defaultVolatility)
		}
		return out
	}

	tokens := make([]string, 0, len(seen))
	for t := range seen {
		tokens = append(tokens, t)
	}
	logger.Info("[Prioritization] Volatility: fetching for tokens", "tokens", tokens)
	source := "API"
	if useCREHTTP {
		source = "CRE HTTP"
	}
	for token := range seen {
		var vol float64
		var err error
		if useCREHTTP {
			vol, err = fetchVolatilityWithCREHTTP(ctx, cfg, runtime, runtime.Logger(), token)
		} else {
			vol, err = calc.CalculateVolatility(ctx, token)
		}
		if err != nil {
			logger.Warn("[Prioritization] Volatility: fetch failed, using default", "token", token, "error", err, "default", defaultVolatility)
			out[token] = defaultVolatility
			continue
		}
		out[token] = vol
		logger.Info("[Prioritization] Volatility: result", "token", token, "vol", vol, "source", source)
	}
	return out
}

// decodeQueueFromReflection converts decoded getPrioritizedQueue slice (any struct type) into []*contracts.PositionWithOEV.
// Handles ABI unpack returning a different slice type (e.g. []struct with different layout).
func decodeQueueFromReflection(v interface{}) ([]*contracts.PositionWithOEV, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, fmt.Errorf("getPrioritizedQueue result not a slice (kind=%v)", rv.Kind())
	}
	n := rv.Len()
	out := make([]*contracts.PositionWithOEV, 0, n)
	for i := 0; i < n; i++ {
		elem := rv.Index(i)
		if elem.Kind() == reflect.Interface {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			return nil, fmt.Errorf("queue element %d is not a struct", i)
		}
		var sol solLiquidationPosition
		// Field order in contract: positionId, borrower, healthFactor, collateralValue, debtValue, priorityScore, urgencyLevel, timestamp, executionData (9 fields)
		for j := 0; j < elem.NumField() && j < 9; j++ {
			f := elem.Field(j)
			if !f.CanInterface() {
				continue
			}
			switch j {
			case 0:
				if b, ok := f.Interface().(*big.Int); ok {
					sol.PositionId = b
				}
			case 1:
				if addr, ok := f.Interface().(common.Address); ok {
					sol.Borrower = addr
				}
			case 2:
				if b, ok := f.Interface().(*big.Int); ok {
					sol.HealthFactor = b
				}
			case 3:
				if b, ok := f.Interface().(*big.Int); ok {
					sol.CollateralValue = b
				}
			case 4:
				if b, ok := f.Interface().(*big.Int); ok {
					sol.DebtValue = b
				}
			case 5:
				if b, ok := f.Interface().(*big.Int); ok {
					sol.PriorityScore = b
				}
			case 6:
				if u, ok := f.Interface().(uint8); ok {
					sol.UrgencyLevel = u
				}
			case 7:
				if u, ok := f.Interface().(uint64); ok {
					sol.Timestamp = u
				}
			case 8:
				if b, ok := f.Interface().([]byte); ok {
					sol.ExecutionData = b
				}
			}
		}
		id := uint64(0)
		if sol.PositionId != nil {
			id = sol.PositionId.Uint64()
		}
		out = append(out, solToPositionWithOEV(&sol, id))
	}
	return out, nil
}

// errHistoricalStateUnavailable is returned when the node/simulator does not support historical eth_call.
const errHistoricalStateUnavailable = "historical state"

// readPositionsFromQueue reads positions from PriorityQueue contract via getPrioritizedQueue().
// blockNumber: when non-nil, call is executed at that block first; if the capability returns
// "historical state ... is not available" (e.g. CRE simulate), we retry at latest.
// When queue at latest is empty, txHash (log's TxHash) can be used to fetch the submit tx and decode positions from calldata.
func readPositionsFromQueue(client *evm.Client, runtime cre.Runtime, cfg *config.Config, blockNumber *pb.BigInt, txHash []byte) ([]*contracts.PositionWithOEV, error) {
	if client == nil {
		return []*contracts.PositionWithOEV{}, nil
	}
	queueAddr := common.HexToAddress(cfg.PriorityQueueAddress)
	callData, err := priorityQueueABI.Pack("getPrioritizedQueue")
	if err != nil {
		return nil, fmt.Errorf("pack getPrioritizedQueue: %w", err)
	}

	tryBlock := blockNumber
	for attempt := 0; attempt < 2; attempt++ {
		req := &evm.CallContractRequest{
			Call:        &evm.CallMsg{To: queueAddr.Bytes(), Data: callData},
			BlockNumber: tryBlock,
		}
		if tryBlock != nil {
			runtime.Logger().Info("[Prioritization] getPrioritizedQueue at log block")
		} else if attempt == 1 {
			runtime.Logger().Info("[Prioritization] getPrioritizedQueue fallback at latest")
		}
		reply, err := client.CallContract(runtime, req).Await()
		if err != nil {
			if tryBlock != nil && strings.Contains(err.Error(), errHistoricalStateUnavailable) && strings.Contains(err.Error(), "not available") {
				runtime.Logger().Warn("historical state not available, retrying at latest", "error", err.Error())
				tryBlock = nil
				continue
			}
			return nil, fmt.Errorf("call getPrioritizedQueue: %w", err)
		}
		if reply == nil {
			return nil, fmt.Errorf("nil CallContractReply from getPrioritizedQueue")
		}
		runtime.Logger().Info("[Prioritization] getPrioritizedQueue response", "dataLen", len(reply.Data))
		results, err := priorityQueueABI.Unpack("getPrioritizedQueue", reply.Data)
		if err != nil {
			return nil, fmt.Errorf("unpack getPrioritizedQueue: %w", err)
		}
		rawSlice, ok := results[0].([]struct {
			PositionId      *big.Int       `abi:"positionId"`
			Borrower        common.Address `abi:"borrower"`
			HealthFactor    *big.Int       `abi:"healthFactor"`
			CollateralValue *big.Int       `abi:"collateralValue"`
			DebtValue       *big.Int       `abi:"debtValue"`
			PriorityScore   *big.Int       `abi:"priorityScore"`
			UrgencyLevel    uint8          `abi:"urgencyLevel"`
			Timestamp       uint64         `abi:"timestamp"`
			ExecutionData   []byte         `abi:"executionData"`
		})
		if !ok {
			// Fallback: decode via reflection (handles different slice/struct types from ABI)
			reflected, err := decodeQueueFromReflection(results[0])
			if err != nil {
				if v := reflect.ValueOf(results[0]); v.Kind() == reflect.Slice && v.Len() == 0 {
					return tryPositionsFromSubmitTx(client, runtime, txHash, []*contracts.PositionWithOEV{})
				}
				return nil, fmt.Errorf("getPrioritizedQueue decode: %w", err)
			}
			runtime.Logger().Info("[Prioritization] getPrioritizedQueue decoded", "positions", len(reflected))
			return tryPositionsFromSubmitTx(client, runtime, txHash, reflected)
		}
		out := make([]*contracts.PositionWithOEV, 0, len(rawSlice))
		for i := range rawSlice {
			sol := &solLiquidationPosition{
				PositionId:      rawSlice[i].PositionId,
				Borrower:        rawSlice[i].Borrower,
				HealthFactor:    rawSlice[i].HealthFactor,
				CollateralValue: rawSlice[i].CollateralValue,
				DebtValue:       rawSlice[i].DebtValue,
				PriorityScore:   rawSlice[i].PriorityScore,
				UrgencyLevel:    rawSlice[i].UrgencyLevel,
				Timestamp:       rawSlice[i].Timestamp,
				ExecutionData:   rawSlice[i].ExecutionData,
			}
			id := uint64(0)
			if rawSlice[i].PositionId != nil {
				id = rawSlice[i].PositionId.Uint64()
			}
			out = append(out, solToPositionWithOEV(sol, id))
		}
		runtime.Logger().Info("[Prioritization] getPrioritizedQueue decoded", "positions", len(out))
		return tryPositionsFromSubmitTx(client, runtime, txHash, out)
	}
	return nil, fmt.Errorf("call getPrioritizedQueue: exhausted retries")
}

// tryPositionsFromSubmitTx: if fromContract is empty and txHash is set, fetch the submit tx and decode positions from calldata; otherwise return fromContract.
func tryPositionsFromSubmitTx(client *evm.Client, runtime cre.Runtime, txHash []byte, fromContract []*contracts.PositionWithOEV) ([]*contracts.PositionWithOEV, error) {
	if len(fromContract) > 0 || len(txHash) < 32 {
		return fromContract, nil
	}
	req := &evm.GetTransactionByHashRequest{Hash: txHash}
	if len(txHash) > 32 {
		req.Hash = txHash[len(txHash)-32:]
	}
	reply, err := client.GetTransactionByHash(runtime, req).Await()
	if err != nil || reply == nil || reply.Transaction == nil {
		return fromContract, nil
	}
	tx := reply.Transaction
	runtime.Logger().Info("[Prioritization] Submit tx fetched (GetTransactionByHash)", "dataLen", len(tx.Data))
	if len(tx.Data) < 4 {
		return fromContract, nil
	}
	positions, err := DecodePositionsFromSubmitTxData(tx.Data)
	if err != nil {
		runtime.Logger().Warn("decode positions from submit tx failed, using contract result", "error", err)
		return fromContract, nil
	}
	if len(positions) > 0 {
		runtime.Logger().Info("[Prioritization] Queue empty at latest; using positions from submit tx", "positions", len(positions))
		return positions, nil
	}
	return fromContract, nil
}

// updatePriorityQueue writes the reordered queue back to the contract via updateQueue(prioritizedIds, deferredIds).
func updatePriorityQueue(
	client *evm.Client,
	runtime cre.Runtime,
	cfg *config.Config,
	queue []*contracts.ScoredPosition,
	deferred []*contracts.ScoredPosition,
) error {
	if client == nil {
		return nil
	}
	prioritizedIds := make([]*big.Int, 0, len(queue))
	for _, sp := range queue {
		if sp != nil && sp.Position != nil {
			prioritizedIds = append(prioritizedIds, big.NewInt(int64(positionIDFromString(sp.Position.PositionID))))
		}
	}
	deferredIds := make([]*big.Int, 0, len(deferred))
	for _, sp := range deferred {
		if sp != nil && sp.Position != nil {
			deferredIds = append(deferredIds, big.NewInt(int64(positionIDFromString(sp.Position.PositionID))))
		}
	}
	queueAddr := common.HexToAddress(cfg.PriorityQueueAddress)
	callData, err := priorityQueueABI.Pack("updateQueue", prioritizedIds, deferredIds)
	if err != nil {
		return fmt.Errorf("pack updateQueue: %w", err)
	}
	report, err := cre.X_GeneratedCodeOnly_WrapReport(&sdk.ReportResponse{RawReport: callData})
	if err != nil {
		return fmt.Errorf("wrap report for updateQueue: %w", err)
	}
	writeReply, err := client.WriteReport(runtime, &evm.WriteCreReportRequest{
		Receiver:  queueAddr.Bytes(),
		Report:    report,
		GasConfig: nil,
	}).Await()
	if err != nil {
		return fmt.Errorf("call updateQueue: %w", err)
	}
	if writeReply != nil && len(writeReply.TxHash) >= 32 {
		txHashHex := common.BytesToHash(writeReply.TxHash).Hex()
		runtime.Logger().Info("[Prioritization] updateQueue tx broadcast", "txHash", txHashHex)
		runtime.Logger().Info("[Prioritization] Next: run Execution with --evm-tx-hash " + txHashHex)
	}
	return nil
}

// ReadPrioritizedQueueAsScored reads the queue via getPrioritizedQueue and returns ScoredPosition slice
// (for Handler 3 execution). Uses priorityScore and urgencyLevel from the contract.
func ReadPrioritizedQueueAsScored(client *evm.Client, runtime cre.Runtime, cfg *config.Config) ([]*contracts.ScoredPosition, error) {
	positions, err := readPositionsFromQueue(client, runtime, cfg, nil, nil) // latest, no tx hash
	if err != nil {
		return nil, err
	}
	return positionsToScored(positions), nil
}

// QueueFromUpdateQueueTx builds the prioritized queue from an updateQueue tx when getPrioritizedQueue returns empty.
// Decodes updateQueue(prioritizedIds, deferredIds) from tx calldata, then calls getPosition(id) for each prioritizedId.
func QueueFromUpdateQueueTx(client *evm.Client, runtime cre.Runtime, cfg *config.Config, txHash []byte) ([]*contracts.ScoredPosition, error) {
	if client == nil || len(txHash) < 32 {
		return nil, nil
	}
	hash := txHash
	if len(hash) > 32 {
		hash = hash[len(hash)-32:]
	}
	req := &evm.GetTransactionByHashRequest{Hash: hash}
	reply, err := client.GetTransactionByHash(runtime, req).Await()
	if err != nil || reply == nil || reply.Transaction == nil {
		runtime.Logger().Warn("QueueFromUpdateQueueTx: get tx failed or nil", "error", err)
		return nil, nil
	}
	if len(reply.Transaction.Data) < 4 {
		runtime.Logger().Warn("QueueFromUpdateQueueTx: tx data too short")
		return nil, nil
	}
	ids, err := DecodeUpdateQueueCalldata(reply.Transaction.Data)
	if err != nil {
		runtime.Logger().Warn("QueueFromUpdateQueueTx: decode updateQueue calldata failed", "error", err)
		return nil, nil
	}
	if len(ids) == 0 {
		return nil, nil
	}
	runtime.Logger().Info("[Prioritization] Queue from updateQueue tx", "prioritizedIds", len(ids))
	queueAddr := common.HexToAddress(cfg.PriorityQueueAddress)
	var out []*contracts.ScoredPosition
	for _, id := range ids {
		callData, err := priorityQueueABI.Pack("getPosition", id)
		if err != nil {
			continue
		}
		req := &evm.CallContractRequest{
			Call:        &evm.CallMsg{To: queueAddr.Bytes(), Data: callData},
			BlockNumber: nil,
		}
		reply, err := client.CallContract(runtime, req).Await()
		if err != nil || reply == nil || len(reply.Data) == 0 {
			continue
		}
		sol, err := DecodeGetPositionReply(reply.Data)
		if err != nil {
			continue
		}
		posId := uint64(0)
		if sol.PositionId != nil {
			posId = sol.PositionId.Uint64()
		}
		p := solToPositionWithOEV(sol, posId)
		out = append(out, &contracts.ScoredPosition{Position: p, Score: 0.5, UrgencyLevel: UrgencyLevelToString(sol.UrgencyLevel)})
	}
	return out, nil
}

func positionsToScored(positions []*contracts.PositionWithOEV) []*contracts.ScoredPosition {
	out := make([]*contracts.ScoredPosition, 0, len(positions))
	for _, p := range positions {
		if p == nil {
			continue
		}
		out = append(out, &contracts.ScoredPosition{
			Position:     p,
			Score:        0.5,
			UrgencyLevel: "MEDIUM",
		})
	}
	return out
}
