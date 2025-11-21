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
import { api } from "@/lib/api";
import { MarketTicker } from "@/components/terminal/MarketTicker";
import { Market } from "@/types";
import { Loader2 } from "lucide-react";

// Fetch functions
const fetchFreshDrops = async (): Promise<Market[]> => {
  const { data } = await api.get<Market[]>("/markets/fresh");
  return data;
};

const fetchActiveMarkets = async (): Promise<Market[]> => {
  const { data } = await api.get<Market[]>("/markets/active");
  return data;
};

export default function DashboardPage() {
  const { data: freshDrops, isLoading: isLoadingFresh } = useQuery({
    queryKey: ["markets", "fresh"],
    queryFn: fetchFreshDrops,
    refetchInterval: 30000, // Poll every 30s for new drops
  });

  const { data: activeMarkets, isLoading: isLoadingActive } = useQuery({
    queryKey: ["markets", "active"],
    queryFn: fetchActiveMarkets,
    refetchInterval: 60000, // Poll every 60s for volume updates
  });

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
        freshDrops={freshDrops || []} 
        activeMarkets={activeMarkets || []} 
      />
      
      {/* Additional Dashboard Widgets could go here (Whale Monitor, etc.) */}
    </div>
  );
}

