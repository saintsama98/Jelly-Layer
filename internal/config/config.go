package config

import (
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
)

// Config holds all configuration for Jelly Engine.
// This struct is used as the generic type parameter for cre.Workflow[*Config].
type Config struct {
	// Chain configuration
	ChainID       int64
	ChainSelector uint64 // CRE chain selector (not the same as chain ID)
	RPCURL        string

	// Contract addresses (hex strings)
	OracleAddress                  string
	PriorityQueueAddress           string
	ExecutorRegistryAddress        string
	LiquidationOrchestratorAddress string
	OEVDistributorAddress          string
	LendingPoolAddress             string

	// Protocol adapter — determines which lending pool adapter to use.
	// Supported: "jelly_mock", (future: "aave_v3", "morpho_blue", "compound_v3")
	ProtocolType string

	// Workflow parameters
	MaxLiquidationCapacity uint64  // in wei
	MaxPriceImpact         float64 // percentage (0.05 = 5%)
	LiquidationBonus       float64 // percentage (0.05 = 5%)
	OEVProtocolSplit       float64 // percentage (0.40 = 40%)
	OEVExecutorSplit       float64 // percentage (0.50 = 50%)
	OEVValidatorSplit      float64 // percentage (0.10 = 10%)

	// Execution window
	ExecutionDelayBlocks int64
	ExecutionWindowSize  int64
}

// Address bytes helpers — return 20-byte EVM addresses for use in evm.FilterLogTriggerRequest.
func (c *Config) OracleAddressBytes() []byte {
	return common.HexToAddress(c.OracleAddress).Bytes()
}

func (c *Config) PriorityQueueAddressBytes() []byte {
	return common.HexToAddress(c.PriorityQueueAddress).Bytes()
}

func (c *Config) LiquidationOrchestratorAddressBytes() []byte {
	return common.HexToAddress(c.LiquidationOrchestratorAddress).Bytes()
}

func (c *Config) OEVDistributorAddressBytes() []byte {
	return common.HexToAddress(c.OEVDistributorAddress).Bytes()
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() (*Config, error) {
	return &Config{
		ChainID:                        getEnvInt64("CHAIN_ID", 11155111),
		ChainSelector:                  getEnvUint64("CHAIN_SELECTOR", 16015286601757825753), // Sepolia selector
		RPCURL:                         getEnv("RPC_URL", ""),
		OracleAddress:                  getEnv("ORACLE_ADDRESS", ""),
		PriorityQueueAddress:           getEnv("PRIORITY_QUEUE_ADDRESS", ""),
		ExecutorRegistryAddress:        getEnv("EXECUTOR_REGISTRY_ADDRESS", ""),
		LiquidationOrchestratorAddress: getEnv("LIQUIDATION_ORCHESTRATOR_ADDRESS", ""),
		OEVDistributorAddress:          getEnv("OEV_DISTRIBUTOR_ADDRESS", ""),
		LendingPoolAddress:             getEnv("LENDING_POOL_ADDRESS", ""),
		ProtocolType:                   getEnv("PROTOCOL_TYPE", "jelly_mock"),
		MaxLiquidationCapacity:         getEnvUint64("MAX_LIQUIDATION_CAPACITY", 1_000_000_000_000_000_000_000_000),
		MaxPriceImpact:                 getEnvFloat64("MAX_PRICE_IMPACT", 0.05),
		LiquidationBonus:               getEnvFloat64("LIQUIDATION_BONUS", 0.05),
		OEVProtocolSplit:               getEnvFloat64("OEV_PROTOCOL_SPLIT", 0.40),
		OEVExecutorSplit:               getEnvFloat64("OEV_EXECUTOR_SPLIT", 0.50),
		OEVValidatorSplit:              getEnvFloat64("OEV_VALIDATOR_SPLIT", 0.10),
		ExecutionDelayBlocks:           getEnvInt64("EXECUTION_DELAY_BLOCKS", 5),
		ExecutionWindowSize:            getEnvInt64("EXECUTION_WINDOW_SIZE", 10),
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvUint64(key string, defaultValue uint64) uint64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}
