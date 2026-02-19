# Jelly Layer - CRE-Based OEV Liquidation Orchestration

Jelly Layer is a Chainlink CRE-powered liquidation orchestration system that brings structured, regulated execution to oracle-triggered liquidations while preventing cascading risks and capturing OEV for protocols.

## Architecture

### Single Workflow with Multiple Handlers

- **Handler 1**: Oracle Price Update → Detection
- **Handler 2**: Priorities Submitted → Prioritization  
- **Handler 3**: Queue Updated → Execution Routing
- **Handler 4**: Liquidation Executed → Distribution

### Core Components

- **CRE Workflows**: Go-based workflows using Chainlink CRE SDK
- **Smart Contracts**: Solidity contracts for state management and coordination
- **Capabilities**: EVM read/write, HTTP for external data, volatility calculation

## Project Structure

```
jelly-layer-cre/
├── cmd/
│   └── jelly-engine/          # Main entry point
├── internal/
│   ├── engine/                # Engine orchestrator & init workflow
│   ├── workflows/             # Workflow handlers
│   │   ├── detection/         # Handler 1: OEV Detection
│   │   ├── prioritization/    # Handler 2: Prioritization & Scoring
│   │   ├── execution/         # Handler 3: Execution Routing
│   │   └── distribution/      # Handler 4: OEV Distribution
│   ├── capabilities/          # CRE capability wrappers
│   │   ├── evm/               # EVM read/write
│   │   ├── http/              # HTTP requests
│   │   └── volatility/        # Volatility calculation
│   ├── contracts/             # Contract bindings/interfaces
│   └── config/                # Configuration management
├── contracts/
│   └── src/                   # Solidity contracts
│       ├── PriorityQueue.sol
│       ├── ExecutorRegistry.sol
│       ├── LiquidationOrchestrator.sol
│       ├── OEVDistributor.sol
│       └── JellyLendingPool.sol
├── go.mod
├── project.yaml               # CRE project configuration
└── README.md
```

## Setup

1. **Install Dependencies**
   ```bash
   go mod download
   ```

2. **Configure Environment**
   ```bash
   cp .env.example .env
   # Edit .env with your configuration
   ```

3. **Deploy Contracts**
   ```bash
   cd contracts
   # Deploy contracts using Hardhat/Foundry
   ```

4. **Update Contract Addresses**
   Update `.env` with deployed contract addresses

5. **Run CRE Workflow**
   ```bash
   go run cmd/jelly-engine/main.go
   ```

## Key Features

- **Risk-Based Prioritization**: Multi-factor scoring (HF, OEV, Risk, Time)
- **Anti-Cascading Logic**: Prevents simultaneous liquidations of same collateral
- **Market Adaptation**: Adjusts strategy based on volatility
- **Structured OEV Capture**: 40% protocol, 50% executor, 10% validators
- **Decentralized Consensus**: BFT validation via CRE DON

## Workflow Flow

1. **Oracle Update** → Detection workflow scans positions
2. **Positions Found** → Prioritization workflow scores and ranks
3. **Anti-Cascading** → Filters positions to prevent cascades
4. **Queue Updated** → Execution workflow selects liquidator
5. **Liquidation Executed** → Distribution workflow splits OEV

## License

MIT

