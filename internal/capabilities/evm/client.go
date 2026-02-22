package evm

// This package previously wrapped a fictional CRE EVM capability.
//
// With the real CRE SDK, the EVM client is accessed directly from the runtime:
//
//     evmClientRaw, err := runtime.EVMClient(chainSelector)
//     evmClient := evmClientRaw.(evm.Client)
//
// The evm.Client interface is defined in:
//     github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm
//
// This wrapper package is preserved only to avoid breaking support files
// (coordinator.go, selector.go, monitor.go) until they are fully migrated.

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	sdkevm "github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
)

// Client wraps the CRE SDK evm.Client for convenience methods used by
// the execution sub-modules (coordinator, selector, monitor).
type Client struct {
	inner sdkevm.Client
}

// NewClientFromSDK wraps a CRE SDK evm.Client.
func NewClientFromSDK(inner sdkevm.Client) *Client {
	return &Client{inner: inner}
}

// Read reads from a smart contract (eth_call).
func (c *Client) Read(ctx context.Context, contractAddress string, method string, args ...interface{}) ([]interface{}, error) {
	addr := common.HexToAddress(contractAddress)
	return c.inner.ReadContract(ctx, addr, method, args...)
}

// Write writes to a smart contract (send transaction).
func (c *Client) Write(ctx context.Context, contractAddress string, method string, args ...interface{}) (string, error) {
	addr := common.HexToAddress(contractAddress)
	txHash, err := c.inner.WriteContract(ctx, addr, method, args...)
	if err != nil {
		return "", err
	}
	return txHash.Hex(), nil
}

// GetCurrentBlock gets current block number.
func (c *Client) GetCurrentBlock(ctx context.Context) (int64, error) {
	blockNum, err := c.inner.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	return blockNum.Int64(), nil
}

// ReserveWindow would reserve an execution window on-chain. Intentionally a no-op:
// with DON-verified execution, timing is enforced by the workflow; the contract
// trusts the DON (jellyEngine) and does not need on-chain window reservation.
func (c *Client) ReserveWindow(ctx context.Context, window interface{}) error {
	_ = window
	return nil
}

var (
	ErrInvalidCapability = fmt.Errorf("invalid EVM capability")
	_                    = big.NewInt // suppress unused import
)
