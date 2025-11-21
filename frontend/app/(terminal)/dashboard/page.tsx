/**
 * @description
 * Dashboard Page.
 * The primary view for the "Market Radar".
 * Fetches market data and renders the Ticker lanes.
 *
 * @dependencies
 * - @tanstack/react-query
 * - frontend/lib/api
 * - frontend/components/terminal/MarketTicker
 */

"use client";

import React from "react";
import { useQuery } from "@tanstack/react-query";
import { api, API_BASE_URL } from "@/lib/api";
import { MarketTicker } from "@/components/terminal/MarketTicker";
import { Market } from "@/types";
import { Loader2 } from "lucide-react";

// Force dynamic rendering to prevent static generation
export const dynamic = 'force-dynamic';

export default function DashboardPage() {
  const fetchFreshDrops = React.useCallback(async (): Promise<Market[]> => {
    try {
      const { data } = await api.get<Market[]>("/markets/fresh");
      return data || [];
    } catch (error) {
      console.error("Failed to fetch fresh drops:", error);
      return [];
    }
  }, []);

  const fetchActiveMarkets = React.useCallback(async (): Promise<Market[]> => {
    try {
      const { data } = await api.get<Market[]>("/markets/active");
      return data || [];
    } catch (error) {
      console.error("Failed to fetch active markets:", error);
      return [];
    }
  }, []);

  type AssetPrice = {
    condition_id: string;
    price: number;
    best_bid: number;
    best_ask: number;
    timestamp: string;
  };

  const [assetPrices, setAssetPrices] = React.useState<Record<string, AssetPrice>>({});

  React.useEffect(() => {
    const streamUrl = `${API_BASE_URL}/api/v1/markets/stream`;
    const source = new EventSource(streamUrl);

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

        setAssetPrices((prev) => ({
          ...prev,
          [payload.asset_id]: {
            condition_id: payload.condition_id,
            price: payload.price,
            best_bid: payload.best_bid,
            best_ask: payload.best_ask,
            timestamp: payload.timestamp,
          },
        }));
      } catch (err) {
        console.error("Failed to parse price update:", err);
      }
    };

    source.onerror = () => {
      console.warn("SSE connection lost, retrying...");
    };

    return () => {
      source.close();
    };
  }, []);

  const augmentMarket = React.useCallback(
    (market: Market): Market => {
      const yes = assetPrices[market.token_id_yes];
      const no = assetPrices[market.token_id_no];

      return {
        ...market,
        yes_price: yes?.price ?? market.yes_price,
        yes_best_bid: yes?.best_bid ?? market.yes_best_bid,
        yes_best_ask: yes?.best_ask ?? market.yes_best_ask,
        no_price: no?.price ?? market.no_price,
        no_best_bid: no?.best_bid ?? market.no_best_bid,
        no_best_ask: no?.best_ask ?? market.no_best_ask,
      };
    },
    [assetPrices]
  );

  const { data: freshDropsData, isLoading: isLoadingFresh } = useQuery({
    queryKey: ["markets", "fresh"],
    queryFn: fetchFreshDrops,
    refetchInterval: 30000,
  });

  const { data: activeMarketsData, isLoading: isLoadingActive } = useQuery({
    queryKey: ["markets", "active"],
    queryFn: fetchActiveMarkets,
    refetchInterval: 60000,
  });

  const hydratedFreshDrops = React.useMemo(
    () => (freshDropsData || []).map(augmentMarket),
    [freshDropsData, augmentMarket]
  );

  const hydratedActiveMarkets = React.useMemo(
    () => (activeMarketsData || []).map(augmentMarket),
    [activeMarketsData, augmentMarket]
  );

  const isLoading = isLoadingFresh || isLoadingActive;

  if (isLoading) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="flex flex-col items-center gap-4">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="font-mono text-sm text-muted-foreground">Initializing Market Radar...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="container max-w-[1600px] py-6 space-y-6">
      <div className="flex flex-col space-y-2">
        <h1 className="text-3xl font-bold tracking-tight font-mono text-primary drop-shadow-[0_0_10px_rgba(41,121,255,0.5)]">
          MARKET RADAR
        </h1>
        <p className="text-muted-foreground">
          Real-time scanning of Polymarket liquidity events and fresh listings.
        </p>
      </div>

      {/* Market Ticker Component */}
      <MarketTicker 
        freshDrops={hydratedFreshDrops} 
        activeMarkets={hydratedActiveMarkets} 
      />
      
      {/* Additional Dashboard Widgets could go here (Whale Monitor, etc.) */}
    </div>
  );
}

