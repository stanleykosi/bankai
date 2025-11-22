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
import { useQuery } from "@tanstack/react-query";
import { api, API_BASE_URL } from "@/lib/api";
import { MarketTicker } from "@/components/terminal/MarketTicker";
import { MarketFilters } from "@/components/terminal/MarketFilters";
import type { SortOption } from "@/components/terminal/MarketFilters";
import { Market } from "@/types";
import { Loader2 } from "lucide-react";

export const dynamic = "force-dynamic";

type ActiveMarketParams = Partial<{
  category: string;
  tag: string;
  sort: SortOption;
  limit: number;
  offset: number;
}>;

const fetchFreshDrops = async (): Promise<Market[]> => {
  try {
    const { data } = await api.get<Market[]>("/markets/fresh");
    return data || [];
  } catch (error) {
    console.error("Failed to fetch fresh drops:", error);
    return [];
  }
};

const fetchActiveMarkets = async (params: ActiveMarketParams = {}): Promise<Market[]> => {
  try {
    const requestParams: Record<string, string | number> = {};

    if (params.category) {
      requestParams.category = params.category;
    }
    if (params.tag) {
      requestParams.tag = params.tag;
    }

    const isAllPool = !params.sort || params.sort === "all";
    if (!isAllPool) {
      if (params.sort) {
        requestParams.sort = params.sort;
      }
      requestParams.limit = params.limit ?? 500;
    }

    const { data } = await api.get<Market[]>("/markets/active", { params: requestParams });
    return data || [];
  } catch (error) {
    console.error("Failed to fetch active markets:", error);
    return [];
  }
};

export default function DashboardPage() {
  const [filters, setFilters] = React.useState<ActiveMarketParams>({
    sort: "all",
  });

  const handleFilterChange = React.useCallback((update: ActiveMarketParams) => {
    setFilters((prev) => ({
      ...prev,
      ...update,
      offset: 0,
    }));
  }, []);

  const resetFilters = React.useCallback(() => {
    setFilters({ sort: "all" });
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

  const hydrateMarkets = React.useCallback(
    (markets?: Market[]) => (markets || []).map(augmentMarket),
    [augmentMarket]
  );

  const { data: freshDropsData, isLoading: isLoadingFresh } = useQuery({
    queryKey: ["markets", "fresh"],
    queryFn: fetchFreshDrops,
    refetchInterval: 30000,
  });

  const { data: activeMarketsData, isLoading: isLoadingActive } = useQuery({
    queryKey: ["markets", "active", filters],
    queryFn: () => fetchActiveMarkets(filters),
    refetchInterval: 60000,
  });

  const { data: masterActiveData } = useQuery({
    queryKey: ["markets", "active", "master"],
    queryFn: () => fetchActiveMarkets({ sort: "all" }),
    staleTime: 5 * 60 * 1000,
  });

  const categories = React.useMemo(() => {
    if (!masterActiveData) return [];
    const counts = new Map<string, number>();
    masterActiveData.forEach((market) => {
      if (!market.category) return;
      counts.set(market.category, (counts.get(market.category) || 0) + 1);
    });
    return Array.from(counts.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([value, count]) => ({ value, label: value, count }));
  }, [masterActiveData]);

  const tags = React.useMemo(() => {
    if (!masterActiveData) return [];
    const counts = new Map<string, number>();
    masterActiveData.forEach((market) => {
      market.tags?.forEach((tag) => {
        counts.set(tag, (counts.get(tag) || 0) + 1);
      });
    });
    return Array.from(counts.entries())
      .sort((a, b) => b[1] - a[1])
      .slice(0, 75)
      .map(([value, count]) => ({ value, label: value, count }));
  }, [masterActiveData]);

  const hydratedFreshDrops = React.useMemo(() => hydrateMarkets(freshDropsData), [freshDropsData, hydrateMarkets]);
  const hydratedActiveMarkets = React.useMemo(
    () => hydrateMarkets(activeMarketsData),
    [activeMarketsData, hydrateMarkets]
  );
  const hydratedMasterActive = React.useMemo(
    () => hydrateMarkets(masterActiveData),
    [masterActiveData, hydrateMarkets]
  );

  const usingAllSort = !filters.sort || filters.sort === "all";
  const hasTagOrCategory = Boolean(filters.category || filters.tag);

  const baseActivePool = React.useMemo(() => {
    if (usingAllSort && !hasTagOrCategory) {
      return hydratedMasterActive.length > 0 ? hydratedMasterActive : hydratedActiveMarkets;
    }
    return hydratedActiveMarkets;
  }, [hydratedActiveMarkets, hydratedMasterActive, hasTagOrCategory, usingAllSort]);

  const freshLaneMarkets = React.useMemo(() => {
    if (usingAllSort && !hasTagOrCategory) {
      return hydratedFreshDrops.slice(0, 20);
    }

    return [...baseActivePool]
      .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
      .slice(0, 20);
  }, [baseActivePool, hasTagOrCategory, hydratedFreshDrops, usingAllSort]);

  const highVelocityMarkets = React.useMemo(() => {
    return [...baseActivePool]
      .sort((a, b) => (b.volume_all_time ?? 0) - (a.volume_all_time ?? 0))
      .slice(0, 20);
  }, [baseActivePool]);

  const deepLiquidityMarkets = React.useMemo(() => {
    return [...baseActivePool].sort((a, b) => (b.liquidity ?? 0) - (a.liquidity ?? 0)).slice(0, 20);
  }, [baseActivePool]);

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

      <MarketFilters
        categories={categories}
        tags={tags}
        selectedCategory={filters.category}
        selectedTag={filters.tag}
        sort={(filters.sort as SortOption) || "all"}
        onChange={handleFilterChange}
        onReset={resetFilters}
      />

      <MarketTicker
        freshDrops={freshLaneMarkets}
        highVelocity={highVelocityMarkets}
        deepLiquidity={deepLiquidityMarkets}
      />

      {/* Additional widgets in future */}
    </div>
  );
}

