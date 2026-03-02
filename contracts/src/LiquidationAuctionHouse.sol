// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title LiquidationAuctionHouse
 * @notice Tracks liquidation auctions for positions and winning executors.
 *
 * @dev
 * This contract is intentionally minimal and accounting-focused:
 * - It does NOT implement on-chain bidding logic.
 * - Bids are discovered and evaluated off-chain by the Jelly engine / CRE DON.
 * - The engine records the winning executor and bid amounts here for transparency
 *   and to enable future on-chain enforcement (slashing, distributions, etc.).
 *
 * The intended flow is:
 *  1. Jelly engine detects a liquidatable position and runs an off-chain auction
 *     among registered executors/searchers.
 *  2. The engine calls `recordWinningBid` with:
 *       - positionId
 *       - winning executor address
 *       - total bid amount
 *       - upfront amount (half of total bid in the intended design)
 *  3. After the execution window, the engine calls `settleSuccess` or
 *     `settleFailure` to mark whether the executor delivered.
 *  4. Other contracts (e.g., OEVDistributor, ExecutorRegistry) can use this
 *     state and the emitted events to apply rewards or penalties.
 */
contract LiquidationAuctionHouse {
    struct Auction {
        uint256 positionId;
        address executor;
        uint256 totalBid;      // Total bid amount for this position (abstract units)
        uint256 upfrontAmount; // Portion considered "paid upfront" (e.g., 50% of totalBid)
        uint64 startBlock;
        uint64 deadlineBlock;
        bool settled;
        bool success;
    }

    /// @notice positionId => auction state
    mapping(uint256 => Auction) public auctions;

    /// @notice positionId => executor => bid amount
    mapping(uint256 => mapping(address => uint256)) public bids;

    /// @notice positionId => list of bidders (for best-bid scans)
    mapping(uint256 => address[]) private bidders;

    /// @notice positionId => executor => has bid flag
    mapping(uint256 => mapping(address => bool)) private hasBidder;

    address public owner;
    address public jellyEngine;

    event AuctionStarted(
        uint256 indexed positionId,
        uint64 startBlock,
        uint64 deadlineBlock
    );

    event WinningBidRecorded(
        uint256 indexed positionId,
        address indexed executor,
        uint256 totalBid,
        uint256 upfrontAmount
    );

    event BidPlaced(
        uint256 indexed positionId,
        address indexed executor,
        uint256 amount
    );

    event AuctionSettled(
        uint256 indexed positionId,
        address indexed executor,
        bool success
    );

    modifier onlyOwner() {
        require(msg.sender == owner, "Only owner");
        _;
    }

    modifier onlyJellyEngine() {
        require(msg.sender == jellyEngine, "Only Jelly Engine");
        _;
    }

    constructor(address _jellyEngine) {
        owner = msg.sender;
        jellyEngine = _jellyEngine;
    }

    /**
     * @notice Set jelly engine address (CRE workflow coordinator).
     */
    function setJellyEngine(address _jellyEngine) external onlyOwner {
        jellyEngine = _jellyEngine;
    }

    /**
     * @notice Initialize or reset an auction for a given position.
     * @param positionId ID of the liquidatable position.
     * @param deadlineBlock Block after which the auction is considered expired.
     */
    function startAuction(
        uint256 positionId,
        uint64 deadlineBlock
    ) external onlyJellyEngine {
        Auction storage a = auctions[positionId];
        a.positionId = positionId;
        a.startBlock = uint64(block.number);
        a.deadlineBlock = deadlineBlock;
        a.settled = false;
        a.success = false;
        a.executor = address(0);
        a.totalBid = 0;
        a.upfrontAmount = 0;

        emit AuctionStarted(positionId, a.startBlock, deadlineBlock);
    }

    /**
     * @notice Place or update a bid for a given position.
     * @dev
     * - Requires the auction to be started.
     * - Does not enforce any specific payment mechanism; this contract
     *   only records bids for selection and accounting.
     */
    function placeBid(uint256 positionId, uint256 amount) external {
        require(amount > 0, "Bid must be > 0");

        Auction storage a = auctions[positionId];
        require(a.positionId == positionId, "Auction not started");
        if (a.deadlineBlock != 0) {
            require(block.number <= a.deadlineBlock, "Auction ended");
        }

        if (!hasBidder[positionId][msg.sender]) {
            bidders[positionId].push(msg.sender);
            hasBidder[positionId][msg.sender] = true;
        }

        bids[positionId][msg.sender] = amount;

        emit BidPlaced(positionId, msg.sender, amount);
    }

    /**
     * @notice Return the current best bid (highest amount) for a given position.
     * @return bestExecutor Address of the best bidder (zero address if none).
     * @return bestBid Bid amount of the best bidder (0 if none).
     */
    function getBestBid(
        uint256 positionId
    ) external view returns (address bestExecutor, uint256 bestBid) {
        address[] memory addrs = bidders[positionId];
        uint256 len = addrs.length;
        if (len == 0) {
            return (address(0), 0);
        }

        uint256 highest = 0;
        address highestAddr = address(0);

        for (uint256 i = 0; i < len; i++) {
            address bidder = addrs[i];
            uint256 bidAmount = bids[positionId][bidder];
            if (bidAmount > highest) {
                highest = bidAmount;
                highestAddr = bidder;
            }
        }

        return (highestAddr, highest);
    }

    /**
     * @notice Record the winning bid for a position after an off-chain auction.
     *
     * @dev
     * - This should be called once per position, after `startAuction`.
     * - `upfrontAmount` is expected to be half of `totalBid` in the intended design,
     *   but this contract does not enforce that ratio.
     */
    function recordWinningBid(
        uint256 positionId,
        address executor,
        uint256 totalBid,
        uint256 upfrontAmount
    ) external onlyJellyEngine {
        Auction storage a = auctions[positionId];
        require(a.positionId == positionId, "Auction not started");
        require(!a.settled, "Auction already settled");
        require(executor != address(0), "Invalid executor");

        a.executor = executor;
        a.totalBid = totalBid;
        a.upfrontAmount = upfrontAmount;

        emit WinningBidRecorded(positionId, executor, totalBid, upfrontAmount);
    }

    /**
     * @notice Settle auction as successful (executor delivered liquidation).
     */
    function settleSuccess(uint256 positionId) external onlyJellyEngine {
        Auction storage a = auctions[positionId];
        require(a.positionId == positionId, "Auction not started");
        require(!a.settled, "Auction already settled");

        a.settled = true;
        a.success = true;

        emit AuctionSettled(positionId, a.executor, true);
    }

    /**
     * @notice Settle auction as failed (executor did not deliver in time).
     */
    function settleFailure(uint256 positionId) external onlyJellyEngine {
        Auction storage a = auctions[positionId];
        require(a.positionId == positionId, "Auction not started");
        require(!a.settled, "Auction already settled");

        a.settled = true;
        a.success = false;

        emit AuctionSettled(positionId, a.executor, false);
    }

    /**
     * @notice View helper to get auction details for a position.
     */
    function getAuction(
        uint256 positionId
    ) external view returns (Auction memory) {
        return auctions[positionId];
    }
}

