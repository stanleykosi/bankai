/**
 * @description
 * All Markets page.
 * Provides a paginated table view with full sorting/filter controls.
 */
"use client";

import * as React from "react";
import Link from "next/link";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { MarketFilters } from "@/components/terminal/MarketFilters";
import type { SortOption } from "@/components/terminal/MarketFilters";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { usePriceStream } from "@/hooks/usePriceStream";
import {
  fetchMarketMeta,
  fetchMarketsPage,
  type ActiveMarketParams,
} from "@/lib/market-data";
import type { Market } from "@/types";

const PAGE_SIZE = 200;

const formatCurrency = (value?: number) => {
  if (!value || Number.isNaN(value)) return "—";
  if (value >= 1_000_000_000) {
    return `$${(value / 1_000_000_000).toFixed(2)}B`;
  }
  if (value >= 1_000_000) {
    return `$${(value / 1_000_000).toFixed(2)}M`;
  }
  if (value >= 1_000) {
    return `$${(value / 1_000).toFixed(2)}K`;
  }
  return `$${value.toFixed(2)}`;
};

const formatCents = (value?: number) => {
  if (typeof value !== "number" || Number.isNaN(value)) return "—";
  return `${(value * 100).toFixed(1)}¢`;
};

const formatDate = (iso?: string) => {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
};

const parseOutcomes = (outcomes?: string): string[] => {
  if (!outcomes) return [];
  try {
    const parsed = JSON.parse(outcomes);
    if (Array.isArray(parsed)) {
      return parsed.map((value) => String(value));
    }
  } catch {
    // ignore malformed JSON
  }
  return [];
};

export default function MarketsPage() {
  const { hydrateMarkets } = usePriceStream();
  const [filters, setFilters] = React.useState<ActiveMarketParams>({
    sort: "all",
  });
  const [sortVersion, setSortVersion] = React.useState(0);

  const handleFilterChange = React.useCallback((update: { category?: string; tag?: string; sort?: SortOption }) => {
    setFilters((prev) => {
      const next = { ...prev, ...update };
      if (update.sort && update.sort !== prev.sort) {
        setSortVersion((v) => v + 1);
      }
      if (!update.sort && prev.sort === "all" && update.category === undefined && update.tag === undefined) {
        setSortVersion((v) => v + 1);
      }
      return next;
    });
  }, []);

  const resetFilters = React.useCallback(() => {
    setFilters({ sort: "all" });
    setSortVersion((v) => v + 1);
  }, []);

  const { data: metaData } = useQuery({
    queryKey: ["markets", "meta"],
    queryFn: fetchMarketMeta,
    staleTime: 5 * 60 * 1000,
  });

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, status } = useInfiniteQuery({
    queryKey: ["markets", "list", filters, sortVersion],
    queryFn: ({ pageParam = 0 }) => fetchMarketsPage(filters, PAGE_SIZE, pageParam),
    initialPageParam: 0,
    refetchInterval: 60_000,
    getNextPageParam: (lastPage) =>
      lastPage.nextOffset < lastPage.total ? lastPage.nextOffset : undefined,
  });

  const markets = React.useMemo(() => {
    const pages = data?.pages ?? [];
    const flattened = pages.flatMap((page) => page.markets);
    return hydrateMarkets(flattened);
  }, [data?.pages, hydrateMarkets]);

  const totalMarkets = React.useMemo(() => {
    if (data?.pages?.length) {
      const last = data.pages[data.pages.length - 1];
      return last.total;
    }
    return metaData?.total ?? markets.length;
  }, [data?.pages, metaData?.total, markets.length]);

  const categories =
    metaData?.categories?.map((entry) => ({
      value: entry.value,
      label: entry.value,
      count: entry.count,
    })) ?? [];

  const tags =
    metaData?.tags?.map((entry) => ({
      value: entry.value,
      label: entry.value,
      count: entry.count,
    })) ?? [];

  const isLoadingInitial = status === "pending";

  return (
    <div className="container max-w-[1600px] py-6 space-y-6">
      <div className="flex flex-col space-y-2">
        <h1 className="text-3xl font-bold tracking-tight font-mono text-primary drop-shadow-[0_0_10px_rgba(41,121,255,0.5)]">
          ALL ACTIVE MARKETS
        </h1>
        <p className="text-muted-foreground">
          Browse the complete Polymarket universe with live prices, deep filtering, and infinite scroll.
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

      <div className="rounded-lg border border-border bg-card/60 shadow-lg shadow-black/20">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3 text-xs uppercase tracking-wide text-muted-foreground">
          <span>
            Showing{" "}
            <strong className="text-primary">{markets.length.toLocaleString()}</strong> of{" "}
            <strong className="text-primary">{totalMarkets.toLocaleString()}</strong> markets
          </span>
          <span>Page size {PAGE_SIZE}</span>
        </div>

        {isLoadingInitial ? (
          <div className="flex h-[400px] items-center justify-center">
            <Loader2 className="h-6 w-6 animate-spin text-primary" />
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Market</TableHead>
                <TableHead>Outcomes</TableHead>
                <TableHead>All-Time Volume</TableHead>
                <TableHead>24h Volume</TableHead>
                <TableHead>Liquidity</TableHead>
                <TableHead>Spread</TableHead>
                <TableHead>Start</TableHead>
                <TableHead>End</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {markets.map((market) => (
                <TableRow key={market.condition_id}>
                  <TableCell className="space-y-1">
                    <div className="flex items-center gap-3">
                      {market.image_url || market.icon_url ? (
                        <div className="h-10 w-10 rounded-md overflow-hidden border border-border/40 bg-background/40">
                          <img
                            src={market.image_url || market.icon_url || ""}
                            alt={market.title}
                            className="h-full w-full object-cover"
                            loading="lazy"
                          />
                        </div>
                      ) : null}
                      <div className="flex-1">
                        <Link
                          href={`/market/${market.slug}`}
                          className="font-semibold text-primary hover:underline"
                        >
                          {market.title}
                        </Link>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    <div className="flex flex-col gap-1">
                      {(() => {
                        const labels = parseOutcomes(market.outcomes);
                        const entries = [];
                        if (labels.length > 0) {
                          entries.push({ label: labels[0], price: market.yes_price, color: "text-constructive" });
                        }
                        if (labels.length > 1) {
                          entries.push({ label: labels[1], price: market.no_price, color: "text-destructive" });
                        }
                        if (!entries.length) {
                          entries.push({ label: "Outcome A", price: market.yes_price, color: "text-constructive" });
                          entries.push({ label: "Outcome B", price: market.no_price, color: "text-destructive" });
                        }
                        labels.slice(2).forEach((label) => {
                          entries.push({ label, price: undefined, color: "text-muted-foreground" });
                        });
                        return entries.slice(0, 4).map((entry) => (
                          <div key={`${market.condition_id}-${entry.label}`} className="flex justify-between gap-2">
                            <span className="truncate">{entry.label}</span>
                            <span className={`font-mono ${entry.color}`}>{formatCents(entry.price)}</span>
                          </div>
                        ));
                      })()}
                    </div>
                  </TableCell>
                  <TableCell>{formatCurrency(market.volume_all_time ?? market.volume_24h)}</TableCell>
                  <TableCell>{formatCurrency(market.volume_24h)}</TableCell>
                  <TableCell>{formatCurrency(market.liquidity)}</TableCell>
                  <TableCell>{formatCents(market.spread)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatDate(market.start_date)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatDate(market.end_date)}</TableCell>
                </TableRow>
              ))}
              {!markets.length && (
                <TableRow>
                  <TableCell colSpan={9} className="text-center text-muted-foreground">
                    No markets match the selected filters.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        )}

        <div className="flex items-center justify-between border-t border-border px-4 py-3">
          <span className="text-xs text-muted-foreground">
            Updated {new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
          </span>
          <Button
            variant="outline"
            disabled={!hasNextPage || isFetchingNextPage}
            onClick={() => fetchNextPage()}
          >
            {isFetchingNextPage ? (
              <span className="inline-flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Loading…
              </span>
            ) : hasNextPage ? (
              "Load more"
            ) : (
              "All markets loaded"
            )}
          </Button>
        </div>
      </div>
    </div>
  );
}

