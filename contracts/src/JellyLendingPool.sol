// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title JellyLendingPool
 * @notice Minimal mock lending pool used by Jelly Engine for end-to-end testing.
 *
 * The Go adapter in internal/adapters/jellymock/lending.go expects the ABI:
 *
 *   function getAllPositions() external view returns (Position[]);
 *   function liquidate(address borrower) external;
 *
 * where Position is the tuple:
 *   (address borrower,
 *    uint256 collateralAmount,
 *    address collateralToken,
 *    uint256 debtAmount,
 *    address debtToken,
 *    uint256 healthFactor,
 *    bool isActive)
 *
 * This contract implements exactly that shape and provides simple admin
 * functions to seed and manage positions for testing. It is NOT a full
 * lending protocol.
 */
contract JellyLendingPool {
    struct Position {
        address borrower;
        uint256 collateralAmount;
        address collateralToken;
        uint256 debtAmount;
        address debtToken;
        uint256 healthFactor;
        bool isActive;
    }

    address public owner;

    // Storage of all positions. Indices are stable; lookups by borrower use a mapping.
    Position[] private _positions;
    mapping(address => uint256) private _positionIndex; // borrower -> index + 1

    event PositionUpserted(
        address indexed borrower,
        uint256 collateralAmount,
        address collateralToken,
        uint256 debtAmount,
        address debtToken,
        uint256 healthFactor,
        bool isActive
    );

    event Liquidated(address indexed borrower);

    modifier onlyOwner() {
        require(msg.sender == owner, "Only owner");
        _;
    }

    constructor() {
        owner = msg.sender;
    }

    /**
     * @notice Returns all positions currently stored in the pool.
     * @dev Matches the ABI expected by the Go adapter.
     */
    function getAllPositions() external view returns (Position[] memory) {
        return _positions;
    }


    
    /**
     * @notice Upsert a position for a borrower.
     * @dev this is only for testing purposes to create or update positions, no realtime functionality.
     */
    function upsertPosition(
        address borrower,
        uint256 collateralAmount,
        address collateralToken,
        uint256 debtAmount,
        address debtToken,
        uint256 healthFactor,
        bool isActive
    ) external onlyOwner {
        require(borrower != address(0), "Invalid borrower");

        uint256 idxPlusOne = _positionIndex[borrower];
        if (idxPlusOne == 0) {
            // New position
            _positions.push(
                Position({
                    borrower: borrower,
                    collateralAmount: collateralAmount,
                    collateralToken: collateralToken,
                    debtAmount: debtAmount,
                    debtToken: debtToken,
                    healthFactor: healthFactor,
                    isActive: isActive
                })
            );
            _positionIndex[borrower] = _positions.length; // index + 1
        } else {
            // Update existing position
            uint256 idx = idxPlusOne - 1;
            Position storage p = _positions[idx];
            p.collateralAmount = collateralAmount;
            p.collateralToken = collateralToken;
            p.debtAmount = debtAmount;
            p.debtToken = debtToken;
            p.healthFactor = healthFactor;
            p.isActive = isActive;
        }

        emit PositionUpserted(
            borrower,
            collateralAmount,
            collateralToken,
            debtAmount,
            debtToken,
            healthFactor,
            isActive
        );
    }

    /**
     * @notice Liquidate a borrower.
     * @dev Minimal behavior: mark the position as inactive and set debt to zero.
     *      The real health factor will be recomputed off-chain by the adapter;
     *      here we just provide a simple state transition.
     */
    function liquidate(address borrower) external {
        require(borrower != address(0), "Invalid borrower");
        uint256 idxPlusOne = _positionIndex[borrower];
        require(idxPlusOne != 0, "Position not found");

        uint256 idx = idxPlusOne - 1;
        Position storage p = _positions[idx];
        require(p.isActive, "Already liquidated");

        p.isActive = false;
        p.debtAmount = 0;

        emit Liquidated(borrower);
    }
}

