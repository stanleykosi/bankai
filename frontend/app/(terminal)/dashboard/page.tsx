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
    const { data } = await api.get<Market[]>("/markets/active", {
      params: {
        limit: params.limit ?? 200,
        ...params,
      },
    });
    return data || [];
  } catch (error) {
    console.error("Failed to fetch active markets:", error);
    return [];
  }
};

export default function DashboardPage() {
  const [filters, setFilters] = React.useState<ActiveMarketParams>({
    sort: "volume",
    limit: 200,
  });

  const handleFilterChange = React.useCallback((update: ActiveMarketParams) => {
    setFilters((prev) => ({
      ...prev,
      ...update,
      offset: 0,
    }));
  }, []);

  const resetFilters = React.useCallback(() => {
    setFilters({ sort: "volume", limit: 200 });
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
    queryFn: () => fetchActiveMarkets({ limit: 500, sort: "volume" }),
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
        sort={(filters.sort as SortOption) || "volume"}
        onChange={handleFilterChange}
        onReset={resetFilters}
      />

      <MarketTicker freshDrops={hydratedFreshDrops} activeMarkets={hydratedActiveMarkets} />

      {/* Additional widgets in future */}
    </div>
  );
}

