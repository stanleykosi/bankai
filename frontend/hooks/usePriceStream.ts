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
  price: number;
  best_bid: number;
  best_ask: number;
  timestamp: string;
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
            price: number;
            best_bid: number;
            best_ask: number;
            timestamp: string;
          };

          priceMapRef.current[payload.asset_id] = {
            condition_id: payload.condition_id,
            price: payload.price,
            best_bid: payload.best_bid,
            best_ask: payload.best_ask,
            timestamp: payload.timestamp,
          };
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
        yes_price: yes?.price ?? market.yes_price,
        yes_best_bid: yes?.best_bid ?? market.yes_best_bid,
        yes_best_ask: yes?.best_ask ?? market.yes_best_ask,
        yes_price_updated: yes?.timestamp ?? market.yes_price_updated,
        no_price: no?.price ?? market.no_price,
        no_best_bid: no?.best_bid ?? market.no_best_bid,
        no_best_ask: no?.best_ask ?? market.no_best_ask,
        no_price_updated: no?.timestamp ?? market.no_price_updated,
      };
    },
    [assetPrices]
  );

  const hydrateMarkets = React.useCallback(
    (markets?: Market[]) => (markets || []).map(augmentMarket),
    [augmentMarket]
  );

  return { hydrateMarkets };
};

