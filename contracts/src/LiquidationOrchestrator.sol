// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

interface ILendingPoolJellyMock {
    function liquidate(address borrower) external;
}

/**
 * @title LiquidationOrchestrator
 * @notice Orchestrates liquidation execution with CRE coordination
 */
contract LiquidationOrchestrator {
    struct LiquidationParams {
        uint256 positionId;
        address executor;
        uint256 debtAmount;
        uint256 collateralAmount;
        address borrower;
        // Planner-provided OEV estimate (fallback notion, not realized value).
        // For JellyMock this maps to PositionWithOEV.OEVPotential.
        uint256 oevPotential;
        // Optional: the gas cost estimate used by the planner (for transparency).
        uint256 estimatedGasCost;
    }

    struct LiquidationResult {
        bool executed;
        uint256 capturedOEV;
        uint256 gasUsed;
        bytes32 txHash;
    }

    mapping(uint256 => LiquidationResult) public liquidationResults;
    
    address public owner;
    address public jellyEngine;
    address public lendingPool;
    address public oevDistributor;

    event LiquidationExecuted(
        uint256 indexed positionId,
        address indexed executor,
        uint256 capturedOEV,
        bytes32 txHash
    );
    
    event LiquidationFailed(
        uint256 indexed positionId,
        address indexed executor,
        string reason
    );

    modifier onlyJellyEngine() {
        require(msg.sender == jellyEngine, "Only Jelly Engine");
        _;
    }

    constructor(address _lendingPool) {
        owner = msg.sender;
        lendingPool = _lendingPool;
    }

    function setJellyEngine(address _jellyEngine) external {
        require(msg.sender == owner, "Only owner");
        jellyEngine = _jellyEngine;
    }

    function setOEVDistributor(address _oevDistributor) external {
        require(msg.sender == owner, "Only owner");
        oevDistributor = _oevDistributor;
    }

    /**
     * @notice Execute liquidation (called by CRE Execution workflow)
     */
    function executeLiquidation(LiquidationParams memory params) external onlyJellyEngine returns (LiquidationResult memory) {
        require(params.borrower != address(0), "Invalid borrower");

        // Validate liquidation
        require(validateLiquidation(params.positionId), "Invalid liquidation");
        
        // Call underlying lending pool's liquidate function for the borrower.
        ILendingPoolJellyMock(lendingPool).liquidate(params.borrower);
 
        // Use planner-provided OEV notion as a fallback estimate.
        uint256 capturedOEV = calculateOEVCaptured(params);
        
        LiquidationResult memory result = LiquidationResult({
            executed: true,
            capturedOEV: capturedOEV,
            gasUsed: gasleft(), // Placeholder
            txHash: bytes32(0) // Will be set by transaction
        });
        
        liquidationResults[params.positionId] = result;
        
        emit LiquidationExecuted(params.positionId, params.executor, capturedOEV, result.txHash);
        
        return result;
    }

    /**
     * @notice borrower validation basic
     */
    function validateLiquidation(uint256 positionId) public view returns (bool) {
        if (liquidationResults[positionId].executed) {
            return false;
        }

        // Check if position is still liquidatable
        return true;
    }

    /**
     * @notice Get liquidation result
     */
    function getLiquidationResult(uint256 positionId) external view returns (LiquidationResult memory) {
        return liquidationResults[positionId];
    }

    /**
     * @notice Calculate OEV captured
     */
    function calculateOEVCaptured(LiquidationParams memory params) internal pure returns (uint256) {
        // For JellyMock, the planner already computes OEVPotential as:
        //   max(0, liquidationReward - estimatedGasCost)
        // where liquidationReward = collateralValue * liquidationBonus.
        //
        // We treat this as an estimated "captured OEV" fallback when there is
        // no auction house providing a realized on-chain value. It is NOT a
        // precise economic settlement measure.
        return params.oevPotential;
    }
}

