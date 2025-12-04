"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { Loader2, ArrowLeft, RefreshCcw } from "lucide-react";

import { TradeForm } from "@/components/terminal/TradeForm";
import { OrdersPanel } from "@/components/terminal/OrdersPanel";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { fetchMarketBySlug, requestMarketStream } from "@/lib/market-data";
import { cn } from "@/lib/utils";

export default function MarketDetailPage() {
  const params = useParams<{ slug: string }>();
  const slug = params?.slug;
  const streamRequestedRef = useRef<string | null>(null);

  const {
    data: market,
    isLoading,
    isError,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ["market-detail", slug],
    queryFn: () => fetchMarketBySlug(String(slug)),
    enabled: Boolean(slug),
  });

  const loadingState = isLoading || !slug;

  useEffect(() => {
    const conditionId = market?.condition_id;
    if (!conditionId) {
      return;
    }
    if (streamRequestedRef.current === conditionId) {
      return;
    }
    streamRequestedRef.current = conditionId;
    requestMarketStream(conditionId).catch((err) => {
      if (process.env.NODE_ENV !== "production") {
        console.warn("Failed to request live stream for market", conditionId, err);
      }
    });
  }, [market?.condition_id]);

  if (loadingState) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="flex flex-col items-center gap-3 font-mono text-sm text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
          <p>Loading market...</p>
        </div>
      </div>
    );
  }

  if (isError || !market) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="flex flex-col items-center gap-4 text-center">
          <p className="font-mono text-sm text-destructive">
            Unable to load this market.
          </p>
          <Button
            variant="outline"
            onClick={() => refetch()}
            className="flex items-center gap-2"
          >
            <RefreshCcw className="h-4 w-4" />
            Retry
          </Button>
          <Button asChild variant="ghost">
            <Link href="/markets">Back to Markets</Link>
          </Button>
        </div>
      </div>
    );
  }

  const outcomes = (() => {
    if (!market.outcomes) {
      return ["Yes", "No"];
    }
    try {
      const parsed = JSON.parse(market.outcomes);
      if (Array.isArray(parsed) && parsed.length) {
        return parsed.map((value: unknown) => String(value));
      }
    } catch {
      // ignore parsing error
    }
    return ["Yes", "No"];
  })();

  return (
    <div className="container max-w-6xl py-6 space-y-6">
      <div className="flex items-center justify-between">
        <Button asChild variant="ghost" className="font-mono text-xs gap-2">
          <Link href="/markets">
            <ArrowLeft className="h-4 w-4" />
            Markets
          </Link>
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="font-mono text-xs"
          onClick={() => refetch()}
          disabled={isFetching}
        >
          {isFetching ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <RefreshCcw className="h-4 w-4" />
          )}
          <span className="ml-2">Refresh</span>
        </Button>
      </div>

      <div className="grid gap-6 lg:grid-cols-[2fr,1fr]">
        <Card className="border-border bg-card/60 backdrop-blur">
          <CardContent className="p-6 space-y-4">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                {market.tags?.slice(0, 4).map((tag) => (
                  <span
                    key={tag}
                    className="rounded-full border border-border px-2 py-0.5 text-[10px] font-mono uppercase tracking-wide text-muted-foreground"
                  >
                    {tag}
                  </span>
                ))}
              </div>
              <h1 className="text-2xl font-semibold leading-tight text-foreground">
                {market.title}
              </h1>
              <p className="text-sm text-muted-foreground">{market.description}</p>
            </div>

            <div className="grid gap-4 sm:grid-cols-3 font-mono text-xs text-muted-foreground">
              <div>
                <p className="uppercase tracking-wide text-[10px]">Ends</p>
                <p className="text-foreground">
                  {market.end_date
                    ? new Date(market.end_date).toLocaleString()
                    : "--"}
                </p>
              </div>
              <div>
                <p className="uppercase tracking-wide text-[10px]">Liquidity</p>
                <p className="text-foreground">
                  {market.liquidity
                    ? `$${market.liquidity.toLocaleString(undefined, {
                        maximumFractionDigits: 0,
                      })}`
                    : "$0"}
                </p>
              </div>
              <div>
                <p className="uppercase tracking-wide text-[10px]">Outcomes</p>
                <p className="text-foreground truncate">
                  {outcomes.join(" • ")}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        <div className="space-y-4">
          <Card className="border-border bg-card/70 backdrop-blur">
            <CardContent className="p-4">
              <TradeForm market={market} />
            </CardContent>
          </Card>
          <OrdersPanel />
        </div>
      </div>
    </div>
  );
}

