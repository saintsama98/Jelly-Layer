package evm

// EVM writer utilities.
//
// Helper functions for writing to contracts via the CRE SDK evm.Client.
// Example usage:
//
//     txHash, err := client.Write(ctx, contractAddr, "submitPositions", positions)
