/**
 * @description
 * Container for the trading chart: fetches history, streams live prices, and renders controls.
 */

"use client";

import React, { useEffect, useMemo, useState } from "react";
import dynamic from "next/dynamic";
import { useQuery } from "@tanstack/react-query";
import { Layers, Loader2, RefreshCcw } from "lucide-react";

import {
  ChartDataPoint,
  HistoryPoint,
  deriveInverseSeries,
  mergeLiveTick,
  transformHistoryData,
} from "@/lib/feed";
import { api } from "@/lib/api";
import { usePriceStream } from "@/hooks/usePriceStream";
import type { Market } from "@/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { calculateDisplayPrice } from "@/lib/price-utils";

const TVChart = dynamic(() => import("./TVChart"), {
  ssr: false,
  loading: () => (
    <div className="flex h-[400px] items-center justify-center bg-card/50">
      <Loader2 className="h-8 w-8 animate-spin text-primary" />
    </div>
  ),
});

interface ChartContainerProps {
  marketId: string;
  tokenYesId: string;
  tokenNoId: string;
  initialHeight?: number;
  market?: Market;
}

type TimeRange = "1H" | "6H" | "1D" | "1W" | "1M" | "ALL";

const RANGES: TimeRange[] = ["1H", "6H", "1D", "1W", "1M", "ALL"];

export function ChartContainer({
  marketId,
  tokenYesId,
  tokenNoId,
  initialHeight = 400,
  market,
}: ChartContainerProps) {
  const [range, setRange] = useState<TimeRange>("1D");
  const [showNo, setShowNo] = useState(false);
  const [latestTick, setLatestTick] = useState<{ time: number; price: number } | null>(null);

  const { data: historyData, isLoading, isError, refetch } = useQuery({
    queryKey: ["price-history", marketId, range],
    queryFn: async () => {
      const { data } = await api.get<HistoryPoint[]>(
        `/markets/${encodeURIComponent(marketId)}/history`,
        { params: { range: range.toLowerCase() } }
      );
      return data;
    },
    refetchOnWindowFocus: false,
    enabled: Boolean(marketId),
  });

  const { augmentMarket } = usePriceStream();

  const liveMarket = useMemo(() => {
    if (market) {
      return market;
    }

    const stubMarket = {
      condition_id: marketId,
      token_id_yes: tokenYesId,
      token_id_no: tokenNoId,
    } satisfies Partial<Market>;

    return augmentMarket(stubMarket as Market);
  }, [augmentMarket, market, marketId, tokenYesId, tokenNoId]);

  const [chartDataYes, setChartDataYes] = useState<ChartDataPoint[]>([]);

  useEffect(() => {
    if (!historyData) return;
    const base = transformHistoryData(historyData);
    if (!latestTick) {
      setChartDataYes(base);
      return;
    }
    const lastPoint = base[base.length - 1];
    const lastTime = lastPoint && typeof lastPoint.time === "number" ? lastPoint.time : undefined;
    const adjustedTick =
      typeof lastTime === "number" && latestTick.time < lastTime
        ? { ...latestTick, time: lastTime }
        : latestTick;
    if (adjustedTick.time !== latestTick.time) {
      setLatestTick(adjustedTick);
    }
    setChartDataYes(mergeLiveTick(base, adjustedTick));
  }, [historyData, latestTick]);

  useEffect(() => {
    setLatestTick(null);
  }, [marketId]);


  useEffect(() => {
    if (!liveMarket) return;

    // Use Polymarket display rule for live updates (midpoint unless spread > 10c).
    const currentPrice = calculateDisplayPrice(
      liveMarket.yes_best_bid,
      liveMarket.yes_best_ask,
      liveMarket.yes_price,
      `${marketId}:yes`
    );

    const { yes_price_updated: updatedAt } = liveMarket as {
      yes_price_updated?: string;
    };

    if (typeof currentPrice !== "number") {
      return;
    }

    let tsMs = updatedAt ? Date.parse(updatedAt) : Number.NaN;
    if (Number.isNaN(tsMs) && updatedAt) {
      const numericTs = Number(updatedAt);
      if (Number.isFinite(numericTs)) {
        tsMs = numericTs > 1e12 ? numericTs : numericTs * 1000;
      }
    }

    const lastPoint = chartDataYes[chartDataYes.length - 1];
    const lastSeriesTime =
      lastPoint && typeof lastPoint.time === "number" ? lastPoint.time : undefined;
    const fallbackMs =
      typeof lastSeriesTime === "number"
        ? lastSeriesTime * 1000
        : latestTick
          ? latestTick.time * 1000
          : Date.now();
    const effectiveMs = Number.isNaN(tsMs) ? fallbackMs : tsMs;
    const tickTime = Math.floor(effectiveMs / 1000);
    const tick = { time: tickTime, price: currentPrice };
    setLatestTick(tick);
    setChartDataYes((prev) => mergeLiveTick(prev, tick));
  }, [liveMarket, chartDataYes, latestTick]);

  const chartDataNo = useMemo(() => deriveInverseSeries(chartDataYes), [chartDataYes]);

  return (
    <Card className="overflow-hidden border-border bg-card/60 shadow-xl backdrop-blur">
      <CardHeader className="flex flex-row items-center justify-between border-b border-border/50 px-4 py-3">
        <div className="flex items-center gap-4">
          <CardTitle className="text-sm font-mono uppercase tracking-widest text-muted-foreground">
            Price Action
          </CardTitle>
          <div className="flex items-center rounded-md border border-border/50 bg-background/50 p-1">
            {RANGES.map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={cn(
                  "rounded-sm px-2.5 py-1 text-[10px] font-bold transition-all",
                  range === r
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                )}
              >
                {r}
              </button>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setShowNo((prev) => !prev)}
            className={cn(
              "h-7 border border-transparent px-2 text-[10px] font-mono",
              showNo ? "border-destructive/50 bg-destructive/10 text-destructive" : "text-muted-foreground"
            )}
            title="Toggle Inverse (NO) Line"
          >
            <Layers className="mr-1.5 h-3 w-3" />
            {showNo ? "Hide NO" : "Show NO"}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => refetch()}
            className="h-7 w-7 text-muted-foreground hover:text-foreground"
            title="Refresh History"
          >
            <RefreshCcw className="h-3 w-3" />
          </Button>
        </div>
      </CardHeader>
      <CardContent className="relative p-0">
        {isLoading && (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-background/50">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
          </div>
        )}

        {isError && (
          <div className="absolute inset-0 z-10 flex flex-col items-center justify-center bg-background/80 text-muted-foreground">
            <p className="mb-2 text-sm font-mono">Failed to load price history</p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              Retry
            </Button>
          </div>
        )}

        <div className="w-full" style={{ height: initialHeight }}>
          <TVChart
            dataYes={chartDataYes}
            dataNo={chartDataNo}
            showNoSeries={showNo}
            height={initialHeight}
          />
        </div>
      </CardContent>
    </Card>
  );
}
