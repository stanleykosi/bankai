/**
 * @description
 * EIP-712 Signing Utilities for Polymarket Orders.
 * Constructs the Typed Data payload required for placing orders via the CLOB.
 * 
 * @dependencies
 * - viem (implied by wagmi usage in project)
 * - ./polymarket (constants)
 * 
 * @notes
 * - The structure MUST match the on-chain Exchange contract's EIP-712 definition.
 * - Handles conversion of human-readable amounts to wei/atomic units.
 */

import {
  CTF_EXCHANGE_ADDR,
  POLYGON_CHAIN_ID,
  EIP712_DOMAIN_NAME,
  EIP712_DOMAIN_VERSION,
  generateSalt
} from "./polymarket";

export const CLOB_AUTH_DOMAIN = {
  name: "ClobAuthDomain",
  version: "1",
  chainId: POLYGON_CHAIN_ID,
} as const;

export const CLOB_AUTH_TYPES = {
  ClobAuth: [
    { name: "address", type: "address" },
    { name: "timestamp", type: "string" },
    { name: "nonce", type: "uint256" },
    { name: "message", type: "string" },
  ],
} as const;

// Order types for EIP-712
export const ORDER_TYPES = {
  Order: [
    { name: "salt", type: "uint256" },
    { name: "maker", type: "address" },
    { name: "signer", type: "address" },
    { name: "taker", type: "address" },
    { name: "tokenId", type: "uint256" },
    { name: "makerAmount", type: "uint256" },
    { name: "takerAmount", type: "uint256" },
    { name: "expiration", type: "uint256" },
    { name: "nonce", type: "uint256" },
    { name: "feeRateBps", type: "uint256" },
    { name: "side", type: "uint8" },
  ],
} as const;

export type OrderStruct = {
  salt: bigint;
  maker: `0x${string}`;
  signer: `0x${string}`;
  taker: `0x${string}`;
  tokenId: bigint;
  makerAmount: bigint;
  takerAmount: bigint;
  expiration: bigint;
  nonce: bigint;
  feeRateBps: bigint;
  side: number; // 0 for BUY, 1 for SELL
};

/**
 * Constructs the EIP-712 domain separator.
 */
export const getDomain = () => ({
  name: EIP712_DOMAIN_NAME,
  version: EIP712_DOMAIN_VERSION,
  chainId: POLYGON_CHAIN_ID,
  verifyingContract: CTF_EXCHANGE_ADDR as `0x${string}`,
});

/**
 * Parameters to build an order signature payload.
 */
export interface BuildOrderParams {
  maker: `0x${string}`;      // User's address (EOA or Vault)
  signer: `0x${string}`;     // Actual signer (EOA)
  tokenId: string;    // The asset ID (Yes or No token)
  price: number;      // Limit price (0.0 to 1.0)
  size: number;       // Number of shares
  side: "BUY" | "SELL";
  expiration?: number; // 0 for GTC
  feeRateBps?: number; // Usually 0 for maker
  nonce?: number;      // Exchange nonce (optional, random/0 used if stateless)
}

/**
 * Prepares the Typed Data for signing.
 * 
 * Logic for Amounts:
 * - BUY (Side 0): Maker gives USDC (Collateral), Taker gives Outcome Token.
 *   makerAmount = size * price (Cost in USDC)
 *   takerAmount = size (Shares to receive)
 * 
 * - SELL (Side 1): Maker gives Outcome Token, Taker gives USDC.
 *   makerAmount = size (Shares to sell)
 *   takerAmount = size * price (Proceeds in USDC)
 * 
 * Note: USDC has 6 decimals. Outcome tokens have 6 decimals (on Polymarket CTF).
 *       However, the CLOB API often expects specific handling. 
 *       Usually both are treated as 1e6 for "1 unit".
 */
export function buildOrderTypedData(params: BuildOrderParams) {
  const {
    maker,
    signer,
    tokenId,
    price,
    size,
    side,
    expiration = 0,
    feeRateBps = 0,
    nonce = 0,
  } = params;

  // Polymarket Tokens (and USDC on Polygon) use 6 decimals
  const DECIMALS = 6;
  const multiplier = Math.pow(10, DECIMALS);

  // Parse Size (Shares)
  const sizeRaw = BigInt(Math.floor(size * multiplier));

  // Parse Price (0.00 - 1.00)
  // Need precise calculation for cost/proceeds
  const valueRaw = BigInt(Math.floor(size * price * multiplier));

  let makerAmount: bigint;
  let takerAmount: bigint;
  let sideInt: number;

  if (side === "BUY") {
    sideInt = 0;
    // Buying: Maker pays USDC (value), Taker gives Shares (size)
    makerAmount = valueRaw;
    takerAmount = sizeRaw;
  } else {
    sideInt = 1;
    // Selling: Maker gives Shares (size), Taker gives USDC (value)
    makerAmount = sizeRaw;
    takerAmount = valueRaw;
  }

  const salt = generateSalt();

  const message: OrderStruct = {
    salt,
    maker,
    signer,
    taker: "0x0000000000000000000000000000000000000000" as `0x${string}`, // Open order
    tokenId: BigInt(tokenId),
    makerAmount,
    takerAmount,
    expiration: BigInt(expiration),
    nonce: BigInt(nonce),
    feeRateBps: BigInt(feeRateBps),
    side: sideInt,
  };

  return {
    domain: getDomain(),
    types: ORDER_TYPES,
    primaryType: "Order" as const,
    message,
  };
}

export interface BuildClobAuthParams {
  address: `0x${string}`;
  timestamp: string; // unix seconds as string
  nonce?: number;
  message?: string;
}

// Builds the typed data for CLOB API key derivation (L1 auth)
export function buildClobAuthTypedData(params: BuildClobAuthParams) {
  const { address, timestamp, nonce = 0, message = "This message attests that I control the given wallet" } = params;

  return {
    domain: CLOB_AUTH_DOMAIN,
    types: CLOB_AUTH_TYPES,
    primaryType: "ClobAuth" as const,
    message: {
      address,
      timestamp,
      nonce,
      message,
    },
  };
}
