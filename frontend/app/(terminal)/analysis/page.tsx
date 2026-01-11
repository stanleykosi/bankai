/**
 * @description
 * Alpha Hub (/analysis) page.
 * Surfaces AI Picks, Smart Money flow, and Whale tape with a high-density layout.
 */

"use client";

import React, { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Brain, Flame, Loader2, Sparkles, Zap } from "lucide-react";

import { Card } from "@/components/ui/card";
import { API_BASE_URL } from "@/lib/api";
import { fetchAnalysisSnapshot, fetchRecentWhales } from "@/lib/analysis";
import type { AIPick, MarketSignal, WhaleEvent } from "@/types/analysis";
import { cn } from "@/lib/utils";

const windowMinutes = 1440;
const maxDisplayMarkets = 50;
const pageSize = 10;
const maxPages = 5;
const whaleLimit = 15;

const whaleKey = (whale: WhaleEvent) =>
  whale.tx_hash ? `tx:${whale.tx_hash}` : `${whale.market_id}-${whale.ts}-${whale.side}-${whale.size_usd}`;

const mergeWhales = (existing: WhaleEvent[], incoming: WhaleEvent[]) => {
  const map = new Map<string, WhaleEvent>();
  const merged = [...incoming, ...existing];
  for (const whale of merged) {
    map.set(whaleKey(whale), whale);
  }
  return Array.from(map.values())
    .sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime())
    .slice(0, whaleLimit);
};

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

function getTierClass(tier?: string) {
  switch ((tier || "").toLowerCase()) {
    case "gold":
      return "bg-amber-500/15 text-amber-300 border border-amber-500/30";
    case "silver":
      return "bg-slate-400/15 text-slate-200 border border-slate-400/30";
    case "bronze":
      return "bg-orange-500/15 text-orange-300 border border-orange-500/30";
    case "new":
      return "bg-emerald-500/10 text-emerald-200 border border-emerald-500/30";
    default:
      return "bg-muted text-foreground/80 border border-border/60";
  }
}

function shortenWallet(wallet?: string) {
  if (!wallet) return "--";
  if (wallet.length <= 10) return wallet;
  return `${wallet.slice(0, 6)}...${wallet.slice(-4)}`;
}

function WhaleRow({ whale }: { whale: WhaleEvent }) {
  const isNewWallet = whale.win_rate < 0 || !whale.wallet_tier;
  const tierLabel = isNewWallet ? "New" : whale.wallet_tier;
  const winRateLabel = whale.win_rate >= 0 ? `${(whale.win_rate * 100).toFixed(0)}%` : "--";
  const priceLabel = whale.price >= 1 ? whale.price.toFixed(2) : whale.price.toFixed(3);
  const marketHref = `/market/${whale.slug || whale.market_id}`;
  const traderHref = whale.wallet ? `/profile/${whale.wallet}` : "";
  const iconUrl = whale.market_icon || whale.market_image;
  const traderLabel = whale.trader_name || shortenWallet(whale.wallet);
  const outcomeLabel = whale.outcome ? whale.outcome.toUpperCase() : "--";
  const pnlValue = whale.realized_pnl || 0;
  const pnlLabel = pnlValue !== 0 ? `${pnlValue > 0 ? "+" : ""}${formatDollars(Math.abs(pnlValue))}` : "--";
  const pnlClass = pnlValue > 0 ? "text-emerald-300" : pnlValue < 0 ? "text-rose-300" : "text-muted-foreground";

  return (
    <Card className="group border-border/60 bg-card/50 p-3 shadow-sm shadow-primary/10 transition hover:border-primary/40">
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted/40">
            {iconUrl ? (
              <img src={iconUrl} alt="" className="h-full w-full object-cover" loading="lazy" />
            ) : (
              <div className="h-7 w-7 rounded-full bg-muted/60" />
            )}
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs font-mono">
              <div className="flex items-center gap-2 text-muted-foreground">
                <span>{new Date(whale.ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase",
                    whale.side === "BUY" ? "bg-emerald-500/15 text-emerald-300" : "bg-rose-500/15 text-rose-300",
                  )}
                >
                  {whale.side}
                </span>
                <span className="rounded-full bg-muted/50 px-2 py-0.5 text-[10px] font-semibold uppercase text-foreground/80">
                  {outcomeLabel}
                </span>
                <span className="text-foreground">{formatDollars(whale.size_usd)} USDC</span>
              </div>
              <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                <span className={cn("rounded-full px-2 py-0.5 font-semibold uppercase", getTierClass(tierLabel))}>
                  {tierLabel}
                </span>
                <span>WR {winRateLabel}</span>
                <span className={pnlClass}>PNL {pnlLabel}</span>
              </div>
            </div>
            <div className="mt-2 flex items-center gap-2">
              <Link
                href={marketHref}
                className="truncate text-sm font-semibold text-foreground transition hover:text-primary"
              >
                {whale.title || whale.market_id}
              </Link>
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
              {traderHref ? (
                <Link
                  href={traderHref}
                  className="flex items-center gap-1.5 rounded-full border border-border/60 px-2 py-0.5 hover:border-primary/40"
                >
                  <span className="flex h-4 w-4 items-center justify-center overflow-hidden rounded-full bg-muted/60 text-[10px] text-foreground/70">
                    {whale.trader_image ? (
                      <img src={whale.trader_image} alt="" className="h-full w-full object-cover" loading="lazy" />
                    ) : (
                      traderLabel.charAt(0).toUpperCase()
                    )}
                  </span>
                  <span>{traderLabel}</span>
                </Link>
              ) : (
                <span className="rounded-full border border-border/60 px-2 py-0.5">{traderLabel}</span>
              )}
              <span className="rounded-full border border-border/60 px-2 py-0.5">Price {priceLabel}</span>
              {typeof whale.spread_bps === "number" ? (
                <span className="rounded-full border border-border/60 px-2 py-0.5">Spread {whale.spread_bps.toFixed(0)} bps</span>
              ) : null}
              {whale.is_wash_trade ? (
                <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-semibold uppercase text-amber-300">Wash</span>
              ) : null}
            </div>
          </div>
        </div>
      </Card>
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

function LoadingState() {
  return (
    <div className="flex flex-col gap-6 rounded-xl border border-border/70 bg-card/60 p-6">
      <div className="flex items-center gap-3 text-muted-foreground">
        <Loader2 className="h-5 w-5 animate-spin" />
        <div className="space-y-1">
          <div className="font-mono text-xs text-foreground/80">Loading Alpha Hub snapshot...</div>
          <div className="text-xs text-muted-foreground">Fetching cached AI + smart money + whale tape.</div>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {Array.from({ length: 6 }).map((_, idx) => (
          <Card key={idx} className="border-border/50 bg-card/40 p-4">
            <div className="h-4 w-3/4 animate-pulse rounded bg-muted/40" />
            <div className="mt-3 flex flex-wrap gap-2">
              <div className="h-5 w-20 animate-pulse rounded-full bg-muted/30" />
              <div className="h-5 w-20 animate-pulse rounded-full bg-muted/30" />
              <div className="h-5 w-24 animate-pulse rounded-full bg-muted/30" />
            </div>
            <div className="mt-4 grid grid-cols-3 gap-3">
              <div className="space-y-2">
                <div className="h-3 w-16 animate-pulse rounded bg-muted/30" />
                <div className="h-3 w-24 animate-pulse rounded bg-muted/30" />
              </div>
              <div className="space-y-2">
                <div className="h-3 w-16 animate-pulse rounded bg-muted/30" />
                <div className="h-3 w-24 animate-pulse rounded bg-muted/30" />
              </div>
              <div className="space-y-2">
                <div className="h-3 w-16 animate-pulse rounded bg-muted/30" />
                <div className="h-3 w-24 animate-pulse rounded bg-muted/30" />
              </div>
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}

export default function AnalysisPage() {
  const [liveWhales, setLiveWhales] = useState<WhaleEvent[]>([]);
  const snapshotQuery = useQuery({
    queryKey: ["analysis", "snapshot", windowMinutes],
    queryFn: () => fetchAnalysisSnapshot(windowMinutes),
    refetchInterval: 5 * 60_000,
    staleTime: 60_000,
  });

  const snapshot = snapshotQuery.data;
  const isLoading = snapshotQuery.isLoading && !snapshot;
  const smart = snapshot?.smart_money;
  const ai = snapshot?.ai;

  const topMarkets = smart?.markets?.slice(0, maxDisplayMarkets) ?? [];
  useEffect(() => {
    let cancelled = false;
    const loadRecentWhales = async () => {
      try {
        const recent = await fetchRecentWhales(whaleLimit);
        if (!cancelled) {
          setLiveWhales((prev) => mergeWhales(prev, recent));
        }
      } catch {
      }
    };

    loadRecentWhales();
    const poll = setInterval(loadRecentWhales, 15_000);
    return () => {
      clearInterval(poll);
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const streamUrl = `${API_BASE_URL}/api/v1/analysis/whales/stream`;
    let source: EventSource | null = null;
    let retryHandle: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      source = new EventSource(streamUrl);

      source.onmessage = (event) => {
        if (!event.data) return;
        try {
          const payload = JSON.parse(event.data) as WhaleEvent;
          setLiveWhales((prev) => {
            return mergeWhales(prev, [payload]);
          });
        } catch {
        }
      };

      source.onerror = () => {
        source?.close();
        retryHandle = setTimeout(connect, 3000);
      };
    };

    connect();

    return () => {
      if (retryHandle) {
        clearTimeout(retryHandle);
      }
      source?.close();
    };
  }, []);

  const whales = useMemo(() => liveWhales.slice(0, whaleLimit), [liveWhales]);

  const totalPages = useMemo(() => {
    const pages = Math.ceil(topMarkets.length / pageSize);
    if (pages === 0) return 1;
    return Math.min(pages, maxPages);
  }, [topMarkets.length]);

  const [page, setPage] = useState(1);
  const currentPage = Math.min(page, totalPages);
  const pagedMarkets = useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return topMarkets.slice(start, start + pageSize);
  }, [currentPage, topMarkets]);

  const aiGenerated = ai?.generated_at ? new Date(ai.generated_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "...";
  const aiStale = Boolean(snapshot?.stale || ai?.stale);

  return (
    <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-6 px-4 py-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
            <Sparkles className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-foreground">Alpha Hub</h1>
            <p className="text-sm text-muted-foreground">Smart Money flow, AI picks, and live whale activity.</p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatPill label="Markets" value={`${pagedMarkets.length}/${topMarkets.length}`} />
          <StatPill label="Whales" value={String(whales.length)} />
          <StatPill label="Updated" value={smart?.generated_at ? new Date(smart.generated_at).toLocaleTimeString() : "..."} />
          <StatPill label="AI Picks" value={ai?.ai_picks?.length ? String(ai.ai_picks.length) : "..."} />
          <StatPill label="AI Generated" value={aiGenerated} />
        </div>
      </div>

      {isLoading ? (
        <LoadingState />
      ) : (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
          {/* AI Picks */}
          <div className="col-span-1 lg:col-span-2 space-y-3">
            <div className="flex items-center gap-2">
              <Brain className="h-4 w-4 text-primary" />
              <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground">AI Picks</h2>
              {aiStale ? <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-semibold uppercase text-amber-300">Stale</span> : null}
              <span className="text-[11px] text-muted-foreground">
                Daily snapshot · {aiGenerated}
              </span>
            </div>
            {snapshotQuery.isLoading && !ai?.ai_picks ? (
              <Card className="border-dashed border-border/70 bg-card/40 p-6 text-center text-sm text-muted-foreground">
                Loading daily AI picks...
              </Card>
            ) : ai?.ai_picks?.length ? (
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
                  No live whale prints yet.
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
          {pagedMarkets.length ? (
            pagedMarkets.map((m) => <MarketSignalCard key={m.market_id} market={m} />)
          ) : (
            <Card className="border-dashed border-border/70 bg-card/40 p-6 text-center text-sm text-muted-foreground">
              No markets available.
            </Card>
          )}
        </div>
        <div className="flex items-center justify-center gap-3">
          <button
            className="rounded-md border border-border/70 bg-card/60 px-3 py-1 text-xs font-medium text-foreground/80 transition hover:bg-card disabled:opacity-50"
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={currentPage === 1}
          >
            Prev
          </button>
          <span className="text-xs font-mono text-muted-foreground">
            Page {currentPage} / {totalPages}
          </span>
          <button
            className="rounded-md border border-border/70 bg-card/60 px-3 py-1 text-xs font-medium text-foreground/80 transition hover:bg-card disabled:opacity-50"
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            disabled={currentPage === totalPages}
          >
            Next
          </button>
        </div>
      </div>
    </div>
  );
}
