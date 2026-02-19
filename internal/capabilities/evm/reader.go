package evm

// EVM reader utilities.
//
// Helper functions for reading contract state via the CRE SDK evm.Client.
// Example usage:
//
//     results, err := client.Read(ctx, contractAddr, "getUserPositions", user)
