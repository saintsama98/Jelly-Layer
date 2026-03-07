// get-oracle-log fetches the latest AnswerUpdated log from the oracle contract on Sepolia
// and prints the transaction hash and event index for use with:
//
//	cre workflow simulate ./cmd/jelly-engine --target sepolia --non-interactive --trigger-index 0 --evm-tx-hash <tx> --evm-event-index <index>
//
// Loads ORACLE_ADDRESS and RPC_URL (or SEPOLIA_RPC_URL) from .env in the project root.
// If the configured RPC fails (e.g. 401), falls back to a public Sepolia RPC.
// Run from project root: go run ./cmd/get-oracle-log
package main

import (
	"bufio"
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jelly-layer-cre/jelly-engine/internal/workflows/detection"
)

const publicSepoliaRPC = "https://ethereum-sepolia-rpc.publicnode.com"

func main() {
	if err := loadEnv(".env"); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warning: .env: %v\n", err)
	}

	rpcURL := getEnv("RPC_URL", "")
	if rpcURL == "" {
		rpcURL = getEnv("SEPOLIA_RPC_URL", publicSepoliaRPC)
	}
	oracleAddr := getEnv("ORACLE_ADDRESS", "")
	if oracleAddr == "" {
		oracleAddr = "0x694AA1769357215DE4FAC081bf1f309aDC325306" // Chainlink ETH/USD Sepolia
	}

	ctx := context.Background()
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to RPC: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	block, err := client.BlockNumber(ctx)
	if err != nil {
		// If .env RPC is invalid (e.g. 401), retry with public Sepolia RPC
		if rpcURL != publicSepoliaRPC && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized") || strings.Contains(err.Error(), "invalid project")) {
			fmt.Fprintf(os.Stderr, "RPC from .env failed (%v), trying public Sepolia RPC...\n", err)
			client.Close()
			client, err = ethclient.DialContext(ctx, publicSepoliaRPC)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to connect to fallback RPC: %v\n", err)
				os.Exit(1)
			}
			defer client.Close()
			block, err = client.BlockNumber(ctx)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get block number: %v\n", err)
			os.Exit(1)
		}
	}

	fromBlock := uint64(0)
	if block > 50000 {
		fromBlock = block - 50000
	}
	addr := common.HexToAddress(oracleAddr)
	topic0 := detection.AnswerUpdatedEventSig

	q := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(block)),
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic0}},
	}

	logs, err := client.FilterLogs(ctx, q)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FilterLogs failed: %v\n", err)
		os.Exit(1)
	}

	if len(logs) == 0 {
		q.FromBlock = big.NewInt(0)
		logs, err = client.FilterLogs(ctx, q)
		if err != nil || len(logs) == 0 {
			fmt.Fprintf(os.Stderr, "No AnswerUpdated logs found for %s (searched to block %d).\n", oracleAddr, block)
			fmt.Fprintf(os.Stderr, "The Sepolia feed may be stale. Use in-repo simulation instead:\n  go test ./internal/engine -run TestSimulateFullPipeline -v\n")
			os.Exit(1)
		}
	}

	last := logs[len(logs)-1]
	fmt.Println("Use these values when the simulator asks for EVM trigger configuration:")
	fmt.Printf("  Transaction hash: %s\n", last.TxHash.Hex())
	fmt.Printf("  Event index:      %d\n", last.Index)
	fmt.Println()
	fmt.Println("Non-interactive example:")
	fmt.Printf("  cre workflow simulate ./cmd/jelly-engine --target sepolia --non-interactive --trigger-index 0 --evm-tx-hash %s --evm-event-index %d\n", last.TxHash.Hex(), last.Index)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		_ = os.Setenv(key, value)
	}
	return s.Err()
}
