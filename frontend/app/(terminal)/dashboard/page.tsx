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

'use client';

import React from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { MarketTicker } from "@/components/terminal/MarketTicker";
import { usePriceStream } from "@/hooks/usePriceStream";
import { fetchMarketLanes } from "@/lib/market-data";
import { Button } from "@/components/ui/button";

export const dynamic = "force-dynamic";

export default function DashboardPage() {
  const { hydrateMarkets } = usePriceStream();

  const { data: laneData, isLoading: isLoadingLanes } = useQuery({
    queryKey: ["markets", "lanes"],
    queryFn: () => fetchMarketLanes({ sort: "all" }),
    refetchInterval: 30_000,
  });

  const hydratedFreshDrops = React.useMemo(
    () => hydrateMarkets(laneData?.fresh ?? []),
    [hydrateMarkets, laneData?.fresh]
  );
  const highVelocityMarkets = React.useMemo(
    () => hydrateMarkets(laneData?.high_velocity ?? []),
    [hydrateMarkets, laneData?.high_velocity]
  );
  const deepLiquidityMarkets = React.useMemo(
    () => hydrateMarkets(laneData?.deep_liquidity ?? []),
    [hydrateMarkets, laneData?.deep_liquidity]
  );

  const isLoading = isLoadingLanes;

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
        <div>
          <Button asChild size="sm" variant="outline" className="font-mono text-xs tracking-wide">
            <Link href="/markets">Browse All Markets ↗</Link>
          </Button>
        </div>
      </div>

      <MarketTicker
        freshDrops={hydratedFreshDrops}
        highVelocity={highVelocityMarkets}
        deepLiquidity={deepLiquidityMarkets}
      />

      {/* Additional widgets in future */}
    </div>
  );
}

