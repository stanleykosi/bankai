"use client";

import { useMemo } from "react";
import { AlertCircle, Loader2, RefreshCw } from "lucide-react";

import { useFills } from "@/hooks/useFills";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const formatNumber = (value: number) => {
  if (Number.isNaN(value)) return "--";
  return value.toFixed(2);
};

const formatPrice = (value: number) => {
  if (Number.isNaN(value)) return "--";
  return value.toFixed(3);
};

const formatDate = (input: string) => {
  if (!input) return "--";
  return new Date(input).toLocaleString();
};

export function FillsPanel() {
  const { fills, isLoading, error, refresh, isFetching } = useFills(true);

  const sortedFills = useMemo(
    () =>
      [...fills].sort(
        (a, b) => Date.parse(b.matched_at) - Date.parse(a.matched_at)
      ),
    [fills]
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground">
          Recent Fills
        </p>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => refresh()}
          disabled={isFetching}
          className="h-8 w-8"
        >
          {isFetching ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <RefreshCw className="h-4 w-4" />
          )}
        </Button>
      </div>

      {error && (
        <div className="flex items-center gap-2 rounded border border-destructive/30 bg-destructive/5 p-3 text-destructive">
          <AlertCircle className="h-4 w-4" />
          <p className="text-xs font-mono">{error.message}</p>
        </div>
      )}

      {sortedFills.length === 0 && isLoading ? (
        <div className="flex items-center justify-center gap-2 rounded-md border border-border/60 py-6 text-xs font-mono text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading fills...
        </div>
      ) : sortedFills.length === 0 ? (
        <p className="text-center text-xs font-mono text-muted-foreground">
          No fills yet.
        </p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-border/60">
          <table className="w-full min-w-[600px] table-fixed text-sm">
            <thead className="bg-muted/20 text-[11px] font-mono uppercase tracking-wider text-muted-foreground">
              <tr>
                <th className="px-3 py-2 text-left">Outcome</th>
                <th className="px-3 py-2 text-left">Side</th>
                <th className="px-3 py-2 text-left">Price</th>
                <th className="px-3 py-2 text-left">Size</th>
                <th className="px-3 py-2 text-left">Market</th>
                <th className="px-3 py-2 text-left">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/60">
              {sortedFills.map((fill) => (
                <tr key={fill.id} className="text-[13px]">
                  <td className="px-3 py-2 font-mono text-xs">
                    {fill.outcome || "—"}
                  </td>
                  <td className="px-3 py-2 font-mono">
                    <span
                      className={cn(
                        "rounded px-2 py-0.5 text-[10px] font-bold uppercase",
                        fill.side === "BUY"
                          ? "bg-constructive/10 text-constructive"
                          : "bg-destructive/10 text-destructive"
                      )}
                    >
                      {fill.side}
                    </span>
                  </td>
                  <td className="px-3 py-2 font-mono">
                    ${formatPrice(fill.price)}
                  </td>
                  <td className="px-3 py-2 font-mono">
                    {formatNumber(fill.size)}
                  </td>
                  <td className="px-3 py-2 font-mono text-[10px] text-muted-foreground">
                    {fill.market_id ?? "Unlinked"}
                  </td>
                  <td className="px-3 py-2 font-mono text-[10px] text-muted-foreground">
                    {formatDate(fill.matched_at)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
