package evm

// This package wraps the CRE SDK EVM capability (cre-sdk-go/capabilities/blockchain/evm)
// to provide Read (eth_call), Write (WriteReport with calldata), and GetCurrentBlock
// used by execution, distribution, and monitoring. The SDK uses Promise-based APIs
// (CallContract, HeaderByNumber, WriteReport) and request/response types (CallMsg,
// CallContractReply.Data, WriteCreReportRequest).

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/smartcontractkit/chainlink-protos/cre/go/sdk"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
	"github.com/smartcontractkit/cre-sdk-go/cre"
)

// Client wraps the CRE SDK *evm.Client and holds a cre.Runtime so that
// Read/Write/GetCurrentBlock can call CallContract, WriteReport, and HeaderByNumber.
type Client struct {
	inner   *evm.Client
	runtime cre.Runtime
}

// NewClientFromSDK wraps a CRE SDK *evm.Client and runtime for use by execution/distribution.
func NewClientFromSDK(inner *evm.Client, runtime cre.Runtime) *Client {
	return &Client{inner: inner, runtime: runtime}
}

// Read performs an eth_call: builds CallContractRequest with CallMsg{To, Data},
// calls inner.CallContract(runtime, req).Await(), returns reply.Data.
func (c *Client) Read(ctx context.Context, contractAddress string, callData []byte) ([]byte, error) {
	_ = ctx // CRE uses runtime, not ctx, for capability calls
	if c.inner == nil || c.runtime == nil {
		return nil, fmt.Errorf("evm client: nil inner or runtime")
	}
	addr := common.HexToAddress(contractAddress)
	req := &evm.CallContractRequest{
		Call: &evm.CallMsg{
			To:   addr.Bytes(),
			Data: callData,
		},
		BlockNumber: nil, // latest
	}
	reply, err := c.inner.CallContract(c.runtime, req).Await()
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("evm client: nil CallContractReply")
	}
	return reply.Data, nil
}

// Write sends a transaction via the CRE EVM WriteReport API: receiver = contract address,
// report payload = ABI-encoded calldata. Returns tx hash as hex string.
func (c *Client) Write(ctx context.Context, contractAddress string, callData []byte) (string, error) {
	_ = ctx
	if c.inner == nil || c.runtime == nil {
		return "", fmt.Errorf("evm client: nil inner or runtime")
	}
	addr := common.HexToAddress(contractAddress)
	report, err := cre.X_GeneratedCodeOnly_WrapReport(&sdk.ReportResponse{
		RawReport: callData,
	})
	if err != nil {
		return "", fmt.Errorf("evm client: wrap report: %w", err)
	}
	req := &evm.WriteCreReportRequest{
		Receiver:  addr.Bytes(),
		Report:    report,
		GasConfig: nil,
	}
	reply, err := c.inner.WriteReport(c.runtime, req).Await()
	if err != nil {
		return "", err
	}
	if reply == nil {
		return "", fmt.Errorf("evm client: nil WriteReportReply")
	}
	if len(reply.TxHash) > 0 {
		return common.BytesToHash(reply.TxHash).Hex(), nil
	}
	return "", nil
}

// GetCurrentBlock returns the latest block number via HeaderByNumber(runtime, nil).Await().
func (c *Client) GetCurrentBlock(ctx context.Context) (int64, error) {
	_ = ctx
	if c.inner == nil || c.runtime == nil {
		return 0, fmt.Errorf("evm client: nil inner or runtime")
	}
	req := &evm.HeaderByNumberRequest{BlockNumber: nil} // latest
	reply, err := c.inner.HeaderByNumber(c.runtime, req).Await()
	if err != nil {
		return 0, err
	}
	if reply == nil || reply.Header == nil || reply.Header.BlockNumber == nil {
		return 0, fmt.Errorf("evm client: nil header or block number")
	}
	// pb.BigInt has GetAbsVal() and GetSign()
	bn := reply.Header.BlockNumber
	n := new(big.Int).SetBytes(bn.GetAbsVal())
	if bn.GetSign() < 0 {
		n.Neg(n)
	}
	if !n.IsInt64() {
		return 0, fmt.Errorf("evm client: block number overflow")
	}
	return n.Int64(), nil
}

// ReserveWindow is a no-op: DON-verified execution does not require on-chain window reservation.
func (c *Client) ReserveWindow(ctx context.Context, window interface{}) error {
	_ = window
	return nil
}

var (
	ErrInvalidCapability = fmt.Errorf("invalid EVM capability")
	_                    = big.NewInt
)
