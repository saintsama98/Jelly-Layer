package evm

// EVM reader utilities.
//
// Read(ctx, contractAddress, callData) performs eth_call via the CRE SDK
// CallContract and returns the raw reply data. Callers pack ABI calldata
// and unpack the result.
