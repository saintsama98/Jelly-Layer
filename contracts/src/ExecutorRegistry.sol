// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title ExecutorRegistry
 * @notice Registry of liquidators/executors with reputation tracking
 */
contract ExecutorRegistry {
    struct Executor {
        address executorAddress;
        uint256 successfulLiquidations;
        uint256 failedAttempts;
        uint256 profitabilityScore;     // 0-1000 scale
        bytes32[] supportedCollaterals;
        bool isActive;
        uint64 lastExecutionTime;
        uint256 totalOEVCaptured;
        uint256 stakeAmount;
    }

    mapping(address => Executor) public executorRegistry;
    address[] public activeExecutors;
    
    address public owner;
    address public jellyEngine;

    event ExecutorRegistered(address indexed executor, uint256 timestamp);
    event StatsUpdated(address indexed executor, uint256 score);

    modifier onlyJellyEngine() {
        require(msg.sender == jellyEngine, "Only Jelly Engine");
        _;
    }

    constructor() {
        owner = msg.sender;
    }

    function setJellyEngine(address _jellyEngine) external {
        require(msg.sender == owner, "Only owner");
        jellyEngine = _jellyEngine;
    }

    /**
     * @notice Register executor
     */
    function registerExecutor(
        bytes32[] memory supportedCollaterals,
        uint256 stakeAmount
    ) external {
        require(!executorRegistry[msg.sender].isActive, "Already registered");
        
        executorRegistry[msg.sender] = Executor({
            executorAddress: msg.sender,
            successfulLiquidations: 0,
            failedAttempts: 0,
            profitabilityScore: 500, // Initial score
            supportedCollaterals: supportedCollaterals,
            isActive: true,
            lastExecutionTime: 0,
            totalOEVCaptured: 0,
            stakeAmount: stakeAmount
        });
        
        activeExecutors.push(msg.sender);
        emit ExecutorRegistered(msg.sender, block.timestamp);
    }

    /**
     * @notice Get active executors
     */
    function getActiveExecutors() external view returns (Executor[] memory) {
        Executor[] memory executors = new Executor[](activeExecutors.length);
        uint256 count = 0;
        
        for (uint256 i = 0; i < activeExecutors.length; i++) {
            if (executorRegistry[activeExecutors[i]].isActive) {
                executors[count] = executorRegistry[activeExecutors[i]];
                count++;
            }
        }
        
        // Resize array
        Executor[] memory result = new Executor[](count);
        for (uint256 i = 0; i < count; i++) {
            result[i] = executors[i];
        }
        
        return result;
    }

    /**
     * @notice Get executor score
     */
    function getExecutorScore(address executor) external view returns (uint256) {
        return executorRegistry[executor].profitabilityScore;
    }

    /**
     * @notice Update executor stats (called by CRE workflow)
     */
    function updateExecutorStats(
        address executor,
        uint256 successfulLiquidations,
        uint256 failedAttempts,
        uint256 totalOEVCaptured
    ) external onlyJellyEngine {
        Executor storage exec = executorRegistry[executor];
        exec.successfulLiquidations += successfulLiquidations;
        exec.failedAttempts += failedAttempts;
        exec.totalOEVCaptured += totalOEVCaptured;
        exec.lastExecutionTime = uint64(block.timestamp);
        
        // Recalculate profitability score
        uint256 totalAttempts = exec.successfulLiquidations + exec.failedAttempts;
        if (totalAttempts > 0) {
            uint256 successRate = (exec.successfulLiquidations * 1000) / totalAttempts;
            exec.profitabilityScore = successRate;
        }
        
        emit StatsUpdated(executor, exec.profitabilityScore);
    }
}

