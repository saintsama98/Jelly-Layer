// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title OEVDistributor
 * @notice Captures and distributes OEV from liquidations
 */
contract OEVDistributor {
    struct Distribution {
        uint256 totalOEV;
        uint256 protocolShare;
        uint256 executorShare;
        uint256 validatorShare;
    }

    mapping(uint256 => Distribution) public distributions;
    
    address public owner;
    address public jellyEngine;
    address public protocolTreasury;
    
    uint256 public constant PROTOCOL_SPLIT = 40; // 40%
    uint256 public constant EXECUTOR_SPLIT = 50; // 50%
    uint256 public constant VALIDATOR_SPLIT = 10; // 10%
    uint256 public constant TOTAL_SPLIT = 100;

    event OEVCaptured(uint256 indexed liquidationId, uint256 amount);
    event OEVDistributed(
        uint256 indexed liquidationId,
        uint256 protocolShare,
        uint256 executorShare,
        uint256 validatorShare
    );

    modifier onlyJellyEngine() {
        require(msg.sender == jellyEngine, "Only Jelly Engine");
        _;
    }

    constructor(address _protocolTreasury) {
        owner = msg.sender;
        protocolTreasury = _protocolTreasury;
    }

    function setJellyEngine(address _jellyEngine) external {
        require(msg.sender == owner, "Only owner");
        jellyEngine = _jellyEngine;
    }

    /**
     * @notice Capture OEV from liquidation
     */
    function captureOEV(uint256 liquidationId, uint256 amount) external onlyJellyEngine {
        // Calculate distribution splits
        Distribution memory dist = Distribution({
            totalOEV: amount,
            protocolShare: (amount * PROTOCOL_SPLIT) / TOTAL_SPLIT,
            executorShare: (amount * EXECUTOR_SPLIT) / TOTAL_SPLIT,
            validatorShare: (amount * VALIDATOR_SPLIT) / TOTAL_SPLIT
        });
        
        distributions[liquidationId] = dist;
        
        emit OEVCaptured(liquidationId, amount);
    }

    /**
     * @notice Distribute OEV to recipients
     */
    function distribute(uint256 liquidationId) external onlyJellyEngine {
        Distribution memory dist = distributions[liquidationId];
        require(dist.totalOEV > 0, "No OEV to distribute");
        
        // Transfer to protocol treasury
        // TODO: Implement actual transfer
        // payable(protocolTreasury).transfer(dist.protocolShare);
        
        // Transfer to executor
        // TODO: Get executor address from liquidation result
        // payable(executor).transfer(dist.executorShare);
        
        // Transfer to validators
        // TODO: Distribute to CRE validators
        // distributeToValidators(dist.validatorShare);
        
        emit OEVDistributed(
            liquidationId,
            dist.protocolShare,
            dist.executorShare,
            dist.validatorShare
        );
    }

    /**
     * @notice Get distribution for liquidation
     */
    function getDistribution(uint256 liquidationId) external view returns (Distribution memory) {
        return distributions[liquidationId];
    }
}

