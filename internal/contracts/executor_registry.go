package contracts

// ExecutorRegistry contract interface.
//
// NOTE: The type-safe generated bindings are in:
//   internal/contracts/generated/executor_registry/
//
// This file contains convenience types used across the workflow pipeline.

// ExecutorStats represents executor performance statistics.
type ExecutorStats struct {
	SuccessfulLiquidations uint64
	FailedAttempts         uint64
	ProfitabilityScore     uint64
	TotalOEVCaptured       uint64
}
