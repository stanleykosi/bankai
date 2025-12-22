"use client";

/**
 * @description
 * Positions Spy component - read-only view of trader's open positions.
 * Allows users to see what positions a trader holds for copy-trading.
 */

import Link from "next/link";
import { TrendingUp, TrendingDown, ExternalLink } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
      <Card className="border-border/50 bg-card/60">
        <CardHeader>
          <CardTitle className="text-lg">Open Positions</CardTitle>
        </CardHeader>
        <CardContent>
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
      <Card className="border-border/50 bg-card/60">
        <CardHeader>
          <CardTitle className="text-lg">Open Positions</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            This trader has no open positions.
          </p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-border/50 bg-card/60">
      <CardHeader>
        <CardTitle className="text-lg flex items-center justify-between">
          <span>Open Positions</span>
          <span className="text-sm font-normal text-muted-foreground">
            {positions.length} positions
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border/50 text-left text-xs uppercase tracking-wide text-muted-foreground">
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
                return (
                  <tr key={index} className="hover:bg-muted/20 transition-colors">
                    <td className="py-3">
                      <div className="max-w-[200px]">
                        <p className="truncate font-medium text-foreground">
                          {position.question || position.slug || "Unknown Market"}
                        </p>
                      </div>
                    </td>
                    <td className="py-3">
                      <span
                        className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${position.outcome === "Yes"
                            ? "bg-green-500/10 text-green-500"
                            : "bg-red-500/10 text-red-500"
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
                          <TrendingUp className="h-3 w-3 text-green-500" />
                        ) : (
                          <TrendingDown className="h-3 w-3 text-red-500" />
                        )}
                        <span
                          className={`font-mono ${isProfitable ? "text-green-500" : "text-red-500"
                            }`}
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
