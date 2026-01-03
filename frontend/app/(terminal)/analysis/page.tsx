/**
 * @description
 * Alpha Hub (/analysis) page.
 * Surfaces AI Picks, Smart Money flow, and Whale tape with a high-density layout.
 */

"use client";

import React from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Brain, Flame, Loader2, Sparkles, Zap } from "lucide-react";

import { Card } from "@/components/ui/card";
import { fetchAIPicks, fetchSmartMoney } from "@/lib/analysis";
import type { AIPick, MarketSignal, WhaleEvent } from "@/types/analysis";
import { cn } from "@/lib/utils";

const windowMinutes = 1440;

const formatDollars = (value: number) =>
  value >= 1_000_000
    ? `${(value / 1_000_000).toFixed(1)}M`
    : value >= 1_000
      ? `${(value / 1_000).toFixed(1)}k`
      : value.toFixed(0);

function StatPill({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-2 rounded-full border border-border/70 bg-card/60 px-3 py-1 text-xs font-medium text-muted-foreground">
      <span className="text-foreground/80">{label}</span>
      <span className="font-mono text-foreground">{value}</span>
    </div>
  );
}

function AIPickCard({ pick }: { pick: AIPick }) {
  return (
    <Card className="border-border/70 bg-card/70 p-4 shadow-md shadow-primary/10">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1">
          <Link
            href={`/market/${pick.slug}`}
            className="text-base font-semibold text-foreground transition hover:text-primary"
          >
            {pick.title}
          </Link>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className={cn("rounded-full px-2 py-0.5 font-mono", getConvictionClass(pick.conviction))}>
              {pick.conviction}
            </span>
            <span className="rounded-full bg-secondary/40 px-2 py-0.5 font-mono">
              P(YES): {(pick.probability_yes * 100).toFixed(1)}%
            </span>
            <span className="rounded-full border border-border/70 px-2 py-0.5 font-mono uppercase">
              {pick.action}
            </span>
          </div>
        </div>
        <ArrowUpRight className="h-4 w-4 text-muted-foreground" />
      </div>
      <p className="mt-3 text-sm leading-relaxed text-muted-foreground">{pick.rationale}</p>
    </Card>
  );
}

function getConvictionClass(conviction: string) {
  switch (conviction.toLowerCase()) {
    case "high":
      return "bg-primary/15 text-primary border border-primary/30";
    case "medium":
      return "bg-amber-500/10 text-amber-400 border border-amber-500/30";
    default:
      return "bg-muted text-foreground/80";
  }
}

function WhaleRow({ whale }: { whale: WhaleEvent }) {
  return (
    <div className="grid grid-cols-6 items-center gap-3 rounded-md border border-border/50 bg-card/40 px-3 py-2 text-xs font-mono">
      <span className="text-muted-foreground">{new Date(whale.ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
      <span className={cn("uppercase", whale.side === "BUY" ? "text-emerald-400" : "text-rose-400")}>{whale.side}</span>
      <span className="text-foreground">{formatDollars(whale.size_usd)} USDC</span>
      <span className="truncate text-muted-foreground">{whale.wallet_tier || "N/A"}</span>
      <span className="truncate text-muted-foreground">{whale.title || whale.market_id}</span>
      <span className="text-muted-foreground">{(whale.win_rate * 100).toFixed(0)}%</span>
    </div>
  );
}

function MarketSignalCard({ market }: { market: MarketSignal }) {
  return (
    <Card className="border-border/70 bg-card/60 p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <Link
            href={`/market/${market.slug}`}
            className="text-base font-semibold text-foreground transition hover:text-primary"
          >
            {market.title}
          </Link>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] font-mono text-muted-foreground">
            <span className="rounded-full border border-border/60 px-2 py-0.5">
              Spread {market.spread_bps.toFixed(0)} bps
            </span>
            <span className="rounded-full border border-border/60 px-2 py-0.5">
              Net Buy {formatDollars(market.smart_money.net_buy_usd)}
            </span>
            <span className="rounded-full border border-border/60 px-2 py-0.5">
              Whale Hits {market.smart_money.whale_hits_count}
            </span>
            <span className="rounded-full border border-border/60 px-2 py-0.5">
              Entry Edge {market.smart_money.avg_entry_vs_mid_bps.toFixed(1)} bps
            </span>
          </div>
        </div>
        <div className="text-right">
          <div className="text-2xl font-semibold text-primary">{(market.yes_price * 100).toFixed(1)}%</div>
          <div className="text-xs text-muted-foreground">Score {market.score.toFixed(1)}</div>
        </div>
      </div>
      <div className="mt-3 grid grid-cols-3 gap-3 text-xs font-mono text-muted-foreground">
        <div>
          <div className="text-foreground">Smart Money</div>
          <div>Gold {formatDollars(market.smart_money.gold_buys)}</div>
          <div>Silver {formatDollars(market.smart_money.silver_buys)}</div>
        </div>
        <div>
          <div className="text-foreground">Momentum</div>
          <div>1h {market.p1h.toFixed(2)}</div>
          <div>24h {market.p24h.toFixed(2)}</div>
        </div>
        <div>
          <div className="text-foreground">Liquidity</div>
          <div>Bid {market.best_bid.toFixed(3)} / Ask {market.best_ask.toFixed(3)}</div>
          <div>Vol 24h {formatDollars(market.volume_24h)}</div>
        </div>
      </div>
    </Card>
  );
}

export default function AnalysisPage() {
  const smartQuery = useQuery({
    queryKey: ["analysis", "smart", windowMinutes],
    queryFn: () => fetchSmartMoney(windowMinutes),
    refetchInterval: 60_000,
  });

  const aiQuery = useQuery({
    queryKey: ["analysis", "ai", windowMinutes],
    queryFn: () => fetchAIPicks(windowMinutes),
    refetchInterval: 120_000,
    enabled: Boolean(smartQuery.data),
  });

  const isLoading = smartQuery.isLoading;
  const smart = smartQuery.data;
  const ai = aiQuery.data;

  const topMarkets = smart?.markets?.slice(0, 6) ?? [];
  const whales = smart?.whales?.slice(0, 15) ?? [];

  return (
    <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-6 px-4 py-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
            <Sparkles className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-foreground">Alpha Hub</h1>
            <p className="text-sm text-muted-foreground">Smart Money flow, AI picks, and Whale activity (last {windowMinutes}m)</p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatPill label="Markets" value={String(topMarkets.length)} />
          <StatPill label="Whales" value={String(whales.length)} />
          <StatPill label="Updated" value={smart?.generated_at ? new Date(smart.generated_at).toLocaleTimeString() : "..."} />
        </div>
      </div>

      {isLoading ? (
        <div className="flex h-[60vh] items-center justify-center">
          <div className="flex items-center gap-3 text-muted-foreground">
            <Loader2 className="h-5 w-5 animate-spin" />
            <span className="font-mono text-xs">Collecting smart money flow...</span>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
          {/* AI Picks */}
          <div className="col-span-1 lg:col-span-2 space-y-3">
            <div className="flex items-center gap-2">
              <Brain className="h-4 w-4 text-primary" />
              <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground">AI Picks</h2>
            </div>
            {ai?.ai_picks?.length ? (
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                {ai.ai_picks.map((pick) => (
                  <AIPickCard key={`${pick.market_id}-${pick.slug}`} pick={pick} />
                ))}
              </div>
            ) : (
              <Card className="border-dashed border-border/70 bg-card/40 p-6 text-center text-sm text-muted-foreground">
                No AI picks yet. Waiting for fresh smart money flow.
              </Card>
            )}
          </div>

          {/* Whale Activity */}
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Flame className="h-4 w-4 text-amber-400" />
              <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground">Whale Activity</h2>
            </div>
            <div className="space-y-2">
              {whales.length ? (
                whales.map((w, idx) => <WhaleRow whale={w} key={`${w.market_id}-${w.ts}-${idx}`} />)
              ) : (
                <Card className="border-dashed border-border/70 bg-card/40 p-6 text-center text-sm text-muted-foreground">
                  No whale prints in the last {windowMinutes} minutes.
                </Card>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Smart Money Markets */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <Zap className="h-4 w-4 text-emerald-400" />
          <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground">Smart Money Flow</h2>
        </div>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          {topMarkets.map((m) => (
            <MarketSignalCard key={m.market_id} market={m} />
          ))}
        </div>
      </div>
    </div>
  );
}
