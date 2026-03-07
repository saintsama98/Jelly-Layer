package evm

// EVM writer utilities.
//
// Write(ctx, contractAddress, callData) sends a transaction via the CRE SDK
// WriteReport (receiver = contract, report payload = ABI-encoded calldata).
// Callers pack ABI calldata before calling.
