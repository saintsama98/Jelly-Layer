package execution

import (
	"context"

	evmwrap "github.com/jelly-layer-cre/jelly-engine/internal/capabilities/evm"
	"github.com/jelly-layer-cre/jelly-engine/internal/config"
	"github.com/jelly-layer-cre/jelly-engine/internal/contracts"
)

// LiquidatorSelector selects the best liquidator for a position.
type LiquidatorSelector struct {
	strategy string // "STAKER_POOL", "AUCTION", "ROUND_ROBIN"
}

// NewLiquidatorSelector creates a new selector.
func NewLiquidatorSelector(strategy string) *LiquidatorSelector {
	return &LiquidatorSelector{
		strategy: strategy,
	}
}

// SelectExecutor selects an executor based on the configured strategy.
func (ls *LiquidatorSelector) SelectExecutor(
	client *evmwrap.Client,
	position *contracts.ScoredPosition,
	cfg *config.Config,
) (*contracts.Executor, error) {
	switch ls.strategy {
	case "STAKER_POOL":
		return ls.selectFromStakerPool(client, position, cfg)
	case "AUCTION":
		return ls.selectFromAuction(client, position, cfg)
	case "ROUND_ROBIN":
		return ls.selectRoundRobin(client, position, cfg)
	default:
		return ls.selectFromStakerPool(client, position, cfg)
	}
}

// placeholderExecutorAddress is used when ExecutorRegistry is not yet wired.
const placeholderExecutorAddress = "0x0000000000000000000000000000000000000001"

// selectFromStakerPool selects the executor with highest stake × success rate from ExecutorRegistry.
func (ls *LiquidatorSelector) selectFromStakerPool(client *evmwrap.Client, position *contracts.ScoredPosition, cfg *config.Config) (*contracts.Executor, error) {
	ctx := context.Background()
	executors, err := FetchActiveExecutors(ctx, client, cfg)
	if err != nil || len(executors) == 0 {
		return &contracts.Executor{Address: placeholderExecutorAddress, IsActive: true}, nil
	}
	best := SelectBestExecutorByStakeAndSuccess(executors)
	if best != nil {
		return best, nil
	}
	return &contracts.Executor{Address: placeholderExecutorAddress, IsActive: true}, nil
}

// selectFromAuction selects the executor via an auction-style strategy.
// For the initial implementation, the Jelly engine:
//   - uses ExecutorRegistry to get the active executor set,
//   - scores executors based on stake * success rate (as a proxy for bid competitiveness),
//   - records the "winning bid" for observability in the LiquidationAuctionHouse contract.
func (ls *LiquidatorSelector) selectFromAuction(client *evmwrap.Client, position *contracts.ScoredPosition, cfg *config.Config) (*contracts.Executor, error) {
	ctx := context.Background()
	executors, err := FetchActiveExecutors(ctx, client, cfg)
	if err != nil || len(executors) == 0 {
		// Fallback: if no registry or executors, behave like staker pool fallback.
		return &contracts.Executor{Address: placeholderExecutorAddress, IsActive: true}, nil
	}

	// If we have an auction house configured and a concrete numeric position ID,
	// prefer the on-chain best bid for selection.
	var winner *contracts.Executor
	var totalBid uint64

	if cfg != nil && cfg.LiquidationAuctionHouseAddress != "" && position != nil && position.Position != nil {
		posNumeric := positionIDToUint64(position.Position.PositionID)
		if posNumeric != 0 {
			bestAddr, bestBid, err := FetchBestBid(ctx, client, cfg, posNumeric)
			if err == nil && bestAddr != "" && bestBid > 0 {
				// Find matching executor by address.
				for _, e := range executors {
					if e.Address == bestAddr {
						winner = e
						totalBid = bestBid
						break
					}
				}
			}
		}
	}

	// Fallback: no valid on-chain bids, use stake * successRate heuristic.
	if winner == nil {
		winner = SelectBestExecutorByStakeAndSuccess(executors)
		if winner == nil {
			return &contracts.Executor{Address: placeholderExecutorAddress, IsActive: true}, nil
		}

		// Derive a notional bid from OEV potential purely for accounting.
		if position != nil && position.Position != nil {
			oev := position.Position.OEVPotential
			const maxBid = uint64(1e18)
			if oev > maxBid {
				totalBid = maxBid
			} else {
				totalBid = oev
			}
		}
	}

	upfront := totalBid / 2

	// Optionally record the auction outcome on-chain if configured.
	if cfg != nil && cfg.LiquidationAuctionHouseAddress != "" && position != nil && position.Position != nil {
		// Best-effort write; failures should not block execution selection.
		_ = RecordWinningBid(ctx, client, cfg, position.Position.PositionID, winner.Address, totalBid, upfront)
	}

	return winner, nil
}

// selectRoundRobin selects executors in rotation (not implemented; uses staker pool fallback).
func (ls *LiquidatorSelector) selectRoundRobin(client *evmwrap.Client, position *contracts.ScoredPosition, cfg *config.Config) (*contracts.Executor, error) {
	return ls.selectFromStakerPool(client, position, cfg)
}

