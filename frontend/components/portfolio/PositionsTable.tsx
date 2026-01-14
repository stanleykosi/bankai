"use client";

/**
 * @description
 * Portfolio positions table for the connected wallet.
 * Shows mark-to-market value and PnL for open positions.
 */

import { useEffect, useState } from "react";
import Link from "next/link";
import { ExternalLink, TrendingDown, TrendingUp } from "lucide-react";

import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type { Position } from "@/types";

interface PositionsTableProps {
  positions: Position[] | undefined;
  isLoading?: boolean;
  error?: string | null;
}

const PAGE_SIZE = 12;

const formatCurrency = (value: number): string => {
  if (!Number.isFinite(value)) return "$0.00";
  if (Math.abs(value) >= 1_000_000) {
    return `$${(value / 1_000_000).toFixed(2)}M`;
  }
  if (Math.abs(value) >= 1_000) {
    return `$${(value / 1_000).toFixed(1)}K`;
  }
  return `$${value.toFixed(2)}`;
};

const formatPercent = (value: number): string => {
  if (!Number.isFinite(value)) return "0.00%";
  const sign = value >= 0 ? "+" : "";
  return `${sign}${value.toFixed(2)}%`;
};

export function PositionsTable({
  positions,
  isLoading,
  error,
}: PositionsTableProps) {
  const [page, setPage] = useState(1);

  useEffect(() => {
    if (!positions) {
      setPage(1);
      return;
    }
    const maxPage = Math.max(1, Math.ceil(positions.length / PAGE_SIZE));
    setPage((prev) => Math.min(prev, maxPage));
  }, [positions?.length]);

  if (isLoading) {
    return (
      <Card className="border-border/60 bg-card/70">
        <CardHeader className="pb-3">
          <div className="h-4 w-32 animate-pulse rounded bg-muted/50" />
        </CardHeader>
        <CardContent className="pt-0">
          <div className="space-y-3">
            {[...Array(5)].map((_, i) => (
              <div key={i} className="h-16 animate-pulse rounded bg-muted/30" />
            ))}
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className="border-border/60 bg-card/70">
        <CardHeader className="pb-3">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Positions
            </p>
            <h3 className="text-lg font-semibold text-foreground">Open Positions</h3>
          </div>
        </CardHeader>
        <CardContent className="pt-0 text-sm text-destructive">
          {error}
        </CardContent>
      </Card>
    );
  }

  if (!positions || positions.length === 0) {
    return (
      <Card className="border-border/60 bg-card/70">
        <CardHeader className="pb-3">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Positions
            </p>
            <h3 className="text-lg font-semibold text-foreground">Open Positions</h3>
          </div>
        </CardHeader>
        <CardContent className="pt-0">
          <p className="text-sm text-muted-foreground">
            No open positions found for this wallet.
          </p>
        </CardContent>
      </Card>
    );
  }

  const totalValue = positions.reduce(
    (sum, position) => sum + position.currentValue,
    0
  );
  const totalPnL = positions.reduce((sum, position) => sum + position.cashPnl, 0);
  const totalPnLColor = totalPnL >= 0 ? "text-emerald-400" : "text-rose-400";

  const totalPositions = positions.length;
  const totalPages = Math.max(1, Math.ceil(totalPositions / PAGE_SIZE));
  const startIndex = (page - 1) * PAGE_SIZE;
  const endIndex = Math.min(startIndex + PAGE_SIZE, totalPositions);
  const paginatedPositions = positions.slice(startIndex, endIndex);

  return (
    <Card className="border-border/60 bg-card/70">
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Positions
            </p>
            <h3 className="text-lg font-semibold text-foreground">Open Positions</h3>
          </div>
          <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
            <span className="font-mono">{positions.length} positions</span>
            <span className="inline-flex items-center gap-2 rounded-full border border-border/60 bg-background/40 px-3 py-1 font-mono">
              <span className="text-muted-foreground">MTM Value</span>
              <span className="text-foreground">{formatCurrency(totalValue)}</span>
            </span>
            <span className="inline-flex items-center gap-2 rounded-full border border-border/60 bg-background/40 px-3 py-1 font-mono">
              <span className="text-muted-foreground">Unrealized</span>
              <span className={totalPnLColor}>{formatCurrency(totalPnL)}</span>
            </span>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border/60 text-left text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
                <th className="pb-2">Market</th>
                <th className="pb-2">Side</th>
                <th className="pb-2 text-right">Size</th>
                <th className="pb-2 text-right">Avg Price</th>
                <th className="pb-2 text-right">Current</th>
                <th className="pb-2 text-right">Value</th>
                <th className="pb-2 text-right">PnL</th>
                <th className="pb-2"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/30">
              {paginatedPositions.map((position, index) => {
                const isProfitable = position.cashPnl >= 0;
                const isYes = position.outcome?.toLowerCase() === "yes";
                return (
                  <tr key={index} className="hover:bg-muted/20 transition-colors">
                    <td className="py-3">
                      <div className="max-w-[220px]">
                        <p className="truncate font-medium text-foreground">
                          {position.title || position.slug || position.asset || "Unknown Market"}
                        </p>
                      </div>
                    </td>
                    <td className="py-3">
                      <span
                        className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium ${
                          isYes
                            ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
                            : "border-rose-500/30 bg-rose-500/10 text-rose-400"
                        }`}
                      >
                        {position.outcome}
                      </span>
                    </td>
                    <td className="py-3 text-right font-mono">
                      {position.size.toFixed(2)}
                    </td>
                    <td className="py-3 text-right font-mono">
                      ${position.avgPrice.toFixed(3)}
                    </td>
                    <td className="py-3 text-right font-mono">
                      ${position.curPrice.toFixed(3)}
                    </td>
                    <td className="py-3 text-right font-mono">
                      {formatCurrency(position.currentValue)}
                    </td>
                    <td className="py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {isProfitable ? (
                          <TrendingUp className="h-3 w-3 text-emerald-400" />
                        ) : (
                          <TrendingDown className="h-3 w-3 text-rose-400" />
                        )}
                        <span
                          className={`font-mono ${isProfitable ? "text-emerald-400" : "text-rose-400"}`}
                        >
                          {formatCurrency(position.cashPnl)}
                        </span>
                        <span
                          className={`text-xs ${isProfitable ? "text-emerald-300" : "text-rose-300"}`}
                        >
                          {formatPercent(position.percentPnl)}
                        </span>
                      </div>
                    </td>
                    <td className="py-3 text-right">
                      {position.slug && (
                        <Link
                          href={`/market/${position.slug}`}
                          className="inline-flex items-center text-muted-foreground hover:text-primary transition-colors"
                        >
                          <ExternalLink className="h-3.5 w-3.5" />
                        </Link>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <div className="mt-4 flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
          <span className="font-mono">
            Showing {startIndex + 1}-{endIndex} of {totalPositions}
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
            <span className="font-mono text-foreground">
              Page {page} / {totalPages}
            </span>
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
