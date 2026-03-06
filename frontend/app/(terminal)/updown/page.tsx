"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  ChevronRight,
  Clock3,
  Gauge,
  RefreshCw,
  ShieldAlert,
  Signal,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  TradeForm,
  type TradeRecommendationPrefill,
} from "@/components/terminal/TradeForm";
import { usePriceStream } from "@/hooks/usePriceStream";
import { requestMarketStream } from "@/lib/market-data";
import {
  createUpDownEventSource,
  fetchUpDownMarkets,
  fetchUpDownPerformance,
  fetchUpDownRecommendations,
  fetchUpDownSignal,
  logUpDownDecision,
} from "@/lib/updown-api";
import type { Recommendation, UpDownMarket, UpDownSignal } from "@/types";
import { cn } from "@/lib/utils";

const ASSETS = ["ALL", "BTC", "ETH", "SOL", "XRP"] as const;
const WINDOWS = ["all", "5m", "15m", "1h", "4h"] as const;
const PREFILL_DRIFT_BPS = 35;
const STREAM_RETRY_BASE_MS = 3500;
const STREAM_MAX_RETRIES = 5;

const pct = (value?: number) => {
  if (typeof value !== "number" || Number.isNaN(value)) return "--";
  return `${(value * 100).toFixed(1)}%`;
};

const money = (value?: number) => {
  if (typeof value !== "number" || Number.isNaN(value)) return "--";
  return value >= 0
    ? `+$${value.toFixed(4)}`
    : `-$${Math.abs(value).toFixed(4)}`;
};

const fmtCountdown = (seconds: number) => {
  if (!Number.isFinite(seconds)) return "--";
  if (seconds <= 0) return "00:00";
  const s = Math.floor(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const rem = s % 60;
  if (h > 0) {
    return `${h}:${String(m).padStart(2, "0")}:${String(rem).padStart(2, "0")}`;
  }
  return `${String(m).padStart(2, "0")}:${String(rem).padStart(2, "0")}`;
};

const toMillis = (value?: string) => {
  if (!value) return 0;
  const ts = Date.parse(value);
  return Number.isNaN(ts) ? 0 : ts;
};

const pickNextMarket = (
  markets: UpDownMarket[],
  nowMs: number,
  currentSlug: string | null,
): string | null => {
  if (!markets.length) return null;
  const sorted = [...markets].sort(
    (a, b) => toMillis(a.event_start_time) - toMillis(b.event_start_time),
  );
  const active = sorted.find((m) => {
    const start = toMillis(m.event_start_time);
    const end = toMillis(m.event_end_time);
    return start <= nowMs && nowMs < end;
  });
  if (active) return active.slug;

  if (currentSlug) {
    const current = sorted.find((m) => m.slug === currentSlug);
    if (current) {
      const end = toMillis(current.event_end_time);
      if (nowMs < end) return currentSlug;
    }
  }

  const upcoming = sorted.find((m) => toMillis(m.event_start_time) > nowMs);
  return upcoming?.slug ?? sorted[0]?.slug ?? null;
};

export default function UpDownPage() {
  const [asset, setAsset] = useState<(typeof ASSETS)[number]>("ALL");
  const [windowType, setWindowType] = useState<(typeof WINDOWS)[number]>("all");
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);
  const [liveSignals, setLiveSignals] = useState<Record<string, UpDownSignal>>(
    {},
  );
  const [prefill, setPrefill] = useState<TradeRecommendationPrefill | null>(
    null,
  );
  const [prefillBlockReason, setPrefillBlockReason] = useState<string | null>(
    null,
  );
  const [nowMs, setNowMs] = useState<number>(Date.now());
  const { augmentMarket } = usePriceStream();

  const marketsQuery = useQuery({
    queryKey: ["updown-markets", asset, windowType],
    queryFn: () =>
      fetchUpDownMarkets({
        asset: asset === "ALL" ? undefined : asset,
        window: windowType === "all" ? undefined : windowType,
      }),
    refetchInterval: 10_000,
    staleTime: 4_000,
  });

  const recommendationsQuery = useQuery({
    queryKey: ["updown-recommendations", asset],
    queryFn: () =>
      fetchUpDownRecommendations({
        asset: asset === "ALL" ? undefined : asset,
        limit: 80,
      }),
    refetchInterval: 12_000,
    staleTime: 6_000,
  });

  const performanceQuery = useQuery({
    queryKey: ["updown-performance"],
    queryFn: () => fetchUpDownPerformance({}),
    refetchInterval: 60_000,
    staleTime: 15_000,
  });

  const hasSelectedSlug =
    typeof selectedSlug === "string" &&
    selectedSlug.trim() !== "" &&
    selectedSlug !== "null" &&
    selectedSlug !== "undefined";

  const signalQuery = useQuery({
    queryKey: ["updown-signal", selectedSlug],
    queryFn: () => fetchUpDownSignal(selectedSlug as string),
    enabled: hasSelectedSlug,
    refetchInterval: 5_000,
    staleTime: 2_000,
  });
  const markets = marketsQuery.data ?? [];
  const selectedMarket = useMemo(
    () => markets.find((m) => m.slug === selectedSlug) ?? null,
    [markets, selectedSlug],
  );

  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const markets = marketsQuery.data ?? [];
    if (!markets.length) {
      if (selectedSlug !== null) {
        setSelectedSlug(null);
      }
      return;
    }

    // Auto-pick only when we don't have a selection yet, or it no longer
    // exists in the current market set (filter change / market expired).
    const hasSelection = typeof selectedSlug === "string" && selectedSlug !== "";
    const selectionStillExists =
      hasSelection && markets.some((m) => m.slug === selectedSlug);
    if (selectionStillExists) {
      return;
    }

    const next = pickNextMarket(markets, nowMs, null);
    if (next && next !== selectedSlug) {
      setSelectedSlug(next);
    }
  }, [marketsQuery.data, nowMs, selectedSlug]);

  useEffect(() => {
    if (markets.length === 0) {
      setLiveSignals({});
      return;
    }

    let source: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;
    let retries = 0;

    const clearRetry = () => {
      if (retry) {
        clearTimeout(retry);
        retry = null;
      }
    };

    const connect = () => {
      if (stopped) return;
      source = createUpDownEventSource();
      source.onopen = () => {
        retries = 0;
      };
      source.onmessage = (event) => {
        if (!event.data) return;
        try {
          const payload = JSON.parse(event.data) as {
            slug?: string;
            signal?: UpDownSignal;
          };
          if (!payload.slug || !payload.signal) return;
          setLiveSignals((prev) => ({
            ...prev,
            [payload.slug as string]: payload.signal as UpDownSignal,
          }));
        } catch {}
      };
      source.onerror = () => {
        source?.close();
        source = null;
        if (stopped) return;
        if (retries >= STREAM_MAX_RETRIES) return;
        retries += 1;
        clearRetry();
        retry = setTimeout(connect, STREAM_RETRY_BASE_MS * retries);
      };
    };
    connect();
    return () => {
      stopped = true;
      clearRetry();
      source?.close();
    };
  }, [markets.length]);

  useEffect(() => {
    const conditionId = selectedMarket?.condition_id;
    if (!conditionId) return;
    requestMarketStream(conditionId).catch(() => undefined);
  }, [selectedMarket?.condition_id]);

  const selectedSignal = useMemo(() => {
    if (!selectedSlug) return null;
    const liveSignal = liveSignals[selectedSlug] ?? null;
    const polledSignal = signalQuery.data ?? null;
    if (!liveSignal) return polledSignal;
    if (!polledSignal) return liveSignal;

    const liveTs = toMillis(liveSignal.timestamp);
    const polledTs = toMillis(polledSignal.timestamp);
    if (!liveTs) return polledSignal;
    if (!polledTs) return liveSignal;
    return polledTs > liveTs ? polledSignal : liveSignal;
  }, [liveSignals, selectedSlug, signalQuery.data]);

  const selectedRecommendation = useMemo(() => {
    if (selectedSignal?.recommendation) return selectedSignal.recommendation;
    return (
      (recommendationsQuery.data ?? []).find((r) => r.slug === selectedSlug) ??
      null
    );
  }, [recommendationsQuery.data, selectedSignal?.recommendation, selectedSlug]);

  const staleSignal = useMemo(() => {
    const ts = toMillis(selectedSignal?.timestamp);
    if (!ts) return true;
    return nowMs - ts > 30_000;
  }, [selectedSignal?.timestamp, nowMs]);

  const activeCountdown = useMemo(() => {
    if (!selectedMarket) return "--";
    const start = toMillis(selectedMarket.event_start_time);
    const end = toMillis(selectedMarket.event_end_time);
    if (nowMs < start) {
      return `Starts in ${fmtCountdown((start - nowMs) / 1000)}`;
    }
    return `Ends in ${fmtCountdown((end - nowMs) / 1000)}`;
  }, [nowMs, selectedMarket]);

  const liveMarket = selectedMarket
    ? augmentMarket(selectedMarket.market)
    : null;
  const integrityFailure =
    staleSignal ||
    !!selectedSignal?.risk_flags?.data_integrity_failed ||
    !!selectedSignal?.risk_flags?.kill_switch ||
    !!selectedRecommendation?.prefill?.disabled ||
    !!prefillBlockReason;

  const prefillDriftBps = useMemo(() => {
    if (!prefill || prefill.disabled || !selectedSignal || !selectedMarket)
      return 0;
    const isUpOutcome =
      prefill.outcomeIndex === selectedMarket.outcome_index_up;
    const liveAsk = isUpOutcome
      ? selectedSignal.executable_ask_up
      : selectedSignal.executable_ask_down;
    const ref = prefill.limitPrice ?? 0;
    if (!liveAsk || !ref) return 0;
    return (Math.abs(liveAsk - ref) / Math.max(ref, 0.01)) * 10_000;
  }, [prefill, selectedSignal, selectedMarket]);

  useEffect(() => {
    if (!prefill || prefill.disabled) return;
    if (prefillDriftBps <= PREFILL_DRIFT_BPS) return;
    const driftText = prefillDriftBps.toFixed(0);
    setPrefill((prev) =>
      prev
        ? {
            ...prev,
            disabled: true,
            applyToken: String(Date.now()),
          }
        : prev,
    );
    setPrefillBlockReason(
      `Recommendation invalidated: odds drifted ${driftText} bps. Re-apply strategy prefill before placing a trade.`,
    );
  }, [prefill, prefillDriftBps]);

  useEffect(() => {
    setPrefill(null);
    setPrefillBlockReason(null);
  }, [selectedSlug]);

  const applyRecommendation = (rec: Recommendation | null) => {
    if (!rec || rec.prefill.disabled) return;
    setPrefillBlockReason(null);
    setPrefill({
      side: "BUY",
      outcomeIndex: rec.prefill.outcome_index,
      limitPrice: rec.prefill.limit_price,
      sizeShares: rec.prefill.size_shares,
      disabled: rec.prefill.disabled,
      applyToken: String(Date.now()),
    });
    void logUpDownDecision({
      slug: rec.slug,
      recommendation_id: rec.id,
      action: "accepted",
      chosen_side: rec.recommended_side,
    }).catch(() => undefined);
  };

  const rejectRecommendation = (rec: Recommendation | null) => {
    if (!rec) return;
    void logUpDownDecision({
      slug: rec.slug,
      recommendation_id: rec.id,
      action: "rejected",
      chosen_side: rec.recommended_side,
    }).catch(() => undefined);
  };

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!markets.length) return;
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (
        target?.closest("input, textarea, select, [contenteditable='true']")
      ) {
        return;
      }
      if (event.key === "j" || event.key === "ArrowDown") {
        event.preventDefault();
        const idx = markets.findIndex((m) => m.slug === selectedSlug);
        const next = markets[(idx + 1 + markets.length) % markets.length];
        if (next) setSelectedSlug(next.slug);
      }
      if (event.key === "k" || event.key === "ArrowUp") {
        event.preventDefault();
        const idx = markets.findIndex((m) => m.slug === selectedSlug);
        const prev = markets[(idx - 1 + markets.length) % markets.length];
        if (prev) setSelectedSlug(prev.slug);
      }
      if (event.key.toLowerCase() === "p") {
        event.preventDefault();
        if (selectedRecommendation && !selectedRecommendation.prefill.disabled) {
          applyRecommendation(selectedRecommendation);
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [markets, selectedRecommendation, selectedSlug]);

  return (
    <div className="mx-auto flex w-full max-w-[1500px] flex-col gap-6 px-4 py-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-primary/15">
            <Signal className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-foreground">
              Up/Down Pro
            </h1>
            <p className="text-xs text-muted-foreground">
              Crypto windows only. Strategy advisory + user-confirmed execution.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {ASSETS.map((option) => (
            <Button
              key={option}
              size="sm"
              variant={asset === option ? "default" : "outline"}
              className="h-7 px-3 font-mono text-[10px]"
              onClick={() => setAsset(option)}
            >
              {option}
            </Button>
          ))}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {WINDOWS.map((option) => (
          <Button
            key={option}
            size="sm"
            variant={windowType === option ? "default" : "outline"}
            className="h-7 px-3 font-mono text-[10px]"
            onClick={() => setWindowType(option)}
          >
            {option.toUpperCase()}
          </Button>
        ))}
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto h-7 px-3 font-mono text-[10px]"
          onClick={() => {
            void marketsQuery.refetch();
            void signalQuery.refetch();
            void recommendationsQuery.refetch();
          }}
        >
          <RefreshCw className="mr-1 h-3 w-3" />
          Refresh
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[320px,1fr,420px]">
        <Card className="border-border/70 bg-card/70">
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 font-mono text-xs uppercase tracking-widest">
              <Activity className="h-3.5 w-3.5" />
              Market Rail
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {markets.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                No tradable up/down markets in this filter.
              </p>
            ) : (
              <div className="max-h-[70vh] space-y-1 overflow-y-auto pr-1">
                {markets.map((m) => {
                  const start = toMillis(m.event_start_time);
                  const end = toMillis(m.event_end_time);
                  const active = start <= nowMs && nowMs < end;
                  const selected = selectedSlug === m.slug;
                  const countdown = active
                    ? `T-${fmtCountdown((end - nowMs) / 1000)}`
                    : nowMs < start
                      ? `+${fmtCountdown((start - nowMs) / 1000)}`
                      : "Closed";
                  return (
                    <button
                      type="button"
                      key={m.slug}
                      onClick={() => setSelectedSlug(m.slug)}
                      className={cn(
                        "w-full rounded-md border px-3 py-2 text-left transition",
                        selected
                          ? "border-primary bg-primary/10"
                          : "border-border/50 bg-background/40 hover:border-primary/40",
                      )}
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-mono text-[11px] font-semibold">
                          {m.asset} {m.window_type.toUpperCase()}
                        </span>
                        <span
                          className={cn(
                            "rounded px-1.5 py-0.5 font-mono text-[10px]",
                            active
                              ? "bg-constructive/20 text-constructive"
                              : "bg-muted text-muted-foreground",
                          )}
                        >
                          {active ? "LIVE" : "NEXT"}
                        </span>
                      </div>
                      <div className="mt-1 flex items-center justify-between text-[10px] text-muted-foreground">
                        <span>{m.resolution_source_type}</span>
                        <span>{countdown}</span>
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>

        <div className="space-y-4">
          <Card className="border-border/70 bg-card/70">
            <CardHeader className="pb-2">
              <CardTitle className="flex items-center justify-between">
                <div className="flex items-center gap-2 font-mono text-xs uppercase tracking-widest">
                  <Gauge className="h-3.5 w-3.5" />
                  Active Signal
                </div>
                <span className="text-[11px] text-muted-foreground">
                  {activeCountdown}
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              {!selectedMarket || !selectedSignal ? (
                <p className="text-sm text-muted-foreground">
                  Select a market to load signal and recommendation.
                </p>
              ) : (
                <>
                  <div className="grid gap-3 sm:grid-cols-5">
                    <Metric
                      label="P_Market"
                      value={pct(selectedSignal.p_market_up)}
                    />
                    <Metric
                      label="P_Synth"
                      value={pct(selectedSignal.p_synth_up)}
                    />
                    <Metric
                      label="P_Model"
                      value={pct(selectedSignal.p_model_up)}
                    />
                    <Metric label="P_LP" value={pct(selectedSignal.p_lp_up)} />
                    <Metric
                      label="P_Final"
                      value={pct(selectedSignal.p_final_up)}
                      accent
                    />
                  </div>

                  <div className="grid gap-3 sm:grid-cols-5">
                    <Metric label="EV Up" value={money(selectedSignal.ev_up)} />
                    <Metric
                      label="EV Down"
                      value={money(selectedSignal.ev_down)}
                    />
                    <Metric
                      label="EV Gate"
                      value={money(selectedSignal.ev_min_threshold)}
                    />
                    <Metric
                      label="Confidence"
                      value={pct(selectedSignal.confidence)}
                    />
                    <Metric label="Regime" value={selectedSignal.regime} />
                  </div>

                  <div className="rounded-md border border-border/60 bg-background/40 p-3 text-xs">
                    <div className="mb-2 flex items-center justify-between">
                      <span className="font-mono uppercase tracking-wide text-muted-foreground">
                        Recommendation
                      </span>
                      <span className="font-mono text-foreground">
                        {selectedRecommendation?.decision ?? "NO_TRADE"}
                      </span>
                    </div>
                    <p className="text-muted-foreground">
                      {selectedRecommendation?.reason_codes?.join(" · ") ||
                        "No reason codes available."}
                    </p>
                  </div>

                  <div className="grid gap-3 sm:grid-cols-2">
                    <Card className="border-border/60 bg-background/40">
                      <CardHeader className="pb-1">
                        <CardTitle className="text-[11px] font-mono uppercase tracking-wide text-muted-foreground">
                          Microstructure
                        </CardTitle>
                      </CardHeader>
                      <CardContent className="space-y-1 text-xs">
                        <div className="flex items-center justify-between">
                          <span>Spread (Up)</span>
                          <span>
                            {(selectedSignal.spread_up * 100).toFixed(2)}¢
                          </span>
                        </div>
                        <div className="flex items-center justify-between">
                          <span>Spread (Down)</span>
                          <span>
                            {(selectedSignal.spread_down * 100).toFixed(2)}¢
                          </span>
                        </div>
                        <div className="flex items-center justify-between">
                          <span>Depth Imbalance</span>
                          <span>
                            {selectedSignal.depth_imbalance.toFixed(3)}
                          </span>
                        </div>
                        <div className="flex items-center justify-between">
                          <span>Expected Slippage</span>
                          <span>
                            {(
                              selectedSignal.expected_slippage * 10_000
                            ).toFixed(0)}{" "}
                            bps
                          </span>
                        </div>
                      </CardContent>
                    </Card>

                    <Card className="border-border/60 bg-background/40">
                      <CardHeader className="pb-1">
                        <CardTitle className="text-[11px] font-mono uppercase tracking-wide text-muted-foreground">
                          Risk Flags
                        </CardTitle>
                      </CardHeader>
                      <CardContent className="space-y-1 text-xs">
                        {Object.entries(selectedSignal.risk_flags)
                          .filter(([, v]) => Boolean(v))
                          .slice(0, 6)
                          .map(([k]) => (
                            <div
                              key={k}
                              className="flex items-center gap-2 text-amber-300"
                            >
                              <AlertTriangle className="h-3 w-3" />
                              <span>{k.replaceAll("_", " ")}</span>
                            </div>
                          ))}
                        {Object.values(selectedSignal.risk_flags).every(
                          (v) => !v,
                        ) ? (
                          <div className="text-muted-foreground">
                            No active risk flags.
                          </div>
                        ) : null}
                      </CardContent>
                    </Card>
                  </div>
                </>
              )}
            </CardContent>
          </Card>

          <Card className="border-border/70 bg-card/70">
            <CardHeader className="pb-2">
              <CardTitle className="flex items-center gap-2 font-mono text-xs uppercase tracking-widest">
                <Clock3 className="h-3.5 w-3.5" />
                Performance
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid gap-3 sm:grid-cols-4">
                <Metric
                  label="Trades"
                  value={String(performanceQuery.data?.trades ?? 0)}
                />
                <Metric
                  label="Hit Rate"
                  value={pct(performanceQuery.data?.hit_rate)}
                />
                <Metric
                  label="Brier"
                  value={(performanceQuery.data?.brier_score ?? 0).toFixed(4)}
                />
                <Metric
                  label="Realized EV"
                  value={money(performanceQuery.data?.realized_ev)}
                />
              </div>
            </CardContent>
          </Card>
        </div>

        <Card className="border-border/70 bg-card/70">
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center justify-between">
              <span className="flex items-center gap-2 font-mono text-xs uppercase tracking-widest">
                <ChevronRight className="h-3.5 w-3.5" />
                Execution Panel
              </span>
              {integrityFailure ? (
                <span className="inline-flex items-center gap-1 rounded bg-amber-500/20 px-2 py-0.5 text-[10px] text-amber-300">
                  <ShieldAlert className="h-3 w-3" />
                  Caution
                </span>
              ) : null}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {!selectedMarket || !liveMarket ? (
              <p className="text-sm text-muted-foreground">
                No market selected.
              </p>
            ) : (
              <>
                <div className="rounded-md border border-border/60 bg-background/40 p-3 text-xs">
                  <div className="flex items-center justify-between">
                    <span className="font-mono uppercase tracking-wide text-muted-foreground">
                      Strategy Action
                    </span>
                    <span className="font-semibold">
                      {selectedRecommendation?.decision ?? "NO_TRADE"}
                    </span>
                  </div>
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    <Button
                      size="sm"
                      className="h-7 px-3 font-mono text-[10px]"
                      onClick={() =>
                        applyRecommendation(selectedRecommendation)
                      }
                      disabled={
                        !selectedRecommendation ||
                        selectedRecommendation.prefill.disabled
                      }
                    >
                      Apply Prefill
                      <ArrowRight className="ml-1 h-3 w-3" />
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 px-3 font-mono text-[10px]"
                      onClick={() =>
                        rejectRecommendation(selectedRecommendation)
                      }
                      disabled={!selectedRecommendation}
                    >
                      Reject
                    </Button>
                    {selectedRecommendation?.prefill?.disabled_why ? (
                      <span className="text-[11px] text-amber-300">
                        {selectedRecommendation.prefill.disabled_why}
                      </span>
                    ) : null}
                    {prefillDriftBps > 0 ? (
                      <span className="text-[11px] text-muted-foreground">
                        Drift {prefillDriftBps.toFixed(0)} bps
                      </span>
                    ) : null}
                  </div>
                </div>
                <TradeForm
                  market={liveMarket}
                  recommendationPrefill={prefill}
                  externalBlockReason={
                    prefillBlockReason ??
                    (staleSignal
                      ? "Signal feed is stale. Trading is disabled until a fresh signal arrives."
                      : selectedSignal?.risk_flags?.kill_switch
                        ? "Up/Down kill switch is active."
                        : selectedSignal?.risk_flags?.data_integrity_failed
                          ? "Data integrity failure detected. Trading is disabled."
                          : null)
                  }
                />
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function Metric({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: boolean;
}) {
  return (
    <div
      className={cn(
        "rounded border border-border/60 bg-background/40 p-2",
        accent && "border-primary/50",
      )}
    >
      <div className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div
        className={cn("mt-1 text-sm font-semibold", accent && "text-primary")}
      >
        {value}
      </div>
    </div>
  );
}
