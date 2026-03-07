"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
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

const price = (value?: number) => {
  if (typeof value !== "number" || Number.isNaN(value) || value <= 0) return "--";
  return `$${value.toLocaleString(undefined, {
    maximumFractionDigits: 2,
    minimumFractionDigits: 2,
  })}`;
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

const formatClock = (value?: string) => {
  if (!value) return "--";
  const ts = Date.parse(value);
  if (Number.isNaN(ts)) return "--";
  return new Date(ts).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
};

const formatDateClock = (value?: string) => {
  if (!value) return "--";
  const ts = Date.parse(value);
  if (Number.isNaN(ts)) return "--";
  return new Date(ts).toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
};

const toMillis = (value?: string) => {
  if (!value) return 0;
  const ts = Date.parse(value);
  return Number.isNaN(ts) ? 0 : ts;
};

const deriveStartMs = (market: UpDownMarket, anchorMs: number) => {
  const parsed = toMillis(market.event_start_time);
  if (parsed > 0) return parsed;
  if (Number.isFinite(anchorMs) && Number.isFinite(market.time_to_start_seconds)) {
    return anchorMs + market.time_to_start_seconds * 1000;
  }
  return 0;
};

const deriveEndMs = (market: UpDownMarket, anchorMs: number) => {
  const parsed = toMillis(market.event_end_time);
  if (parsed > 0) return parsed;
  if (Number.isFinite(anchorMs) && Number.isFinite(market.time_to_end_seconds)) {
    return anchorMs + market.time_to_end_seconds * 1000;
  }
  return 0;
};

const WINDOW_ORDER: Record<string, number> = {
  "5m": 0,
  "15m": 1,
  "1h": 2,
  "4h": 3,
};

const normalizeSelectedSlug = (value: string | null | undefined): string | null => {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (trimmed === "" || trimmed === "null" || trimmed === "undefined") {
    return null;
  }
  return trimmed;
};

const isMarketActiveAt = (market: UpDownMarket, nowMs: number, anchorMs: number) => {
  const start = deriveStartMs(market, anchorMs);
  const end = deriveEndMs(market, anchorMs);
  if (start > 0 && end > 0) {
    return start <= nowMs && nowMs < end;
  }
  return Boolean(market.is_active_window);
};

const formatRiskFlag = (value: string) => value.replaceAll("_", " ");

type RailLane = {
  key: string;
  asset: string;
  windowType: string;
  live: UpDownMarket | null;
  next: UpDownMarket | null;
  queue: UpDownMarket[];
};

const buildRailLanes = (markets: UpDownMarket[], nowMs: number, anchorMs: number): RailLane[] => {
  if (!markets.length) return [];

  const grouped = new Map<string, UpDownMarket[]>();
  for (const market of markets) {
    const key = `${market.asset}|${market.window_type}`;
    const existing = grouped.get(key);
    if (existing) {
      existing.push(market);
    } else {
      grouped.set(key, [market]);
    }
  }

  const lanes: RailLane[] = [];
  for (const [key, groupedMarkets] of grouped) {
    const queue = [...groupedMarkets]
      .filter((market) => deriveEndMs(market, anchorMs) > nowMs)
      .sort((left, right) => {
        const startDiff = deriveStartMs(left, anchorMs) - deriveStartMs(right, anchorMs);
        if (startDiff !== 0) return startDiff;
        const endDiff = deriveEndMs(left, anchorMs) - deriveEndMs(right, anchorMs);
        if (endDiff !== 0) return endDiff;
        return left.slug.localeCompare(right.slug);
      });
    if (!queue.length) continue;

    const liveCandidates = queue.filter((market) => isMarketActiveAt(market, nowMs, anchorMs));
    const live =
      liveCandidates.sort(
        (left, right) => deriveEndMs(left, anchorMs) - deriveEndMs(right, anchorMs),
      )[0] ?? null;

    const future = queue.filter((market) => deriveStartMs(market, anchorMs) > nowMs);
    const next = future[0] ?? null;

    if (!live && !next) continue;

    const [asset, windowType] = key.split("|");
    lanes.push({
      key,
      asset,
      windowType,
      live,
      next,
      queue,
    });
  }

  lanes.sort((left, right) => {
    if (!!left.live !== !!right.live) {
      return left.live ? -1 : 1;
    }

    const leftTime = left.live
      ? deriveEndMs(left.live, nowMs)
      : left.next
        ? deriveStartMs(left.next, anchorMs)
        : 0;
    const rightTime = right.live
      ? deriveEndMs(right.live, anchorMs)
      : right.next
        ? deriveStartMs(right.next, anchorMs)
        : 0;
    if (leftTime !== rightTime) {
      return leftTime - rightTime;
    }

    const leftAsset = left.asset.localeCompare(right.asset);
    if (leftAsset !== 0) return leftAsset;

    return (WINDOW_ORDER[left.windowType] ?? 99) - (WINDOW_ORDER[right.windowType] ?? 99);
  });

  return lanes;
};

const flattenRailActiveMarkets = (lanes: RailLane[]): UpDownMarket[] => {
  return lanes
    .map((lane) => lane.live)
    .filter((market): market is UpDownMarket => Boolean(market));
};

const marketCountdown = (market: UpDownMarket, nowMs: number, anchorMs: number): string => {
  const start = deriveStartMs(market, anchorMs);
  const end = deriveEndMs(market, anchorMs);
  const isActive = isMarketActiveAt(market, nowMs, anchorMs);
  if (isActive) {
    const remaining = end > 0 ? (end - nowMs) / 1000 : market.time_to_end_seconds;
    return `Ends in ${fmtCountdown(remaining)}`;
  }
  const untilStart =
    start > 0 ? (start - nowMs) / 1000 : market.time_to_start_seconds;
  if (untilStart > 0) {
    return `Starts in ${fmtCountdown(untilStart)}`;
  }
  return "Closed";
};

const findLaneForSlug = (lanes: RailLane[], slug: string | null): RailLane | null => {
  if (!slug) return null;
  return lanes.find((lane) => lane.queue.some((market) => market.slug === slug)) ?? null;
};

const pickNextMarket = (
  lanes: RailLane[],
  currentSlug: string | null,
): string | null => {
  const activeMarkets = flattenRailActiveMarkets(lanes);
  if (!activeMarkets.length) return null;

  const currentLane = findLaneForSlug(lanes, currentSlug);
  if (currentLane?.live) {
    return currentLane.live.slug;
  }

  return activeMarkets[0]?.slug ?? null;
};

const hasSynthProbabilities = (signal: UpDownSignal | null) =>
  !!signal &&
  (typeof signal.p_market_up === "number" ||
    typeof signal.p_synth_up === "number" ||
    typeof signal.p_model_up === "number" ||
    typeof signal.p_lp_up === "number");

export default function UpDownPage() {
  const [asset, setAsset] = useState<(typeof ASSETS)[number]>("ALL");
  const [windowType, setWindowType] = useState<(typeof WINDOWS)[number]>("all");
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);
  const [liveSignals, setLiveSignals] = useState<Record<string, UpDownSignal>>({});
  const [prefill, setPrefill] = useState<TradeRecommendationPrefill | null>(null);
  const [prefillBlockReason, setPrefillBlockReason] = useState<string | null>(null);
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

  const normalizedSelectedSlug = normalizeSelectedSlug(selectedSlug);

  const signalQuery = useQuery({
    queryKey: ["updown-signal", normalizedSelectedSlug],
    queryFn: () => fetchUpDownSignal(normalizedSelectedSlug as string),
    enabled: normalizedSelectedSlug !== null,
    refetchInterval: 2_500,
    staleTime: 2_000,
  });

  const markets = marketsQuery.data ?? [];
  const marketAnchorMs = marketsQuery.dataUpdatedAt > 0 ? marketsQuery.dataUpdatedAt : nowMs;
  const railSourceMarkets = useMemo(() => {
    const tradable = markets.filter((market) => market.tradable);
    return tradable.length ? tradable : markets;
  }, [markets]);

  const railLanes = useMemo(
    () => buildRailLanes(railSourceMarkets, nowMs, marketAnchorMs),
    [railSourceMarkets, nowMs, marketAnchorMs],
  );

  const railActiveMarkets = useMemo(() => flattenRailActiveMarkets(railLanes), [railLanes]);

  const selectedMarket = useMemo(
    () =>
      railSourceMarkets.find((market) => market.slug === normalizedSelectedSlug) ??
      markets.find((market) => market.slug === normalizedSelectedSlug) ??
      null,
    [markets, normalizedSelectedSlug, railSourceMarkets],
  );
  const activeConditionIds = useMemo(
    () =>
      Array.from(
        new Set(railActiveMarkets.map((market) => market.condition_id).filter(Boolean)),
      ).sort(),
    [railActiveMarkets],
  );

  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    if (!railActiveMarkets.length) {
      if (selectedSlug !== null) {
        setSelectedSlug(null);
      }
      return;
    }

    if (
      normalizedSelectedSlug !== null &&
      railActiveMarkets.some((market) => market.slug === normalizedSelectedSlug)
    ) {
      return;
    }

    const next = pickNextMarket(railLanes, normalizedSelectedSlug);
    if (next !== normalizedSelectedSlug) {
      setSelectedSlug(next);
    }
  }, [railActiveMarkets, railLanes, nowMs, normalizedSelectedSlug, selectedSlug]);

  useEffect(() => {
    if (railSourceMarkets.length === 0) {
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
        } catch {
          // ignore malformed payloads
        }
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
  }, [railSourceMarkets.length]);

  useEffect(() => {
    const conditionId = selectedMarket?.condition_id;
    if (!conditionId) return;
    requestMarketStream(conditionId).catch(() => undefined);
  }, [selectedMarket?.condition_id]);

  useEffect(() => {
    if (!activeConditionIds.length) return;
    for (const conditionId of activeConditionIds) {
      requestMarketStream(conditionId).catch(() => undefined);
    }
  }, [activeConditionIds.join("|")]);

  const selectedSignal = useMemo(() => {
    if (!normalizedSelectedSlug) return null;
    const liveSignal = liveSignals[normalizedSelectedSlug] ?? null;
    const polledSignal = signalQuery.data ?? null;
    if (!liveSignal) return polledSignal;
    if (!polledSignal) return liveSignal;

    const liveTs = toMillis(liveSignal.timestamp);
    const polledTs = toMillis(polledSignal.timestamp);
    if (!liveTs) return polledSignal;
    if (!polledTs) return liveSignal;
    return polledTs > liveTs ? polledSignal : liveSignal;
  }, [liveSignals, normalizedSelectedSlug, signalQuery.data]);

  const selectedRecommendation = useMemo(() => {
    if (selectedSignal?.recommendation) return selectedSignal.recommendation;
    return (
      (recommendationsQuery.data ?? []).find(
        (recommendation) => recommendation.slug === normalizedSelectedSlug,
      ) ?? null
    );
  }, [recommendationsQuery.data, selectedSignal?.recommendation, normalizedSelectedSlug]);

  const staleSignal = useMemo(() => {
    const ts = toMillis(selectedSignal?.timestamp);
    if (!ts) return true;
    return nowMs - ts > 30_000;
  }, [selectedSignal?.timestamp, nowMs]);

  const activeRiskFlags = useMemo(() => {
    if (!selectedSignal) return [];
    return Object.entries(selectedSignal.risk_flags ?? {})
      .filter(([, enabled]) => Boolean(enabled))
      .map(([flag]) => flag);
  }, [selectedSignal]);

  const activeCountdown = useMemo(() => {
    if (!selectedMarket) return "--";
    return marketCountdown(selectedMarket, nowMs, marketAnchorMs);
  }, [nowMs, selectedMarket, marketAnchorMs]);

  const referenceAgeSeconds = useMemo(() => {
    const ts = toMillis(selectedSignal?.reference_updated_at ?? selectedSignal?.timestamp);
    if (!ts) return null;
    return Math.max(0, Math.floor((nowMs - ts) / 1000));
  }, [selectedSignal?.reference_updated_at, selectedSignal?.timestamp, nowMs]);

  const startSnapshotMissing = useMemo(() => {
    if (!selectedMarket || !selectedSignal) return false;
    const start = deriveStartMs(selectedMarket, marketAnchorMs);
    if (!start || nowMs < start) return false;
    return typeof selectedSignal.reference_start_price !== "number";
  }, [selectedMarket, selectedSignal, nowMs, marketAnchorMs]);

  const liveMarket = selectedMarket ? augmentMarket(selectedMarket.market) : null;
  const signalHasSynth = hasSynthProbabilities(selectedSignal);

  const integrityFailure =
    staleSignal ||
    !!selectedSignal?.risk_flags?.data_integrity_failed ||
    !!selectedSignal?.risk_flags?.kill_switch ||
    !!selectedRecommendation?.prefill?.disabled ||
    !!prefillBlockReason;

  const prefillDriftBps = useMemo(() => {
    if (!prefill || prefill.disabled || !selectedSignal || !selectedMarket) return 0;
    const isUpOutcome = prefill.outcomeIndex === selectedMarket.outcome_index_up;
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
      if (!railActiveMarkets.length) return;
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable='true']")) {
        return;
      }

      if (event.key === "j" || event.key === "ArrowDown") {
        event.preventDefault();
        const idx = railActiveMarkets.findIndex((market) => market.slug === normalizedSelectedSlug);
        const next = railActiveMarkets[(idx + 1 + railActiveMarkets.length) % railActiveMarkets.length];
        if (next) setSelectedSlug(next.slug);
      }

      if (event.key === "k" || event.key === "ArrowUp") {
        event.preventDefault();
        const idx = railActiveMarkets.findIndex((market) => market.slug === normalizedSelectedSlug);
        const prev = railActiveMarkets[(idx - 1 + railActiveMarkets.length) % railActiveMarkets.length];
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
  }, [normalizedSelectedSlug, railActiveMarkets, selectedRecommendation]);

  return (
    <div className="mx-auto flex w-full max-w-[1660px] flex-col gap-5 px-4 py-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-primary/15">
            <Signal className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-foreground">Up/Down Pro</h1>
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

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[320px,minmax(0,1fr),360px] 2xl:grid-cols-[340px,minmax(0,1fr),380px]">
        <Card className="border-border/70 bg-card/70">
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 font-mono text-xs uppercase tracking-widest">
              <Activity className="h-3.5 w-3.5" />
              Market Rail
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {marketsQuery.isError ? (
              <p className="text-xs text-destructive">Failed to load up/down markets.</p>
            ) : railLanes.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                No up/down markets are available for this filter.
              </p>
            ) : (
              <>
                <div className="flex items-center justify-between px-1 text-[10px] font-mono uppercase tracking-wide text-muted-foreground">
                  <span>{railLanes.filter((lane) => lane.live).length} live lanes</span>
                  <span>{railLanes.length} total lanes</span>
                </div>
                <div className="max-h-[72vh] space-y-3 overflow-y-auto pr-1">
                  {railLanes.map((lane) => (
                    <RailLaneCard
                      key={lane.key}
                      lane={lane}
                      nowMs={nowMs}
                      anchorMs={marketAnchorMs}
                      selectedSlug={normalizedSelectedSlug}
                      onSelect={setSelectedSlug}
                    />
                  ))}
                </div>
              </>
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
                <span className="text-[11px] text-muted-foreground">{activeCountdown}</span>
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {!selectedMarket ? (
                <div className="rounded-md border border-border/60 bg-background/40 p-4 text-sm text-muted-foreground">
                  No active windows in this filter. Upcoming windows stay read-only until the
                  start boundary, then auto-slot into the active rail.
                </div>
              ) : !selectedSignal ? (
                <div className="rounded-md border border-border/60 bg-background/40 p-4 text-sm text-muted-foreground">
                  Active market selected, waiting for a fresh signal snapshot.
                </div>
              ) : (
                <>
                  <div className="rounded-md border border-border/60 bg-background/40 p-3">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="rounded bg-primary/15 px-2 py-0.5 font-mono text-[10px] uppercase tracking-wide text-primary">
                          {selectedMarket.asset} {selectedMarket.window_type.toUpperCase()}
                        </span>
                        <span className="rounded bg-muted px-2 py-0.5 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
                          {(selectedMarket.resolution_source_type || "unknown").toUpperCase()}
                        </span>
                        <span
                          className={cn(
                            "rounded px-2 py-0.5 font-mono text-[10px] uppercase tracking-wide",
                            staleSignal
                              ? "bg-amber-500/20 text-amber-300"
                              : "bg-constructive/20 text-constructive",
                          )}
                        >
                          {staleSignal ? "STALE" : "LIVE"}
                        </span>
                      </div>
                      <span className="font-mono text-xs text-foreground">
                        {selectedRecommendation?.decision ?? "NO_TRADE"}
                      </span>
                    </div>
                    <div className="mt-2 text-sm font-semibold text-foreground">
                      {selectedMarket.market?.title || selectedMarket.slug}
                    </div>
                    <div className="mt-3 grid grid-cols-2 gap-2 lg:grid-cols-3 2xl:grid-cols-5">
                      <Metric label="P_Market" value={pct(selectedSignal.p_market_up)} />
                      <Metric label="P_Synth" value={pct(selectedSignal.p_synth_up)} />
                      <Metric label="P_Model" value={pct(selectedSignal.p_model_up)} />
                      <Metric label="P_LP" value={pct(selectedSignal.p_lp_up)} />
                      <Metric
                        label="P_Final"
                        value={signalHasSynth ? pct(selectedSignal.p_final_up) : "--"}
                        accent
                      />
                    </div>
                  </div>

                  <div className="grid gap-3 2xl:grid-cols-2">
                    <div className="space-y-3">
                      <div className="rounded-md border border-border/60 bg-background/40 p-3">
                        <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
                          Execution Quotes
                        </div>
                        <div className="grid grid-cols-2 gap-2">
                          <Metric label="Up Ask" value={pct(selectedSignal.executable_ask_up)} />
                          <Metric label="Down Ask" value={pct(selectedSignal.executable_ask_down)} />
                          <Metric label="Up Bid" value={pct(selectedSignal.executable_bid_up)} />
                          <Metric label="Down Bid" value={pct(selectedSignal.executable_bid_down)} />
                        </div>
                      </div>

                      <div className="rounded-md border border-border/60 bg-background/40 p-3">
                        <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
                          Edge + Confidence
                        </div>
                        <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
                          <Metric label="EV Up" value={signalHasSynth ? money(selectedSignal.ev_up) : "--"} />
                          <Metric
                            label="EV Down"
                            value={signalHasSynth ? money(selectedSignal.ev_down) : "--"}
                          />
                          <Metric label="EV Gate" value={money(selectedSignal.ev_min_threshold)} />
                          <Metric label="Confidence" value={pct(selectedSignal.confidence)} />
                        </div>
                        <div className="mt-2 rounded border border-border/50 bg-background/30 px-2.5 py-2">
                          <div className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
                            Regime
                          </div>
                          <div className="mt-1 text-sm font-semibold text-foreground">
                            {selectedSignal.regime || "--"}
                          </div>
                        </div>
                      </div>
                    </div>

                    <div className="space-y-3">
                      <div className="rounded-md border border-border/60 bg-background/40 p-3">
                        <div className="mb-2 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
                          Oracle Reference
                        </div>
                        <div className="grid grid-cols-2 gap-2">
                          <Metric
                            label="Start Snapshot"
                            value={price(selectedSignal.reference_start_price)}
                            accent={startSnapshotMissing}
                          />
                          <Metric label="Oracle Now" value={price(selectedSignal.reference_current_price)} />
                          <Metric
                            label="End Price"
                            value={
                              typeof selectedSignal.reference_end_price === "number"
                                ? price(selectedSignal.reference_end_price)
                                : isMarketActiveAt(selectedMarket, nowMs, marketAnchorMs)
                                  ? "Pending"
                                  : "--"
                            }
                          />
                          <Metric
                            label="Ref Age"
                            value={
                              referenceAgeSeconds === null
                                ? "--"
                                : referenceAgeSeconds < 1
                                  ? "<1s"
                                  : `${referenceAgeSeconds}s`
                            }
                          />
                          <Metric label="Window Start" value={formatClock(selectedMarket.event_start_time)} />
                          <Metric label="Window End" value={formatClock(selectedMarket.event_end_time)} />
                        </div>
                        {startSnapshotMissing ? (
                          <p className="mt-2 text-[11px] text-amber-300">
                            Start snapshot is missing after window open. Execution should stay guarded.
                          </p>
                        ) : null}
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
                        <div className="mt-2 flex flex-wrap gap-1">
                          {activeRiskFlags.length ? (
                            activeRiskFlags.map((flag) => (
                              <span
                                key={flag}
                                className="rounded bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] text-amber-300"
                              >
                                {formatRiskFlag(flag)}
                              </span>
                            ))
                          ) : (
                            <span className="text-[11px] text-muted-foreground">
                              No active risk flags.
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
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
                <Metric label="Trades" value={String(performanceQuery.data?.trades ?? 0)} />
                <Metric label="Hit Rate" value={pct(performanceQuery.data?.hit_rate)} />
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
              <p className="text-sm text-muted-foreground">No market selected.</p>
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
                      onClick={() => applyRecommendation(selectedRecommendation)}
                      disabled={!selectedRecommendation || selectedRecommendation.prefill.disabled}
                    >
                      Apply Prefill
                      <ArrowRight className="ml-1 h-3 w-3" />
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 px-3 font-mono text-[10px]"
                      onClick={() => rejectRecommendation(selectedRecommendation)}
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
                  {selectedRecommendation && !selectedRecommendation.prefill.disabled ? (
                    <div className="mt-2 grid grid-cols-3 gap-2 rounded border border-border/50 bg-background/50 p-2 text-[10px] font-mono text-muted-foreground">
                      <span>
                        Side: {selectedRecommendation.recommended_side}
                      </span>
                      <span>
                        Limit: {selectedRecommendation.prefill.limit_price.toFixed(3)}
                      </span>
                      <span>
                        Size: {selectedRecommendation.prefill.size_shares.toFixed(2)}
                      </span>
                    </div>
                  ) : null}
                </div>

                <TradeForm
                  market={liveMarket}
                  mode="compact"
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
        "min-h-[74px] rounded border border-border/60 bg-background/40 p-2",
        accent && "border-primary/50",
      )}
    >
      <div className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className={cn("mt-1 break-words text-base font-semibold leading-5", accent && "text-primary")}>
        {value}
      </div>
    </div>
  );
}

function RailLaneCard({
  lane,
  nowMs,
  anchorMs,
  selectedSlug,
  onSelect,
}: {
  lane: RailLane;
  nowMs: number;
  anchorMs: number;
  selectedSlug: string | null;
  onSelect: (slug: string) => void;
}) {
  const upcomingCount = lane.queue.filter((market) => deriveStartMs(market, anchorMs) > nowMs).length;
  const next = lane.next && lane.next.slug !== lane.live?.slug ? lane.next : null;

  return (
    <div className="rounded-lg border border-border/60 bg-background/30 p-2.5">
      <div className="mb-2 flex items-center justify-between">
        <div className="font-mono text-xs uppercase tracking-wide text-foreground">
          {lane.asset} {lane.windowType.toUpperCase()}
        </div>
        <span
          className={cn(
            "rounded px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide",
            lane.live
              ? "bg-constructive/20 text-constructive"
              : "bg-muted text-muted-foreground",
          )}
        >
          {lane.live ? "Live" : "Queued"}
        </span>
      </div>

      <div className="space-y-2">
        {lane.live ? (
          <RailMarketRow
            label="Current"
            market={lane.live}
            nowMs={nowMs}
            anchorMs={anchorMs}
            selectedSlug={selectedSlug}
            onSelect={onSelect}
            selectable
          />
        ) : (
          <div className="rounded-md border border-dashed border-border/50 px-2.5 py-2 text-[11px] text-muted-foreground">
            <span className="font-mono uppercase tracking-wide">Current</span>
            <span className="ml-2">Awaiting activation</span>
          </div>
        )}
        <RailMarketRow
          label="Next"
          market={next}
          nowMs={nowMs}
          anchorMs={anchorMs}
          selectedSlug={selectedSlug}
          onSelect={onSelect}
          selectable={false}
        />
      </div>

      <div className="mt-2 flex items-center justify-between text-[10px] text-muted-foreground">
        <span>{upcomingCount} queued</span>
        <span>{(lane.live ?? lane.next)?.resolution_source_type ?? "unknown"}</span>
      </div>
    </div>
  );
}

function RailMarketRow({
  label,
  market,
  nowMs,
  anchorMs,
  selectedSlug,
  onSelect,
  selectable,
}: {
  label: string;
  market: UpDownMarket | null;
  nowMs: number;
  anchorMs: number;
  selectedSlug: string | null;
  onSelect: (slug: string) => void;
  selectable?: boolean;
}) {
  if (!market) {
    return (
      <div className="rounded-md border border-dashed border-border/50 px-2.5 py-2 text-[11px] text-muted-foreground">
        <span className="font-mono uppercase tracking-wide">{label}</span>
        <span className="ml-2">No market</span>
      </div>
    );
  }

  const start = deriveStartMs(market, anchorMs);
  const isLive = isMarketActiveAt(market, nowMs, anchorMs);
  const selected = selectedSlug === market.slug;
  const countdown = marketCountdown(market, nowMs, anchorMs);
  const canSelect = Boolean(selectable && isLive);
  const stateLabel = isLive ? "LIVE" : start > nowMs ? "QUEUED" : "CLOSED";

  return (
    <button
      type="button"
      disabled={!canSelect}
      onClick={() => {
        if (canSelect) onSelect(market.slug);
      }}
      className={cn(
        "w-full rounded-md border px-2.5 py-2 text-left transition",
        selected
          ? "border-primary bg-primary/10"
          : canSelect
            ? "border-border/50 bg-background/40 hover:border-primary/40"
            : "border-border/40 bg-background/20 opacity-80",
        !canSelect && "cursor-not-allowed",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
        <span
          className={cn(
            "rounded px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide",
            isLive
              ? "bg-constructive/20 text-constructive"
              : "bg-muted text-muted-foreground",
          )}
        >
          {stateLabel}
        </span>
      </div>
      <div className="mt-1 font-mono text-[12px] font-semibold text-foreground">
        {market.asset} {market.window_type.toUpperCase()}
      </div>
      <div className="mt-1 truncate text-[10px] text-muted-foreground">
        {market.market?.title || market.slug}
      </div>
      <div className="mt-1.5 flex items-center justify-between text-[10px] text-muted-foreground">
        <span>{countdown}</span>
        <span>{formatDateClock(market.event_start_time)}</span>
      </div>
    </button>
  );
}
