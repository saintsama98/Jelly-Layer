package contracts

// LiquidationOrchestrator contract interface.
//
// NOTE: The type-safe generated bindings are in:
//   internal/contracts/generated/liquidation_orchestrator/
//
// These were created by `cre generate-bindings evm` from LiquidationOrchestrator.sol ABI.
// Use the generated bindings for log triggers and type-safe event decoding.
//
// This file contains convenience types used across the workflow pipeline.

// LiquidationParams represents parameters for executing a liquidation.
type LiquidationParams struct {
	PositionID       string
	Executor         string
	DebtAmount       uint64
	CollateralAmount uint64
}
