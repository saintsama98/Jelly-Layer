// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title PriorityQueue
 * @notice Stores prioritized liquidation queue with anti-cascading logic
 */
contract PriorityQueue {
    struct LiquidationPosition {
        uint256 positionId;
        address borrower;
        uint256 healthFactor;      // Multiplied by 1e18
        uint256 collateralValue;   // in USD equivalents
        uint256 debtValue;         // in USD equivalents
        uint256 priorityScore;     // 0-10000 scale
        uint8 urgencyLevel;        // 1 (low) - 5 (critical)
        uint64 timestamp;          // Block timestamp
        bytes executionData;       // Encoded liquidation calldata
    }

    mapping(uint256 => LiquidationPosition) public priorityQueue;
    uint256[] public orderedPositionIds;     // Sorted by priority
    uint256 public queueId;
    
    address public owner;
    address public jellyEngine; // CRE workflow address

    event PrioritiesSubmitted(uint256 indexed queueId, uint256 timestamp, uint256 positionCount);
    event QueueUpdated(uint256 indexed queueId, uint256 positionCount, uint256 deferredCount);

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
     * @notice Submit liquidatable positions (called by Detection workflow)
     */
    function submitLiquidatablePositions(LiquidationPosition[] memory positions) external onlyJellyEngine {
        // Clear previous queue
        delete orderedPositionIds;
        
        // Store positions
        for (uint256 i = 0; i < positions.length; i++) {
            priorityQueue[positions[i].positionId] = positions[i];
            orderedPositionIds.push(positions[i].positionId);
        }
        
        queueId++;
        emit PrioritiesSubmitted(queueId, block.timestamp, positions.length);
    }

    /**
     * @notice Update queue with prioritized and deferred positions (called by Prioritization workflow)
     */
    function updateQueue(
        uint256[] memory prioritizedIds,
        uint256[] memory deferredIds
    ) external onlyJellyEngine {
        // Update ordered list
        orderedPositionIds = prioritizedIds;
        
        emit QueueUpdated(queueId, prioritizedIds.length, deferredIds.length);
    }

    /**
     * @notice Get prioritized queue
     */
    function getPrioritizedQueue() external view returns (LiquidationPosition[] memory) {
        LiquidationPosition[] memory queue = new LiquidationPosition[](orderedPositionIds.length);
        for (uint256 i = 0; i < orderedPositionIds.length; i++) {
            queue[i] = priorityQueue[orderedPositionIds[i]];
        }
        return queue;
    }

    /**
     * @notice Get position by ID
     */
    function getPosition(uint256 positionId) external view returns (LiquidationPosition memory) {
        return priorityQueue[positionId];
    }

    /**
     * @notice Clear queue
     */
    function clearQueue() external onlyJellyEngine {
        for (uint256 i = 0; i < orderedPositionIds.length; i++) {
            delete priorityQueue[orderedPositionIds[i]];
        }
        delete orderedPositionIds;
    }
}

