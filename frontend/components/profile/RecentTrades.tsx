"use client";

/**
 * @description
 * Recent Trades panel showing latest fills for the trader.
 * Renders side, outcome, size, price, and timestamp.
 */

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, TrendingDown, TrendingUp } from "lucide-react";

import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type { Trade } from "@/types";

interface RecentTradesProps {
  trades: Trade[] | undefined;
  isLoading?: boolean;
}

const PAGE_SIZE = 15;

const formatCurrency = (value: number): string => {
  if (Math.abs(value) >= 1_000_000) return `$${(value / 1_000_000).toFixed(2)}M`;
  if (Math.abs(value) >= 1_000) return `$${(value / 1_000).toFixed(1)}K`;
  return `$${value.toFixed(2)}`;
};

const formatPrice = (price: number): string => `${(price * 100).toFixed(1)}¢`;

const formatTimestamp = (timestamp: number): string => {
  if (!timestamp) return "--:--";
  const isMillis = timestamp > 1_000_000_000_000;
  const date = new Date(isMillis ? timestamp : timestamp * 1000);
  if (Number.isNaN(date.getTime())) return "--:--";
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
};

export function RecentTrades({ trades, isLoading }: RecentTradesProps) {
  const [page, setPage] = useState(1);

  useEffect(() => {
    if (!trades || trades.length === 0) {
      setPage(1);
      return;
    }
    const maxPage = Math.max(1, Math.ceil(trades.length / PAGE_SIZE));
    setPage((prev) => Math.min(prev, maxPage));
  }, [trades?.length]);

  if (isLoading) {
    return (
      <Card className="border-border/60 bg-card/70">
        <CardHeader className="pb-3">
          <div className="h-4 w-32 animate-pulse rounded bg-muted/50" />
        </CardHeader>
        <CardContent className="pt-0">
          <div className="space-y-2">
            {[...Array(5)].map((_, idx) => (
              <div key={idx} className="h-12 animate-pulse rounded bg-muted/30" />
            ))}
          </div>
        </CardContent>
      </Card>
    );
  }

  if (!trades || trades.length === 0) {
    return (
      <Card className="border-border/60 bg-card/70">
        <CardHeader className="pb-3">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Flow
            </p>
            <h3 className="text-lg font-semibold text-foreground">Recent Trades</h3>
          </div>
        </CardHeader>
        <CardContent className="pt-0">
          <p className="text-sm text-muted-foreground">No recent trades.</p>
        </CardContent>
      </Card>
    );
  }

  const totalTrades = trades.length;
  const totalPages = Math.max(1, Math.ceil(totalTrades / PAGE_SIZE));
  const startIndex = (page - 1) * PAGE_SIZE;
  const endIndex = Math.min(startIndex + PAGE_SIZE, totalTrades);
  const paginatedTrades = trades.slice(startIndex, endIndex);

  return (
    <Card className="border-border/60 bg-card/70">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Flow
            </p>
            <h3 className="text-lg font-semibold text-foreground">Recent Trades</h3>
          </div>
          <span className="text-xs font-mono text-muted-foreground">
            Showing {Math.min(startIndex + 1, totalTrades)}-
            {endIndex} of {totalTrades}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 pt-0">
        {paginatedTrades.map((trade) => {
          const isBuy = trade.side === "BUY";
          const isYes = trade.outcome?.toUpperCase() === "YES";
          return (
            <div
              key={trade.id}
              className="grid grid-cols-[auto,1fr,auto] items-center gap-3 rounded-lg border border-border/40 bg-background/40 p-3 hover:border-primary/40 transition-colors"
            >
              <div className="flex items-center gap-2">
                <span
                  className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-mono ${
                    isBuy
                      ? "border-emerald-400/40 bg-emerald-400/10 text-emerald-400"
                      : "border-rose-400/40 bg-rose-400/10 text-rose-400"
                  }`}
                >
                  {isBuy ? <TrendingUp className="h-3 w-3" /> : <TrendingDown className="h-3 w-3" />}
                  {trade.side}
                </span>
                <span
                  className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-mono ${
                    isYes
                      ? "border-emerald-400/40 text-emerald-400"
                      : "border-rose-400/40 text-rose-400"
                  }`}
                >
                  {trade.outcome}
                </span>
              </div>

              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-foreground">
                  {trade.title || trade.slug || "Unknown market"}
                </p>
                <div className="flex flex-wrap gap-3 text-xs text-muted-foreground">
                  <span className="font-mono">{formatPrice(trade.price)} @ {trade.size.toFixed(2)}</span>
                  <span className="font-mono">{formatCurrency(trade.value || trade.price * trade.size)}</span>
                  <span className="font-mono">{formatTimestamp(trade.timestamp)}</span>
                </div>
              </div>

              {trade.slug && (
                <Link
                  href={`/market/${trade.slug}`}
                  className="inline-flex items-center text-muted-foreground hover:text-primary transition-colors"
                >
                  <ArrowUpRight className="h-4 w-4" />
                </Link>
              )}
            </div>
          );
        })}
        <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
          <span className="font-mono">
            Page {page} / {totalPages}
          </span>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((prev) => Math.max(1, prev - 1))}
              disabled={page === 1}
              className="h-8 px-3"
            >
              Prev
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((prev) => Math.min(totalPages, prev + 1))}
              disabled={page === totalPages}
              className="h-8 px-3"
            >
              Next
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
