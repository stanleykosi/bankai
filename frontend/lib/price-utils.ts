/**
 * @description
 * Price calculation utilities for consistent price display across the UI.
 * 
 * Key concepts:
 * - Last Trade Price: The price at which the most recent trade executed (can be stale)
 * - Best Bid/Ask: Current best bid and ask from the order book (live)
 * - Mid-Price: Average of best bid and ask, used for display unless spread exceeds $0.10
 */

const MAX_DISPLAY_SPREAD = 0.10; // Polymarket rule: use last trade when spread > $0.10
const warnedMissingLastTrade = new Set<string>();

/**
 * Calculates the display price according to Polymarket's pricing rule:
 * - Use midpoint of bid/ask.
 * - If spread > $0.10, use last traded price instead.
 *
 * @param bestBid - Current best bid price (0-1)
 * @param bestAsk - Current best ask price (0-1)
 * @param lastTradePrice - Last traded price fallback (0-1)
 * @param context - Optional identifier for guardrail logging
 * @returns Display price or undefined when no valid inputs are available
 */
export function calculateDisplayPrice(
  bestBid?: number,
  bestAsk?: number,
  lastTradePrice?: number,
  context?: string
): number | undefined {
  const hasBid = typeof bestBid === "number" && bestBid > 0;
  const hasAsk = typeof bestAsk === "number" && bestAsk > 0;
  const hasLastTrade = typeof lastTradePrice === "number" && lastTradePrice > 0;

  if (hasBid && hasAsk && bestBid! <= bestAsk!) {
    const spread = bestAsk! - bestBid!;
    if (spread > MAX_DISPLAY_SPREAD) {
      if (!hasLastTrade) {
        const key = context || "unknown";
        if (!warnedMissingLastTrade.has(key)) {
          warnedMissingLastTrade.add(key);
          console.warn(
            "[pricing] Spread exceeds $0.10 but no last trade price is available.",
            { context: key, bestBid, bestAsk, spread }
          );
        }
      }
      return hasLastTrade ? lastTradePrice : undefined;
    }
    return (bestBid! + bestAsk!) / 2;
  }

  if (hasLastTrade) {
    return lastTradePrice;
  }

  return undefined;
}

/**
 * Gets the current display price for a market outcome.
 * Prioritizes mid-price (most accurate) over last trade price (can be stale).
 * 
 * @param bestBid - Current best bid price
 * @param bestAsk - Current best ask price
 * @param lastTradePrice - Last trade price
 * @param context - Optional identifier for guardrail logging
 * @returns Current price to display
 */
export function getCurrentPrice(
  bestBid?: number,
  bestAsk?: number,
  lastTradePrice?: number,
  context?: string
): number | undefined {
  return calculateDisplayPrice(bestBid, bestAsk, lastTradePrice, context);
}
