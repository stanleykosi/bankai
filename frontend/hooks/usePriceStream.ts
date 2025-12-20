/**
 * @description
 * Shared hook for consuming the SSE price stream and hydrating market data with
 * live yes/no prices.
 */
"use client";

import * as React from "react";

import { API_BASE_URL } from "@/lib/api";
import { Market } from "@/types";

type AssetPrice = {
  condition_id: string;
  best_bid?: number;
  best_ask?: number;
  last_trade_price?: number;
  timestamp?: string;
  last_trade_timestamp?: string;
};

const FLUSH_INTERVAL_MS = 200;

export const usePriceStream = () => {
  const [assetPrices, setAssetPrices] = React.useState<Record<string, AssetPrice>>({});
  const priceMapRef = React.useRef<Record<string, AssetPrice>>({});
  const needsFlushRef = React.useRef(false);

  React.useEffect(() => {
    let source: EventSource | null = null;
    let retryHandle: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      source = new EventSource(`${API_BASE_URL}/api/v1/markets/stream`);
      source.onmessage = (event) => {
        try {
        const payload = JSON.parse(event.data) as {
          condition_id: string;
          asset_id: string;
          best_bid?: number;
          best_ask?: number;
          timestamp?: string;
          last_trade_price?: number;
          last_trade_timestamp?: string;
        };

        const existing = priceMapRef.current[payload.asset_id];
        const next: AssetPrice = {
          condition_id: payload.condition_id || existing?.condition_id || "",
          best_bid: existing?.best_bid,
          best_ask: existing?.best_ask,
          last_trade_price: existing?.last_trade_price,
          timestamp: existing?.timestamp,
          last_trade_timestamp: existing?.last_trade_timestamp,
        };

        if (typeof payload.best_bid === "number") {
          next.best_bid = payload.best_bid;
        }
        if (typeof payload.best_ask === "number") {
          next.best_ask = payload.best_ask;
        }
        if (typeof payload.timestamp === "string") {
          next.timestamp = payload.timestamp;
        }
        if (typeof payload.last_trade_price === "number") {
          next.last_trade_price = payload.last_trade_price;
        }
        if (typeof payload.last_trade_timestamp === "string") {
          next.last_trade_timestamp = payload.last_trade_timestamp;
        }

        priceMapRef.current[payload.asset_id] = next;
        needsFlushRef.current = true;
      } catch (error) {
        console.error("Failed to parse price update:", error);
      }
      };

      source.onerror = () => {
        console.warn("SSE connection lost, retrying...");
        source?.close();
        retryHandle = setTimeout(connect, 3000);
      };
    };

    connect();

    const flushTimer = setInterval(() => {
      if (needsFlushRef.current) {
        needsFlushRef.current = false;
        setAssetPrices({ ...priceMapRef.current });
      }
    }, FLUSH_INTERVAL_MS);

    return () => {
      if (retryHandle) {
        clearTimeout(retryHandle);
      }
      clearInterval(flushTimer);
      source?.close();
    };
  }, []);

  const augmentMarket = React.useCallback(
    (market: Market): Market => {
      const yes = market.token_id_yes ? assetPrices[market.token_id_yes] : undefined;
      const no = market.token_id_no ? assetPrices[market.token_id_no] : undefined;

      return {
        ...market,
        // Store last trade price for spread-aware display rules.
        yes_price: yes?.last_trade_price ?? market.yes_price,
        yes_best_bid: yes?.best_bid ?? market.yes_best_bid,
        yes_best_ask: yes?.best_ask ?? market.yes_best_ask,
        yes_price_updated: yes?.timestamp ?? yes?.last_trade_timestamp ?? market.yes_price_updated,
        no_price: no?.last_trade_price ?? market.no_price,
        no_best_bid: no?.best_bid ?? market.no_best_bid,
        no_best_ask: no?.best_ask ?? market.no_best_ask,
        no_price_updated: no?.timestamp ?? no?.last_trade_timestamp ?? market.no_price_updated,
      };
    },
    [assetPrices]
  );

  const hydrateMarkets = React.useCallback(
    (markets?: Market[]) => (markets || []).map(augmentMarket),
    [augmentMarket]
  );

  return { hydrateMarkets, augmentMarket };
};
