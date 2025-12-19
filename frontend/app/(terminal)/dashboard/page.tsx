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
import type { MarketLaneResponse } from "@/lib/market-data";
import { Button } from "@/components/ui/button";
import type { Market } from "@/types";

export const dynamic = "force-dynamic";

const emptyLanes: MarketLaneResponse = {
  fresh: [],
  high_velocity: [],
  deep_liquidity: [],
};

const emptyNewCounts = {
  fresh: 0,
  high_velocity: 0,
  deep_liquidity: 0,
};

const hasLaneItems = (lanes: MarketLaneResponse) =>
  lanes.fresh.length > 0 || lanes.high_velocity.length > 0 || lanes.deep_liquidity.length > 0;

const countNewMarkets = (prev: Market[], next: Market[]) => {
  if (prev.length === 0) {
    return 0;
  }
  const prevIds = new Set(prev.map((market) => market.condition_id));
  return next.reduce((count, market) => (prevIds.has(market.condition_id) ? count : count + 1), 0);
};

const computeNewCounts = (prev: MarketLaneResponse, next: MarketLaneResponse) => ({
  fresh: countNewMarkets(prev.fresh, next.fresh),
  high_velocity: countNewMarkets(prev.high_velocity, next.high_velocity),
  deep_liquidity: countNewMarkets(prev.deep_liquidity, next.deep_liquidity),
});

const mergeLane = (prev: Market[], next: Market[]): Market[] => {
  if (prev.length === 0) {
    return next;
  }

  const nextById = new Map(next.map((market) => [market.condition_id, market]));
  const prevIds = new Set(prev.map((market) => market.condition_id));
  const newItems = next.filter((market) => !prevIds.has(market.condition_id));
  const existingItems = prev
    .filter((market) => nextById.has(market.condition_id))
    .map((market) => nextById.get(market.condition_id) ?? market);

  return [...newItems, ...existingItems];
};

const mergeLanes = (prev: MarketLaneResponse, next: MarketLaneResponse): MarketLaneResponse => ({
  fresh: mergeLane(prev.fresh, next.fresh),
  high_velocity: mergeLane(prev.high_velocity, next.high_velocity),
  deep_liquidity: mergeLane(prev.deep_liquidity, next.deep_liquidity),
});

export default function DashboardPage() {
  const { hydrateMarkets } = usePriceStream();

  const { data: laneData, isLoading: isLoadingLanes } = useQuery({
    queryKey: ["markets", "lanes"],
    queryFn: () => fetchMarketLanes({ sort: "all" }),
    refetchInterval: 30_000,
  });

  const [mergedLanes, setMergedLanes] = React.useState<MarketLaneResponse>(emptyLanes);
  const [newCounts, setNewCounts] = React.useState(emptyNewCounts);
  const lanesRef = React.useRef<MarketLaneResponse>(emptyLanes);
  const clearNewCountsRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  React.useEffect(() => {
    return () => {
      if (clearNewCountsRef.current) {
        clearTimeout(clearNewCountsRef.current);
      }
    };
  }, []);

  React.useEffect(() => {
    if (!laneData) {
      return;
    }
    const incomingHasItems = hasLaneItems(laneData);
    const prev = lanesRef.current;
    if (!incomingHasItems && hasLaneItems(prev)) {
      return;
    }

    const merged = mergeLanes(prev, laneData);
    const counts = computeNewCounts(prev, laneData);
    const totalNew = counts.fresh + counts.high_velocity + counts.deep_liquidity;

    setMergedLanes(merged);
    lanesRef.current = merged;

    if (clearNewCountsRef.current) {
      clearTimeout(clearNewCountsRef.current);
      clearNewCountsRef.current = null;
    }

    if (totalNew > 0) {
      setNewCounts(counts);
      clearNewCountsRef.current = setTimeout(() => {
        setNewCounts(emptyNewCounts);
      }, 6_000);
    } else {
      setNewCounts(emptyNewCounts);
    }
  }, [laneData]);

  const hydratedFreshDrops = React.useMemo(
    () => hydrateMarkets(mergedLanes.fresh),
    [hydrateMarkets, mergedLanes.fresh]
  );
  const highVelocityMarkets = React.useMemo(
    () => hydrateMarkets(mergedLanes.high_velocity),
    [hydrateMarkets, mergedLanes.high_velocity]
  );
  const deepLiquidityMarkets = React.useMemo(
    () => hydrateMarkets(mergedLanes.deep_liquidity),
    [hydrateMarkets, mergedLanes.deep_liquidity]
  );

  const isLoading = isLoadingLanes && !hasLaneItems(mergedLanes);

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
        newCounts={newCounts}
      />

      {/* Additional widgets in future */}
    </div>
  );
}
