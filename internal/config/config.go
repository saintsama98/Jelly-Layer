package config

import (
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
)

// Config holds all configuration for Jelly Engine.
// This struct is used as the generic type parameter for cre.Workflow[*Config].
// JSON tags are used when loading config from CRE workflow config files (e.g. config.sepolia.json).
type Config struct {
	// Chain configuration
	ChainID       int64  `json:"chain_id"`
	ChainSelector uint64 `json:"chain_selector"` // CRE chain selector (not the same as chain ID)
	RPCURL        string `json:"rpc_url"`

	// Contract addresses (hex strings)
	OracleAddress                  string `json:"oracle_address"`
	PriorityQueueAddress           string `json:"priority_queue_address"`
	ExecutorRegistryAddress        string `json:"executor_registry_address"`
	LiquidationOrchestratorAddress string `json:"liquidation_orchestrator_address"`
	OEVDistributorAddress          string `json:"oev_distributor_address"`
	LendingPoolAddress             string `json:"lending_pool_address"`

	// Optional: address of LiquidationAuctionHouse for auction-based executor selection.
	LiquidationAuctionHouseAddress string `json:"liquidation_auction_house_address,omitempty"`

	// Protocol adapter — determines which lending pool adapter to use.
	// Supported: "jelly_mock", (future: "aave_v3", "morpho_blue", "compound_v3")
	ProtocolType string `json:"protocol_type"`

	// Workflow parameters
	MaxLiquidationCapacity uint64  `json:"max_liquidation_capacity"` // in wei
	MaxPriceImpact         float64 `json:"max_price_impact"`          // percentage (0.05 = 5%)
	LiquidationBonus       float64 `json:"liquidation_bonus"`        // percentage (0.05 = 5%)
	// OEV splits are expressed as fractions (0.50 = 50%).
	OEVProtocolSplit  float64 `json:"oev_protocol_split"`  // e.g. 0.50 = 50%
	OEVExecutorSplit  float64 `json:"oev_executor_split"`  // e.g. 0.50 = 50%
	OEVValidatorSplit float64 `json:"oev_validator_split"` // e.g. 0.00 = 0%

	// Execution window
	ExecutionDelayBlocks int64 `json:"execution_delay_blocks"`
	ExecutionWindowSize  int64 `json:"execution_window_size"`

	// ExecutionStrategy controls how executors are selected for positions.
	// Supported: "STAKER_POOL" (default), "AUCTION", "ROUND_ROBIN"
	ExecutionStrategy string `json:"execution_strategy"`

	// AuctionBidWindowBlocks is the number of blocks after startAuction before we read getBestBid.
	// Used when ExecutionStrategy is AUCTION; deadlineBlock = currentBlock + AuctionBidWindowBlocks.
	AuctionBidWindowBlocks int64 `json:"auction_bid_window_blocks"`

	// VolatilityDefaultToken is used when positions have no collateral token (e.g. from queue).
	// If set (e.g. "ETH"), real-time volatility is fetched for this symbol instead of using 0.5.
	// Leave empty to keep legacy behavior (default 0.5).
	VolatilityDefaultToken string `json:"volatility_default_token"`
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
		LiquidationAuctionHouseAddress: getEnv("LIQUIDATION_AUCTION_HOUSE_ADDRESS", ""),
		ProtocolType:                   getEnv("PROTOCOL_TYPE", "jelly_mock"),
		MaxLiquidationCapacity:         getEnvUint64("MAX_LIQUIDATION_CAPACITY", 1_000_000_000_000_000_000), // 1e18 wei, uint64-safe
		MaxPriceImpact:                 getEnvFloat64("MAX_PRICE_IMPACT", 0.05),
		LiquidationBonus:               getEnvFloat64("LIQUIDATION_BONUS", 0.05),
		// Default OEV split: 50% protocol / 50% executor / 0% validator.
		OEVProtocolSplit:  getEnvFloat64("OEV_PROTOCOL_SPLIT", 0.50),
		OEVExecutorSplit:  getEnvFloat64("OEV_EXECUTOR_SPLIT", 0.50),
		OEVValidatorSplit: getEnvFloat64("OEV_VALIDATOR_SPLIT", 0.00),
		ExecutionDelayBlocks:           getEnvInt64("EXECUTION_DELAY_BLOCKS", 5),
		ExecutionWindowSize:            getEnvInt64("EXECUTION_WINDOW_SIZE", 10),
		ExecutionStrategy:              getEnv("EXECUTION_STRATEGY", "STAKER_POOL"),
		AuctionBidWindowBlocks:         getEnvInt64("AUCTION_BID_WINDOW_BLOCKS", 5),
		VolatilityDefaultToken:         getEnv("VOLATILITY_DEFAULT_TOKEN", ""),
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
