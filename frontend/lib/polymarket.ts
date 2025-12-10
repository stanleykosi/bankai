/**
 * @description
 * Polymarket protocol constants and helpers.
 * Defines contract addresses, chain IDs, and asset logic.
 * 
 * @notes
 * - CTF Exchange Address is critical for EIP-712 Domain Separator.
 * - Uses Polygon Mainnet (137) as default.
 */

export const POLYGON_CHAIN_ID = 137;

// The main CTF Exchange contract used by Polymarket for matching orders
// Source: https://docs.polymarket.com/#deployment-and-additional-information
export const CTF_EXCHANGE_ADDR = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E";

// Neg-Risk Exchange used for negatively correlated markets
export const NEG_RISK_CTF_EXCHANGE_ADDR = "0xC5d563A36AE78145C45a50134d48A1215220f80a";

// The Conditional Tokens Framework (CTF) contract
export const CTF_CONTRACT_ADDR = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045";

export const MAX_ALLOWANCE = "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff";

export const PRE_MATCH_CTA = "0x2F5e3684cb1F318ec51b00Edba38d79Ac2c0aA9d"; // UMA CTF Adapter

// Token mapping helpers
export const SIDE = {
  BUY: "BUY",
  SELL: "SELL",
} as const;

export type OrderSide = keyof typeof SIDE;

// Types for EIP-712 signing
export const EIP712_DOMAIN_NAME = "Polymarket CTF Exchange";
export const EIP712_DOMAIN_VERSION = "1";

/**
 * Calculates the expiration timestamp for an order.
 * @param seconds Seconds from now until expiration. Default 0 (GTC usually handled by API logic, but signed orders need a timestamp if GTD).
 * Note: CLOB API handles "0" as GTC in the timestamp field effectively for most clients, 
 * but for specific GTD orders we need explicit timestamps.
 */
export function getExpirationTimestamp(seconds: number = 0): number {
  if (seconds === 0) return 0;
  return Math.floor(Date.now() / 1000) + seconds;
}

/**
 * Generates a random salt for order uniqueness.
 */
export function generateSalt(): bigint {
  if (typeof crypto !== "undefined" && crypto.getRandomValues) {
    const bytes = new Uint8Array(32);
    crypto.getRandomValues(bytes);
    let hex = "0x";
    bytes.forEach((byte) => {
      hex += byte.toString(16).padStart(2, "0");
    });
    return BigInt(hex);
  }
  // Fallback: combine timestamp with Math.random to avoid duplicate salts in SSR/tests
  const random = BigInt(Math.floor(Math.random() * Number.MAX_SAFE_INTEGER));
  return (BigInt(Date.now()) << BigInt(64)) ^ random;
}
