/**
 * @description
 * Visual Order Book component.
 * Displays current bids/asks for a specific outcome token and calculates spread.
 *
 * @dependencies
 * - @tanstack/react-query: For polling the public CLOB API.
 * - lucide-react: Loading indicator.
 * - utils: ClassName helper.
 */

"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

interface OrderLevel {
  price: string;
  size: string;
}

interface OrderBookResponse {
  bids: OrderLevel[];
  asks: OrderLevel[];
  hash?: string;
  asset_id?: string;
  market?: string;
}

interface ParsedLevel {
  price: number;
  size: number;
}

interface OrderBookProps {
  marketId: string;
  tokenId?: string | null;
  activeOutcome?: "YES" | "NO";
  tokenYesId?: string | null;
  tokenNoId?: string | null;
  onOutcomeChange?: (outcome: "YES" | "NO") => void;
  className?: string;
}

const compactCurrencyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  notation: "compact",
  maximumFractionDigits: 1,
});

const standardCurrencyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 2,
});

const formatCurrency = (value: number) => {
  if (!Number.isFinite(value)) return "--";
  if (Math.abs(value) >= 1000) return compactCurrencyFormatter.format(value);
  if (Math.abs(value) >= 1) return standardCurrencyFormatter.format(value);
  return `$${value.toFixed(4)}`;
};

const formatNumber = (value: number, decimals: number) => {
  if (!Number.isFinite(value)) return "--";
  return value.toFixed(decimals);
};

export function OrderBook({
  marketId,
  tokenId,
  activeOutcome,
  tokenYesId,
  tokenNoId,
  onOutcomeChange,
  className,
}: OrderBookProps) {
  const [localOutcome, setLocalOutcome] = useState<"YES" | "NO">("YES");
  const resolvedOutcome = activeOutcome ?? localOutcome;
  const resolvedTokenId =
    tokenId ??
    (resolvedOutcome === "YES" ? tokenYesId ?? null : tokenNoId ?? null);
  const showOutcomeToggle = Boolean(tokenYesId && tokenNoId);

  const handleOutcomeChange = (next: "YES" | "NO") => {
    if (!activeOutcome) {
      setLocalOutcome(next);
    }
    onOutcomeChange?.(next);
  };

  const { data, isLoading, isError, isFetching } = useQuery({
    queryKey: ["orderbook", marketId, resolvedTokenId],
    queryFn: async () => {
      if (!resolvedTokenId) return null;
      const res = await fetch(
        `https://clob.polymarket.com/book?token_id=${encodeURIComponent(
          resolvedTokenId
        )}`
      );
      if (!res.ok) {
        throw new Error("Failed to fetch order book");
      }
      return (await res.json()) as OrderBookResponse;
    },
    enabled: Boolean(resolvedTokenId),
    refetchInterval: 2000,
  });

  const { bids, asks, spread, spreadPercent } = useMemo(() => {
    if (!data) {
      return { bids: [] as ParsedLevel[], asks: [] as ParsedLevel[], spread: null as number | null, spreadPercent: null as number | null };
    }

    const parseLevels = (levels?: OrderLevel[]) =>
      (levels ?? [])
        .map((level) => ({
          price: Number.parseFloat(level.price),
          size: Number.parseFloat(level.size),
        }))
        .filter((level) => Number.isFinite(level.price) && level.price > 0 && Number.isFinite(level.size) && level.size > 0);

    const parsedBids = parseLevels(data.bids)
      .sort((a, b) => b.price - a.price)
      .slice(0, 15);
    const parsedAsks = parseLevels(data.asks)
      .sort((a, b) => a.price - b.price)
      .slice(0, 15);

    const bestBid = parsedBids[0]?.price ?? 0;
    const bestAsk = parsedAsks[0]?.price ?? 0;
    const computedSpread =
      bestBid > 0 && bestAsk > 0 ? bestAsk - bestBid : null;
    const computedSpreadPercent =
      computedSpread !== null && bestAsk > 0
        ? (computedSpread / bestAsk) * 100
        : null;

    return {
      bids: parsedBids,
      asks: parsedAsks,
      spread: computedSpread,
      spreadPercent: computedSpreadPercent,
    };
  }, [data]);

  const maxSize = useMemo(() => {
    const bidMax = bids.length ? Math.max(...bids.map((bid) => bid.size)) : 0;
    const askMax = asks.length ? Math.max(...asks.map((ask) => ask.size)) : 0;
    return Math.max(bidMax, askMax, 1);
  }, [asks, bids]);

  if (!resolvedTokenId) {
    return (
      <Card
        className={cn(
          "flex h-full flex-col overflow-hidden border-border bg-card/60 backdrop-blur",
          className
        )}
      >
        <CardHeader className="flex flex-row items-center justify-between border-b border-border/50 px-4 py-3">
          <CardTitle className="text-sm font-mono uppercase tracking-widest text-muted-foreground">
            Order Book
          </CardTitle>
        </CardHeader>
        <CardContent className="flex flex-1 items-center justify-center p-6 text-xs text-muted-foreground">
          Order book unavailable
        </CardContent>
      </Card>
    );
  }

  return (
    <Card
      className={cn(
        "flex h-full flex-col overflow-hidden border-border bg-card/60 backdrop-blur",
        className
      )}
    >
      <CardHeader className="flex flex-row items-center justify-between border-b border-border/50 px-4 py-3">
        <CardTitle className="flex items-center gap-2 text-sm font-mono uppercase tracking-widest text-muted-foreground">
          <span
            className={cn(
              "h-2 w-2 rounded-full",
              resolvedOutcome === "YES" ? "bg-constructive" : "bg-destructive"
            )}
          />
          Order Book
        </CardTitle>
        <div className="flex items-center gap-2">
          {showOutcomeToggle && (
            <div className="flex items-center rounded-full border border-border bg-background/50 p-0.5 text-[10px] font-mono">
              {(["YES", "NO"] as const).map((label) => {
                const isActive = resolvedOutcome === label;
                return (
                  <button
                    key={label}
                    type="button"
                    onClick={() => handleOutcomeChange(label)}
                    className={cn(
                      "rounded-full px-2.5 py-1 uppercase tracking-wide transition-colors",
                      isActive
                        ? label === "YES"
                          ? "bg-constructive text-black"
                          : "bg-destructive text-white"
                        : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    {label}
                  </button>
                );
              })}
            </div>
          )}
          {isFetching && (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          )}
        </div>
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col p-0">
        <div className="grid grid-cols-3 border-b border-border/50 bg-background/40 px-4 py-1 text-[10px] font-mono uppercase text-muted-foreground">
          <div className="text-left">Size</div>
          <div className="text-center">Price</div>
          <div className="text-right">Total</div>
        </div>

        {isError ? (
          <div className="flex flex-1 items-center justify-center p-6 text-xs text-destructive">
            Failed to load order book
          </div>
        ) : (
          <div className="grid flex-1 min-h-0 grid-rows-[minmax(0,1fr)_auto_minmax(0,1fr)]">
            <div className="min-h-0 overflow-hidden">
              <div className="custom-scrollbar flex h-full flex-col-reverse overflow-y-auto">
                {asks.length === 0 ? (
                  <div className="flex h-full items-center justify-center text-[10px] text-muted-foreground">
                    {isLoading ? "Loading asks..." : "No asks available"}
                  </div>
                ) : (
                  asks.map((ask, index) => (
                    <OrderRow
                      key={`ask-${index}`}
                      price={ask.price}
                      size={ask.size}
                      maxSize={maxSize}
                      type="ask"
                    />
                  ))
                )}
              </div>
            </div>

            <div className="flex items-center justify-between border-y border-border/50 bg-background/60 px-4 py-1 text-[11px] font-mono">
              <div className="flex items-center gap-1 text-muted-foreground">
                <span>Spread</span>
                <span className="text-[10px] text-muted-foreground/60">
                  {spreadPercent === null
                    ? "--"
                    : `(${spreadPercent.toFixed(2)}%)`}
                </span>
              </div>
              <div
                className={cn(
                  "font-semibold",
                  typeof spread === "number" && spread < 0.02
                    ? "text-constructive"
                    : "text-amber-400"
                )}
              >
                {typeof spread === "number" ? formatCurrency(spread) : "--"}
              </div>
            </div>

            <div className="min-h-0 overflow-hidden">
              <div className="custom-scrollbar h-full overflow-y-auto">
                {bids.length === 0 ? (
                  <div className="flex h-full items-center justify-center text-[10px] text-muted-foreground">
                    {isLoading ? "Loading bids..." : "No bids available"}
                  </div>
                ) : (
                  bids.map((bid, index) => (
                    <OrderRow
                      key={`bid-${index}`}
                      price={bid.price}
                      size={bid.size}
                      maxSize={maxSize}
                      type="bid"
                    />
                  ))
                )}
              </div>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

interface OrderRowProps {
  price: number;
  size: number;
  maxSize: number;
  type: "bid" | "ask";
}

function OrderRow({ price, size, maxSize, type }: OrderRowProps) {
  const depthWidth = `${Math.min((size / maxSize) * 100, 100)}%`;
  const total = price * size;

  return (
    <div className="relative grid grid-cols-3 px-4 py-[3px] text-[11px] font-mono hover:bg-muted/20">
      <div
        className={cn(
          "absolute inset-y-0 right-0 z-0 opacity-10",
          type === "bid" ? "bg-constructive" : "bg-destructive"
        )}
        style={{ width: depthWidth }}
      />
      <div className="relative z-10 text-left text-foreground/80">
        {formatNumber(size, 2)}
      </div>
      <div
        className={cn(
          "relative z-10 text-center font-semibold",
          type === "bid" ? "text-constructive" : "text-destructive"
        )}
      >
        {formatNumber(price, 3)}
      </div>
      <div className="relative z-10 text-right text-muted-foreground">
        {formatCurrency(total)}
      </div>
    </div>
  );
}
