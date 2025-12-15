/**
 * Background sync script to persist builder-attributed trades into the backend.
 * Uses the official Polymarket JS SDK to fetch builder trades and posts them to
 * the backend internal sync endpoint secured by JOB_SYNC_SECRET.
 *
 * Usage (from repo root or frontend/):
 *   export POLY_BUILDER_API_KEY=...
 *   export POLY_BUILDER_SECRET=...
 *   export POLY_BUILDER_PASSPHRASE=...
 *   export BACKEND_URL=http://localhost:8080
 *   export JOB_SYNC_SECRET=super-secret-string
 *   bun run scripts/sync_builder_trades.ts
 */

import { ClobClient, OrderType, Side, BuilderTrade } from "@polymarket/clob-client";
import { BuilderApiKeyCreds, BuilderConfig } from "@polymarket/builder-signing-sdk";
import axios from "axios";

const BACKEND_URL = process.env.BACKEND_URL || "http://localhost:8080";
const CLOB_API_URL = process.env.CLOB_API_URL || "https://clob.polymarket.com";
const JOB_SYNC_SECRET = process.env.JOB_SYNC_SECRET || "";

const BUILDER_CREDENTIALS: BuilderApiKeyCreds = {
  key: process.env.POLY_BUILDER_API_KEY || "",
  secret: process.env.POLY_BUILDER_SECRET || "",
  passphrase: process.env.POLY_BUILDER_PASSPHRASE || "",
};

if (!JOB_SYNC_SECRET) {
  throw new Error("JOB_SYNC_SECRET is required");
}
if (!BUILDER_CREDENTIALS.key || !BUILDER_CREDENTIALS.secret || !BUILDER_CREDENTIALS.passphrase) {
  throw new Error("POLY_BUILDER_API_KEY/SECRET/PASSPHRASE are required");
}

function mapTradeToPayload(trade: BuilderTrade) {
  const toDate = (val?: string | null) =>
    val && !Number.isNaN(Date.parse(val)) ? new Date(val).toISOString() : new Date().toISOString();

  return {
    orderId: trade.id,
    marketId: trade.market || "",
    outcome: trade.outcome || trade.assetId || "",
    outcomeTokenId: trade.assetId || "",
    makerAddress: trade.maker || trade.owner || "",
    side: trade.side === "SELL" || trade.side === Side.SELL ? "SELL" : "BUY",
    price: Number(trade.price || 0),
    size: Number(trade.size || 0),
    orderType: OrderType.FOK,
    status: trade.status || "FILLED",
    statusDetail: trade.status || "",
    orderHashes: trade.takerOrderHash ? [trade.takerOrderHash] : [],
    source: "BANKAI",
    createdAt: toDate(trade.createdAt || trade.matchTime),
    updatedAt: toDate(trade.updatedAt || trade.matchTime),
  };
}

async function main() {
  const builderConfig = new BuilderConfig({
    localBuilderCreds: BUILDER_CREDENTIALS,
  });

  const client = new ClobClient(
    CLOB_API_URL,
    137,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    true,
    builderConfig
  );

  const raw = await client.getBuilderTrades();
  // Some SDK versions return { trades, next_cursor, ... }
  const trades = Array.isArray(raw) ? raw : raw?.trades || [];
  if (!Array.isArray(trades) || trades.length === 0) {
    console.log("No builder trades found");
    return;
  }

  const payload = trades.map(mapTradeToPayload);
  await axios.post(
    `${BACKEND_URL}/api/v1/trade/sync/internal`,
    { orders: payload },
    {
      headers: {
        "X-Job-Secret": JOB_SYNC_SECRET,
      },
      timeout: 15000,
    }
  );

  console.log(`Synced ${payload.length} builder trades`);
}

main().catch((err) => {
  console.error("Sync builder trades failed", err);
  process.exit(1);
});
