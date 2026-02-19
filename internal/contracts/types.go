package contracts

// This package contains type definitions for contract interactions

// Position represents a lending position
type Position struct {
	PositionID                   string
	UserAddress                  string
	HealthFactor                 float64
	CollateralValue              uint64 // in wei/USD equivalent
	DebtValue                    uint64
	CollateralToken              string
	DebtToken                    string
	TimeSinceUndercollateralized float64 // hours
	ProtocolTVL                  uint64
}

// PositionWithOEV extends Position with OEV data
type PositionWithOEV struct {
	Position
	OEVPotential     uint64
	EstimatedGasCost uint64
}

// ScoredPosition extends PositionWithOEV with priority score
type ScoredPosition struct {
	Position     *PositionWithOEV
	Score        float64
	UrgencyLevel string // "CRITICAL", "HIGH", "MEDIUM", "LOW"
}

// PriceUpdate represents oracle price update
type PriceUpdate struct {
	FeedID    string
	Price     uint64
	Timestamp uint64
	RoundID   uint64
}

// Executor represents a liquidator/executor
type Executor struct {
	Address              string
	StakeAmount          uint64
	SuccessRate          float64
	LastExecutionTime    uint64
	SupportedCollaterals []string
	IsActive             bool
}

// ExecutionWindow represents reserved execution window
type ExecutionWindow struct {
	PositionID string
	Executor   string
	StartBlock int64
	EndBlock   int64
}

// ExecutionPlan represents complete execution plan
type ExecutionPlan struct {
	Position          *ScoredPosition
	Executor          *Executor
	Window            *ExecutionWindow
	LiquidationParams interface{}
	EstimatedGas      uint64
	Deadline          int64
}

// ExecutionResult represents execution outcome
type ExecutionResult struct {
	Success       bool
	LiquidationID uint64
	TxHash        string
	CapturedOEV   uint64
	ExecutionCost uint64
	NetOEV        uint64
}

// LiquidationEvent represents liquidation executed event
type LiquidationEvent struct {
	LiquidationID uint64
	PositionID    string
	Executor      string
	CapturedOEV   uint64
	Timestamp     uint64
}

// Distribution represents OEV distribution
type Distribution struct {
	TotalOEV       uint64
	ProtocolShare  uint64
	ExecutorShare  uint64
	ValidatorShare uint64
}

// MarketState represents current market conditions
type MarketState struct {
	Liquidity              uint64
	Volatility             float64
	MaxLiquidationCapacity uint64
}
