"use client";

/**
 * @description
 * Positions Spy component - read-only view of trader's open positions.
 * Allows users to see what positions a trader holds for copy-trading.
 */

import Link from "next/link";
import { TrendingUp, TrendingDown, ExternalLink } from "lucide-react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import type { Position } from "@/types";

interface PositionsSpyProps {
  positions: Position[] | undefined;
  isLoading?: boolean;
}

function formatCurrency(value: number): string {
  if (Math.abs(value) >= 1_000_000) {
    return `$${(value / 1_000_000).toFixed(2)}M`;
  }
  if (Math.abs(value) >= 1_000) {
    return `$${(value / 1_000).toFixed(1)}K`;
  }
  return `$${value.toFixed(2)}`;
}

function formatPercent(value: number): string {
  const sign = value >= 0 ? "+" : "";
  return `${sign}${value.toFixed(2)}%`;
}

export function PositionsSpy({ positions, isLoading }: PositionsSpyProps) {
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
            This trader has no open positions.
          </p>
        </CardContent>
      </Card>
    );
  }

  const totalValue = positions.reduce((sum, position) => sum + position.currentValue, 0);
  const totalPnL = positions.reduce((sum, position) => sum + position.cashPnl, 0);
  const totalPnLColor = totalPnL >= 0 ? "text-emerald-400" : "text-rose-400";

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
              <span className="text-muted-foreground">Total Value</span>
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
                <th className="pb-2 text-right">Value</th>
                <th className="pb-2 text-right">PnL</th>
                <th className="pb-2"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/30">
              {positions.map((position, index) => {
                const isProfitable = position.cashPnl >= 0;
                const isYes = position.outcome?.toLowerCase() === "yes";
                return (
                  <tr key={index} className="hover:bg-muted/20 transition-colors">
                    <td className="py-3">
                      <div className="max-w-[200px]">
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
                      ${position.avgPrice.toFixed(2)}
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
      </CardContent>
    </Card>
  );
}
