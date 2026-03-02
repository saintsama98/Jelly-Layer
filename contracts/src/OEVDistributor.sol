// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title OEVDistributor
 * @notice Captures and distributes OEV from liquidations
 */

/// @notice Interface for ERC20 token
/// @dev  interface for the ERC20 token since we will be more practical and open for this contract
 interface IERC20{
    function transfer(address to, uint256 amount) external returns (bool);
 }


 /// @notice interface for liquidatorAuctionHouse light version around struct
 interface ILiquidationAuctionHouse{
    struct Auction{
        uint256 positionId;
        address executor;
        uint256 totalBid;
        uint256 upfrontAmount;
        uint64 startBlock;
        uint64 deadlineBlock;
        bool settled;
        bool success;
    }
    function getAuction(uint256 positionId) external view returns (Auction memory);
 }
contract OEVDistributor {
    struct Distribution {
        uint256 totalOEV;
        uint256 protocolShare;
        uint256 executorShare;
    }

    mapping(uint256 => Distribution) public distributions;
    
    address public owner;
    address public jellyEngine;
    address public protocolTreasury;
    
    //oevToken for feasibility
    IERC20 public oevToken;
    // Current design: 50% protocol / 50% executor / 0% validator.
    uint256 public constant PROTOCOL_SPLIT = 50; // 50%
    uint256 public constant EXECUTOR_SPLIT = 50; // 50%
    uint256 public constant TOTAL_SPLIT = 100;

    event OEVCaptured(uint256 indexed liquidationId, uint256 amount);
    event OEVDistributed(
        uint256 indexed liquidationId,
        uint256 protocolShare,
        uint256 executorShare
        
    );

    modifier onlyJellyEngine() {
        require(msg.sender == jellyEngine, "Only Jelly Engine");
        _;
    }


    /// @param _protocolTreasury address for protocol treasury
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
            executorShare: (amount * EXECUTOR_SPLIT) / TOTAL_SPLIT
            // validatorShare: (amount * VALIDATOR_SPLIT) / TOTAL_SPLIT
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

        //we shall first get the liquidator or executor from the auction house

        ILiquidationAuctionHouse.Auction memory auction =  ILiquidationAuctionHouse(LiquidationAuctionHouse).getAuction(liquidationId);

        address executor= auction.executor;

        require(executor != address(0), "Executor not found");


        //transfer to executor or liquidator

        if (dist.executorShare >0){
            bool success = oevToken.transfer(executor, dist.executorShare);
            require(success, "Transfer to executor failed");
        }

        //transfer to protocol treasury
        if (dist.protocolShare >0){
            bool success2 = oevToken.transfer(protocolTreasury, dist.protocolShare);
            require(success2, "Transfer to protocol treasury failed");
        }
        
        //optional
        // Transfer to validator
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

