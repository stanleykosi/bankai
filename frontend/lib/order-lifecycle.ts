"use client";

import { OpenOrder, Trade as ClobTrade } from "@polymarket/clob-client";
import type { OrderRecord, OrderStatus } from "@/types";

type TradeAccumulator = {
  id: string;
  marketId: string | null;
  outcome: string;
  tokenId: string;
  side: "BUY" | "SELL";
  avgPrice: number;
  totalSize: number;
  latestStatus: string;
  updatedAt: string;
  createdAt: string;
  makerAddress: string;
  txHash?: string | null;
};

const STATUS_MAP: Array<{ match: RegExp; status: OrderStatus }> = [
  { match: /pending|queued|created|delayed/, status: "PENDING" },
  { match: /live|open|active|unmatched/, status: "OPEN" },
  { match: /partial|partially[_\s-]?filled/, status: "PARTIALLY_FILLED" },
  { match: /filled|matched|executed/, status: "FILLED" },
  { match: /cancel/, status: "CANCELED" },
  { match: /expired/, status: "EXPIRED" },
  { match: /reject|invalid/, status: "REJECTED" },
  { match: /fail|error/, status: "FAILED" },
];

const toNumber = (input?: string | number | null): number => {
  if (input === null || input === undefined) return 0;
  const n = typeof input === "number" ? input : parseFloat(input);
  return Number.isFinite(n) ? n : 0;
};

const toISODate = (value?: string | number | null): string => {
  if (typeof value === "number") {
    return new Date(value * 1000).toISOString();
  }
  if (typeof value === "string") {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed)) return new Date(parsed).toISOString();
  }
  return new Date().toISOString();
};

export const mapOrderStatus = (
  raw?: string | null,
  fallback: OrderStatus = "FAILED"
): OrderStatus => {
  const value = String(raw || "").toLowerCase().trim();
  if (!value) return fallback;

  for (const item of STATUS_MAP) {
    if (item.match.test(value)) {
      return item.status;
    }
  }
  return fallback;
};

export const resolveTradeOrderID = (trade: ClobTrade): string => {
  const normalize = (value?: string | null) =>
    String(value || "").trim().toLowerCase();
  const selfAddress = normalize(trade.maker_address || trade.owner || "");

  if (trade.trader_side === "TAKER" && trade.taker_order_id) {
    return trade.taker_order_id;
  }

  if (Array.isArray(trade.maker_orders) && trade.maker_orders.length > 0) {
    const matchedMaker = trade.maker_orders.find(
      (maker) =>
        normalize(maker.maker_address) === selfAddress ||
        normalize(maker.owner) === selfAddress
    );
    if (matchedMaker?.order_id) {
      return matchedMaker.order_id;
    }
    if (trade.maker_orders[0]?.order_id) {
      return trade.maker_orders[0].order_id;
    }
  }

  if (trade.taker_order_id) {
    return trade.taker_order_id;
  }
  return trade.id;
};

const mapOpenOrder = (order: OpenOrder): OrderRecord => {
  const createdAt = toISODate(order.created_at);
  return {
    id: order.id,
    user_id: "",
    clob_order_id: order.id,
    market_id: order.market || null,
    side: order.side?.toUpperCase() === "SELL" ? "SELL" : "BUY",
    outcome: order.outcome || order.asset_id || "",
    outcome_token_id: order.asset_id || "",
    price: toNumber(order.price),
    size: toNumber(order.original_size || order.size_matched),
    order_type: order.order_type || "GTC",
    status: mapOrderStatus(order.status, "OPEN"),
    status_detail: order.status || null,
    order_hashes:
      order.associate_trades && order.associate_trades.length > 0
        ? order.associate_trades
        : null,
    source: "UNKNOWN",
    maker_address: order.owner || order.maker_address || "",
    error_msg: null,
    tx_hash: null,
    created_at: createdAt,
    updated_at: createdAt,
  };
};

const accumulateTrade = (
  existing: TradeAccumulator | undefined,
  trade: ClobTrade
): TradeAccumulator => {
  const id = resolveTradeOrderID(trade);
  const size = toNumber(trade.size);
  const price = toNumber(trade.price);
  const createdAt = toISODate(trade.match_time || trade.last_update || null);
  const updatedAt = toISODate(trade.last_update || trade.match_time || null);

  if (!existing) {
    return {
      id,
      marketId: trade.market || null,
      outcome: trade.outcome || trade.asset_id || "",
      tokenId: trade.asset_id || "",
      side: trade.side?.toUpperCase() === "SELL" ? "SELL" : "BUY",
      avgPrice: price,
      totalSize: size,
      latestStatus: trade.status || "filled",
      updatedAt,
      createdAt,
      makerAddress: trade.owner || trade.maker_address || "",
      txHash: trade.transaction_hash || null,
    };
  }

  const total = existing.totalSize + size;
  const avg =
    total > 0
      ? (existing.avgPrice * existing.totalSize + price * size) / total
      : existing.avgPrice;
  const isNewer =
    Date.parse(updatedAt) >= Date.parse(existing.updatedAt || existing.createdAt);

  return {
    ...existing,
    avgPrice: avg,
    totalSize: total,
    latestStatus: isNewer ? trade.status || existing.latestStatus : existing.latestStatus,
    updatedAt: isNewer ? updatedAt : existing.updatedAt,
    txHash: (isNewer ? trade.transaction_hash : undefined) || existing.txHash || null,
  };
};

const toRecordFromTrade = (agg: TradeAccumulator): OrderRecord => ({
  id: agg.id,
  user_id: "",
  clob_order_id: agg.id,
  market_id: agg.marketId,
  side: agg.side,
  outcome: agg.outcome,
  outcome_token_id: agg.tokenId,
  price: agg.avgPrice,
  size: agg.totalSize,
  order_type: "TRADE",
  status: mapOrderStatus(agg.latestStatus, "FILLED"),
  status_detail: agg.latestStatus || null,
  order_hashes: null,
  source: "UNKNOWN",
  maker_address: agg.makerAddress || "",
  error_msg: null,
  tx_hash: agg.txHash || null,
  created_at: agg.createdAt,
  updated_at: agg.updatedAt,
});

export const reconcileOrderLifecycle = (
  openOrders: OpenOrder[],
  trades: ClobTrade[]
): OrderRecord[] => {
  const open = (openOrders || []).map(mapOpenOrder);
  const tradeMap = new Map<string, TradeAccumulator>();
  for (const trade of trades || []) {
    const orderID = resolveTradeOrderID(trade);
    tradeMap.set(orderID, accumulateTrade(tradeMap.get(orderID), trade));
  }

  const merged = new Map<string, OrderRecord>();
  for (const [orderID, agg] of tradeMap) {
    merged.set(orderID, toRecordFromTrade(agg));
  }

  for (const openOrder of open) {
    const existing = merged.get(openOrder.clob_order_id);
    if (!existing) {
      merged.set(openOrder.clob_order_id, openOrder);
      continue;
    }

    const filledSize = existing.size || 0;
    const originalSize = openOrder.size || 0;
    const next: OrderRecord = {
      ...openOrder,
      updated_at:
        Date.parse(existing.updated_at) > Date.parse(openOrder.updated_at)
          ? existing.updated_at
          : openOrder.updated_at,
      tx_hash: existing.tx_hash || null,
    };

    if (filledSize > 0 && originalSize > 0 && filledSize < originalSize) {
      next.status = "PARTIALLY_FILLED";
      next.status_detail = "partially_filled";
    }
    if (filledSize >= originalSize && originalSize > 0) {
      next.status = "FILLED";
      next.status_detail = "filled";
      next.size = filledSize;
    }

    merged.set(openOrder.clob_order_id, next);
  }

  return Array.from(merged.values()).sort(
    (a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)
  );
};
