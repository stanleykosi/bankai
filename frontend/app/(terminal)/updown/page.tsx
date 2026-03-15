"use client";

import Image from "next/image";
import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  ArrowUpDown,
  BrainCircuit,
  ChevronDown,
  ChevronUp,
  Info,
  RefreshCw,
  ShieldAlert,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  TradeForm,
  type TradeRecommendationPrefill,
} from "@/components/terminal/TradeForm";
import { usePriceStream } from "@/hooks/usePriceStream";
import { requestMarketStream } from "@/lib/market-data";
import {
  createUpDownEventSource,
  fetchUpDownLLMHealth,
  fetchUpDownLLMPacket,
  fetchUpDownMarkets,
  fetchUpDownPerformance,
  fetchUpDownRecommendations,
  fetchUpDownSignal,
  generateUpDownLLMPacket,
  logUpDownDecision,
} from "@/lib/updown-api";
import type {
  LLMTradePacket,
  Recommendation,
  UpDownMarket,
  UpDownSignal,
} from "@/types";
import { cn } from "@/lib/utils";

const ASSETS = ["ALL", "BTC", "ETH", "SOL", "XRP"] as const;
const WINDOWS = ["all", "5m", "15m", "1h", "4h"] as const;
const EXECUTION_SOURCES = ["llm", "deterministic"] as const;
const ASSET_ICON_MAP: Record<string, string> = {
  BTC: "/icons/crypto/btc.svg",
  ETH: "/icons/crypto/eth.svg",
  SOL: "/icons/crypto/sol.svg",
  XRP: "/icons/crypto/xrp.svg",
};
const PREFILL_DRIFT_BPS = 35;
const STREAM_RETRY_BASE_MS = 1200;
const STREAM_RETRY_MAX_MS = 15000;
const STREAM_REQUEST_RETRY_BASE_MS = 1000;
const STREAM_REQUEST_RETRY_MAX_MS = 12000;
const STREAM_REQUEST_REFRESH_MS = 90_000;
const STREAM_REQUEST_HEARTBEAT_MS = 30_000;
const LLM_PACKET_REQUIRED_WARNING = "Generate LLM directional packet before autofill.";

type StatusTone = "live" | "upcoming" | "warning" | "locked" | "muted";

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
      ? deriveEndMs(left.live, anchorMs)
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

const marketCountdownSeconds = (market: UpDownMarket, nowMs: number, anchorMs: number) => {
  const start = deriveStartMs(market, anchorMs);
  const end = deriveEndMs(market, anchorMs);
  const isActive = isMarketActiveAt(market, nowMs, anchorMs);
  if (isActive) {
    if (end > 0) return Math.max(0, (end - nowMs) / 1000);
    return Number.POSITIVE_INFINITY;
  }
  if (start > nowMs) {
    return (start - nowMs) / 1000;
  }
  return Number.POSITIVE_INFINITY;
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

const impliedUpProbability = (up: number | undefined, down: number | undefined) => {
  if (typeof up === "number" && up > 0 && typeof down === "number" && down > 0) {
    const total = up + down;
    if (total > 0) return Math.min(0.99, Math.max(0.01, up / total));
  }
  if (typeof up === "number" && up > 0) return Math.min(0.99, Math.max(0.01, up));
  if (typeof down === "number" && down > 0) return Math.min(0.99, Math.max(0.01, 1 - down));
  return null;
};

const computeLiveMarketProbability = (
  market: UpDownMarket,
  liveMarket: UpDownMarket["market"] | null,
  fallback?: number,
) => {
  if (!liveMarket) return fallback;

  const upAsk =
    market.outcome_index_up === 0 ? liveMarket.yes_best_ask : liveMarket.no_best_ask;
  const downAsk =
    market.outcome_index_down === 0 ? liveMarket.yes_best_ask : liveMarket.no_best_ask;
  const fromAsk = impliedUpProbability(upAsk, downAsk);
  if (typeof fromAsk === "number") return fromAsk;

  const upLast = market.outcome_index_up === 0 ? liveMarket.yes_price : liveMarket.no_price;
  const downLast = market.outcome_index_down === 0 ? liveMarket.yes_price : liveMarket.no_price;
  const fromLast = impliedUpProbability(upLast, downLast);
  if (typeof fromLast === "number") return fromLast;

  return fallback;
};

const signalDelayed = (signal: UpDownSignal | null, nowMs: number) => {
  if (!signal?.timestamp) return false;
  const ts = toMillis(signal.timestamp);
  if (!ts) return false;
  return nowMs - ts > 30_000;
};

const uniqueStrings = (values: (string | null | undefined)[]) => {
  const seen = new Set<string>();
  const output: string[] = [];
  for (const value of values) {
    if (!value) continue;
    if (seen.has(value)) continue;
    seen.add(value);
    output.push(value);
  }
  return output;
};

const TABLE_ROW_COLUMNS =
  "grid min-w-[1220px] grid-cols-[minmax(260px,2.4fr)_88px_132px_118px_142px_142px_104px_112px_86px] items-center gap-3";

type MarketSortKey =
  | "market"
  | "window"
  | "status"
  | "countdown"
  | "strategy"
  | "ai"
  | "confidence"
  | "edge";

type SortDirection = "asc" | "desc";

const formatDecisionLabel = (value?: string | null) => {
  if (!value) return "--";
  return value.replace(/_/g, " ");
};

const compareOptionalNumbers = (left?: number, right?: number) => {
  const l = typeof left === "number" && Number.isFinite(left) ? left : null;
  const r = typeof right === "number" && Number.isFinite(right) ? right : null;
  if (l === null && r === null) return 0;
  if (l === null) return 1;
  if (r === null) return -1;
  return l - r;
};

export default function UpDownPage() {
  const queryClient = useQueryClient();
  const [asset, setAsset] = useState<(typeof ASSETS)[number]>("ALL");
  const [windowType, setWindowType] = useState<(typeof WINDOWS)[number]>("all");
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);
  const [liveSignals, setLiveSignals] = useState<Record<string, UpDownSignal>>({});
  const requestedStreamsRef = useRef<Map<string, number>>(new Map());
  const [prefill, setPrefill] = useState<TradeRecommendationPrefill | null>(null);
  const [prefillBlockReason, setPrefillBlockReason] = useState<string | null>(null);
  const [executionSource, setExecutionSource] =
    useState<(typeof EXECUTION_SOURCES)[number]>("llm");
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

  const llmPacketQuery = useQuery({
    queryKey: ["updown-llm-packet", normalizedSelectedSlug],
    queryFn: () => fetchUpDownLLMPacket(normalizedSelectedSlug as string),
    enabled: normalizedSelectedSlug !== null,
    refetchInterval: 5_000,
    staleTime: 2_000,
    retry: false,
  });

  const llmHealthQuery = useQuery({
    queryKey: ["updown-llm-health"],
    queryFn: () => fetchUpDownLLMHealth(),
    refetchInterval: 30_000,
    staleTime: 10_000,
    retry: false,
  });

  const llmGenerateMutation = useMutation({
    mutationFn: (payload: { slug: string; force_refresh?: boolean }) =>
      generateUpDownLLMPacket(payload),
    onSuccess: (packet: LLMTradePacket, variables) => {
      const requestedSlug = normalizeSelectedSlug(variables.slug);
      if (requestedSlug) {
        queryClient.setQueryData(["updown-llm-packet", requestedSlug], packet);
      }
      const responseSlug = normalizeSelectedSlug(packet.slug);
      if (responseSlug && responseSlug !== requestedSlug) {
        queryClient.setQueryData(["updown-llm-packet", responseSlug], packet);
      }
    },
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

  const decisionMarkets = useMemo(() => {
    const ordered: UpDownMarket[] = [];
    const seen = new Set<string>();

    for (const lane of railLanes) {
      for (const market of lane.queue) {
        if (seen.has(market.slug)) continue;
        seen.add(market.slug);
        ordered.push(market);
      }
    }

    for (const market of railSourceMarkets) {
      if (seen.has(market.slug)) continue;
      seen.add(market.slug);
      ordered.push(market);
    }

    return ordered;
  }, [railLanes, railSourceMarkets]);

  const selectedMarket = useMemo(
    () =>
      railSourceMarkets.find((market) => market.slug === normalizedSelectedSlug) ??
      markets.find((market) => market.slug === normalizedSelectedSlug) ??
      null,
    [markets, normalizedSelectedSlug, railSourceMarkets],
  );

  const streamRequestTargetsKey = useMemo(() => {
    const targets = new Set<string>();
    const selectedConditionId = selectedMarket?.condition_id?.trim();
    if (selectedConditionId) {
      targets.add(selectedConditionId);
    }
    for (const lane of railLanes) {
      const liveConditionId = lane.live?.condition_id?.trim();
      if (liveConditionId) {
        targets.add(liveConditionId);
      }
      const nextConditionId = lane.next?.condition_id?.trim();
      if (nextConditionId) {
        targets.add(nextConditionId);
      }
    }
    return Array.from(targets).sort().join("|");
  }, [railLanes, selectedMarket?.condition_id]);

  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    if (!railSourceMarkets.length) {
      if (selectedSlug !== null) {
        setSelectedSlug(null);
      }
      return;
    }

    if (
      normalizedSelectedSlug !== null &&
      railSourceMarkets.some((market) => market.slug === normalizedSelectedSlug)
    ) {
      return;
    }

    if (normalizedSelectedSlug !== null) {
      setSelectedSlug(null);
    }
  }, [railSourceMarkets, normalizedSelectedSlug, selectedSlug]);

  useEffect(() => {
    let source: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;
    let attempts = 0;

    const clearRetry = () => {
      if (retry) {
        clearTimeout(retry);
        retry = null;
      }
    };

    const scheduleReconnect = () => {
      if (stopped) return;
      clearRetry();
      attempts += 1;
      const exponential = STREAM_RETRY_BASE_MS * 2 ** Math.min(attempts - 1, 5);
      const backoff = Math.min(STREAM_RETRY_MAX_MS, exponential);
      const jitter = Math.floor(Math.random() * 400);
      retry = setTimeout(connect, backoff + jitter);
    };

    const connect = () => {
      if (stopped) return;
      const nextSource = createUpDownEventSource();
      source = nextSource;

      nextSource.onopen = () => {
        attempts = 0;
      };

      nextSource.onmessage = (event) => {
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

      nextSource.onerror = () => {
        if (source !== nextSource) {
          return;
        }
        nextSource.close();
        source = null;
        scheduleReconnect();
      };
    };

    connect();
    return () => {
      stopped = true;
      clearRetry();
      source?.close();
    };
  }, []);

  useEffect(() => {
    let retry: ReturnType<typeof setTimeout> | null = null;
    let heartbeat: ReturnType<typeof setInterval> | null = null;
    let stopped = false;
    let attempts = 0;

    const clearRetry = () => {
      if (retry) {
        clearTimeout(retry);
        retry = null;
      }
    };

    const scheduleRetry = () => {
      if (stopped) return;
      clearRetry();
      attempts += 1;
      const exponential = STREAM_REQUEST_RETRY_BASE_MS * 2 ** Math.min(attempts - 1, 5);
      const backoff = Math.min(STREAM_REQUEST_RETRY_MAX_MS, exponential);
      const jitter = Math.floor(Math.random() * 250);
      retry = setTimeout(() => {
        void requestPending();
      }, backoff + jitter);
    };

    const requestPending = async () => {
      if (stopped) return;
      clearRetry();
      const targets = streamRequestTargetsKey ? streamRequestTargetsKey.split("|") : [];
      const now = Date.now();

      // Keep this map bounded to current lane/selection targets.
      for (const tracked of Array.from(requestedStreamsRef.current.keys())) {
        if (!targets.includes(tracked)) {
          requestedStreamsRef.current.delete(tracked);
        }
      }

      const pending = targets.filter((conditionId) => {
        const lastRequestedAt = requestedStreamsRef.current.get(conditionId) ?? 0;
        return now - lastRequestedAt >= STREAM_REQUEST_REFRESH_MS;
      });
      if (!pending.length) {
        attempts = 0;
        return;
      }

      const results = await Promise.allSettled(
        pending.map((conditionId) => requestMarketStream(conditionId)),
      );
      if (stopped) return;

      let sawFailure = false;
      for (let i = 0; i < pending.length; i += 1) {
        const conditionId = pending[i];
        const result = results[i];
        if (result?.status === "fulfilled") {
          requestedStreamsRef.current.set(conditionId, now);
          continue;
        }
        sawFailure = true;
        if (process.env.NODE_ENV !== "production") {
          console.warn("Failed to request up/down market stream", conditionId, result?.reason);
        }
      }

      if (sawFailure) {
        scheduleRetry();
        return;
      }

      attempts = 0;
    };

    void requestPending();
    heartbeat = setInterval(() => {
      void requestPending();
    }, STREAM_REQUEST_HEARTBEAT_MS);

    return () => {
      stopped = true;
      clearRetry();
      if (heartbeat) {
        clearInterval(heartbeat);
        heartbeat = null;
      }
    };
  }, [streamRequestTargetsKey]);

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

  const recommendationMap = useMemo(() => {
    const map = new Map<string, Recommendation>();
    for (const recommendation of recommendationsQuery.data ?? []) {
      map.set(recommendation.slug, recommendation);
    }
    return map;
  }, [recommendationsQuery.data]);

  const selectedRecommendation = useMemo(() => {
    if (selectedSignal?.locked_recommendation) return selectedSignal.locked_recommendation;
    if (selectedSignal?.recommendation) return selectedSignal.recommendation;
    return recommendationMap.get(normalizedSelectedSlug ?? "") ?? null;
  }, [recommendationMap, selectedSignal?.locked_recommendation, selectedSignal?.recommendation, normalizedSelectedSlug]);

  const recommendationLockedAt = useMemo(() => {
    if (!selectedSignal?.recommendation_locked_at) return null;
    const ts = Date.parse(selectedSignal.recommendation_locked_at);
    if (Number.isNaN(ts)) return null;
    return new Date(ts);
  }, [selectedSignal?.recommendation_locked_at]);

  const staleSignal = useMemo(() => signalDelayed(selectedSignal, nowMs), [selectedSignal, nowMs]);

  const startSnapshotMissing = useMemo(() => {
    if (!selectedMarket || !selectedSignal) return false;
    const start = deriveStartMs(selectedMarket, marketAnchorMs);
    if (!start || nowMs < start) return false;
    return typeof selectedSignal.reference_start_price !== "number";
  }, [selectedMarket, selectedSignal, nowMs, marketAnchorMs]);

  const llmPacket = llmPacketQuery.data ?? null;

  const llmPacketCache = useMemo(() => {
    const map = new Map<string, LLMTradePacket>();
    const cached = queryClient.getQueriesData<LLMTradePacket>({
      queryKey: ["updown-llm-packet"],
    });

    for (const [queryKey, packet] of cached) {
      if (!packet) continue;
      if (!Array.isArray(queryKey)) continue;
      const keySlug = normalizeSelectedSlug(queryKey[1] as string | undefined);
      if (!keySlug) continue;
      map.set(keySlug, packet);
    }

    if (normalizedSelectedSlug && llmPacket) {
      map.set(normalizedSelectedSlug, llmPacket);
    }

    return map;
  }, [queryClient, normalizedSelectedSlug, llmPacket, llmPacketQuery.dataUpdatedAt]);

  const llmPacketCacheTtlSeconds = useMemo(() => {
    const ttl = llmHealthQuery.data?.cache_ttl_seconds;
    if (typeof ttl !== "number" || ttl <= 0) return 30;
    return ttl;
  }, [llmHealthQuery.data?.cache_ttl_seconds]);

  const llmPacketStale = useMemo(() => {
    if (!llmPacket?.generated_at) return false;
    const ts = toMillis(llmPacket.generated_at);
    if (!ts) return true;
    return nowMs - ts > llmPacketCacheTtlSeconds * 1000;
  }, [llmPacket?.generated_at, llmPacketCacheTtlSeconds, nowMs]);

  const llmGenerateCooldownRemainingSeconds = useMemo(() => {
    if (!llmPacket?.generated_at) return 0;
    const ts = toMillis(llmPacket.generated_at);
    if (!ts) return 0;
    const elapsedSeconds = Math.floor((nowMs - ts) / 1000);
    return Math.max(0, llmPacketCacheTtlSeconds - elapsedSeconds);
  }, [llmPacket?.generated_at, llmPacketCacheTtlSeconds, nowMs]);

  const llmGenerateLocked = llmGenerateCooldownRemainingSeconds > 0;
  const llmGenerateButtonLabel = useMemo(() => {
    if (llmGenerateMutation.isPending) return "Generating...";
    if (llmGenerateLocked) return `Generate (${llmGenerateCooldownRemainingSeconds}s)`;
    return "Generate";
  }, [
    llmGenerateCooldownRemainingSeconds,
    llmGenerateLocked,
    llmGenerateMutation.isPending,
  ]);

  const signalHasSynth = hasSynthProbabilities(selectedSignal);

  const selectedLiveMarket = selectedMarket ? augmentMarket(selectedMarket.market) : null;

  const livePMarketUp = useMemo(() => {
    if (!selectedMarket || !selectedLiveMarket) return selectedSignal?.p_market_up;
    return computeLiveMarketProbability(
      selectedMarket,
      selectedLiveMarket,
      selectedSignal?.p_market_up,
    );
  }, [selectedMarket, selectedLiveMarket, selectedSignal?.p_market_up]);

  const rawIntegrityFailure =
    staleSignal ||
    !!selectedSignal?.risk_flags?.data_integrity_failed ||
    !!selectedSignal?.risk_flags?.kill_switch ||
    !!selectedRecommendation?.prefill?.disabled ||
    !!prefillBlockReason;

  const [integrityFailure, setIntegrityFailure] = useState(false);

  const llmUnsupportedReason = useMemo(() => {
    if (!selectedMarket) return null;
    const assetAllowed =
      selectedMarket.asset.toUpperCase() === "BTC" ||
      selectedMarket.asset.toUpperCase() === "ETH";
    const windowAllowed =
      selectedMarket.window_type === "5m" || selectedMarket.window_type === "15m";
    if (!assetAllowed) return "AI Strategy v1 supports BTC and ETH only.";
    if (!windowAllowed) return "AI Strategy v1 supports 5m and 15m windows only.";
    return null;
  }, [selectedMarket]);

  const llmExecutionBlockedReason = useMemo(() => {
    if (!selectedMarket || !selectedSignal) return "No live market selected.";
    if (llmUnsupportedReason) return llmUnsupportedReason;
    if (!llmPacket) return LLM_PACKET_REQUIRED_WARNING;
    if (llmPacketStale) return "AI packet is delayed. Regenerate before execution.";
    if (!llmPacket.entry) return "AI setup is incomplete. Regenerate recommendation.";
    if (!llmPacket.entry.ready_to_bet) {
      const reasons = (llmPacket.entry.gate_reasons ?? []).join(", ");
      return reasons
        ? `AI entry gate blocked: ${reasons}`
        : "AI entry gate blocked for this window.";
    }
    if (llmPacket.effective_guard_blocks?.length) {
      return `Risk controls are blocking this setup: ${llmPacket.effective_guard_blocks.join(", ")}`;
    }
    if (llmPacket.decision === "NO_TRADE") {
      return "AI Strategy returned NO_TRADE for this window.";
    }
    return null;
  }, [selectedMarket, selectedSignal, llmUnsupportedReason, llmPacket, llmPacketStale]);

  const llmPrefill = useMemo((): TradeRecommendationPrefill | null => {
    if (!selectedMarket || !llmPacket) return null;
    const side = (llmPacket.recommended_side ?? "").toUpperCase();
    if (llmPacket.decision === "NO_TRADE" || side === "NONE") return null;
    if (side !== "UP" && side !== "DOWN") return null;
    const outcomeIndex =
      side === "UP" ? selectedMarket.outcome_index_up : selectedMarket.outcome_index_down;
    const limitPrice = llmPacket.suggested_limit_price > 0 ? llmPacket.suggested_limit_price : undefined;
    let sizeShares = llmPacket.suggested_size_shares > 0 ? llmPacket.suggested_size_shares : undefined;
    if (
      (!sizeShares || sizeShares <= 0) &&
      llmPacket.suggested_notional > 0 &&
      typeof limitPrice === "number" &&
      limitPrice > 0
    ) {
      sizeShares = llmPacket.suggested_notional / limitPrice;
    }
    return {
      side: "BUY",
      outcomeIndex,
      limitPrice,
      sizeShares,
      disabled: false,
    };
  }, [selectedMarket, llmPacket]);

  const executionPolicy = llmHealthQuery.data?.execution_policy ?? "det_allowed";

  const effectiveExecutionSource = useMemo<(typeof EXECUTION_SOURCES)[number]>(() => {
    if (executionPolicy === "llm_only") {
      return "llm";
    }
    if (executionSource === "deterministic" && !selectedRecommendation) {
      return "llm";
    }
    return executionSource;
  }, [executionPolicy, executionSource, selectedRecommendation]);

  const executionBlockedReason = useMemo(() => {
    if (executionPolicy === "llm_only" && effectiveExecutionSource === "deterministic") {
      return "Backend policy enforces AI Strategy-only execution.";
    }
    if (effectiveExecutionSource === "llm") {
      return llmExecutionBlockedReason;
    }
    return null;
  }, [executionPolicy, effectiveExecutionSource, llmExecutionBlockedReason]);

  const executionPreview = useMemo(() => {
    if (effectiveExecutionSource === "llm") {
      if (!llmPacket || !llmPrefill) return null;
      return {
        side: llmPacket.recommended_side,
        limit: llmPrefill.limitPrice ?? llmPacket.suggested_limit_price,
        size: llmPrefill.sizeShares ?? llmPacket.suggested_size_shares,
      };
    }
    if (!selectedRecommendation || selectedRecommendation.prefill.disabled) {
      return null;
    }
    return {
      side: selectedRecommendation.recommended_side,
      limit: selectedRecommendation.prefill.limit_price,
      size: selectedRecommendation.prefill.size_shares,
    };
  }, [effectiveExecutionSource, llmPacket, llmPrefill, selectedRecommendation]);

  useEffect(() => {
    if (!rawIntegrityFailure) {
      setIntegrityFailure(false);
      return;
    }
    const timer = setTimeout(() => setIntegrityFailure(true), 1200);
    return () => clearTimeout(timer);
  }, [rawIntegrityFailure]);

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

  const applyDeterministicRecommendation = (rec: Recommendation | null) => {
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

  const applyExecutionPrefill = () => {
    if (!selectedMarket) return;
    if (executionBlockedReason) return;

    if (effectiveExecutionSource === "llm") {
      if (!llmPacket || !llmPrefill) return;
      setPrefillBlockReason(null);
      setPrefill({
        side: "BUY",
        outcomeIndex: llmPrefill.outcomeIndex,
        limitPrice: llmPrefill.limitPrice,
        sizeShares: llmPrefill.sizeShares,
        disabled: false,
        applyToken: String(Date.now()),
      });
      void logUpDownDecision({
        slug: selectedMarket.slug,
        action: "manual_override",
        chosen_side: llmPacket.recommended_side,
        notes: `llm_prefill:${llmPacket.trace?.prompt_hash?.slice(0, 12) ?? "na"}`,
      }).catch(() => undefined);
      return;
    }

    applyDeterministicRecommendation(selectedRecommendation);
  };

  const rejectExecutionPrefill = () => {
    if (!selectedMarket) return;
    if (effectiveExecutionSource === "llm") {
      void logUpDownDecision({
        slug: selectedMarket.slug,
        action: "manual_override",
        chosen_side: llmPacket?.recommended_side,
        notes: "llm_prefill_rejected",
      }).catch(() => undefined);
      return;
    }
    if (!selectedRecommendation) return;
    void logUpDownDecision({
      slug: selectedRecommendation.slug,
      recommendation_id: selectedRecommendation.id,
      action: "rejected",
      chosen_side: selectedRecommendation.recommended_side,
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
        const next =
          idx < 0
            ? railActiveMarkets[0]
            : railActiveMarkets[(idx + 1 + railActiveMarkets.length) % railActiveMarkets.length];
        if (next) setSelectedSlug(next.slug);
      }

      if (event.key === "k" || event.key === "ArrowUp") {
        event.preventDefault();
        const idx = railActiveMarkets.findIndex((market) => market.slug === normalizedSelectedSlug);
        const prev =
          idx < 0
            ? railActiveMarkets[railActiveMarkets.length - 1]
            : railActiveMarkets[(idx - 1 + railActiveMarkets.length) % railActiveMarkets.length];
        if (prev) setSelectedSlug(prev.slug);
      }

      if (event.key.toLowerCase() === "p") {
        event.preventDefault();
        if (
          (effectiveExecutionSource === "llm"
            ? Boolean(llmPrefill)
            : Boolean(selectedRecommendation && !selectedRecommendation.prefill.disabled)) &&
          !executionBlockedReason
        ) {
          applyExecutionPrefill();
        }
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    normalizedSelectedSlug,
    railActiveMarkets,
    selectedRecommendation,
    executionBlockedReason,
    effectiveExecutionSource,
    llmPrefill,
  ]);

  return (
    <div className="mx-auto flex w-full max-w-[1680px] flex-col gap-4 px-4 pt-6 pb-2">
      <UpDownToolbar
        asset={asset}
        setAsset={setAsset}
        windowType={windowType}
        setWindowType={setWindowType}
        onRefresh={() => {
          void marketsQuery.refetch();
          void signalQuery.refetch();
          void recommendationsQuery.refetch();
          void llmPacketQuery.refetch();
          void llmHealthQuery.refetch();
          void performanceQuery.refetch();
        }}
      />

      <PerformanceStrip
        trades={performanceQuery.data?.trades ?? 0}
        winRate={pct(performanceQuery.data?.hit_rate)}
        brier={(performanceQuery.data?.brier_score ?? 0).toFixed(4)}
        actualReturn={money(performanceQuery.data?.realized_ev)}
      />

      <MarketList
        markets={decisionMarkets}
        selectedSlug={normalizedSelectedSlug}
        onSelect={(slug) => {
          setSelectedSlug((current) => (current === slug ? null : slug));
        }}
        nowMs={nowMs}
        anchorMs={marketAnchorMs}
        isLoading={marketsQuery.isLoading}
        isError={marketsQuery.isError}
        liveSignals={liveSignals}
        selectedSignal={selectedSignal}
        recommendationMap={recommendationMap}
        selectedRecommendation={selectedRecommendation}
        llmPacketCache={llmPacketCache}
        selectedLLMPacket={llmPacket}
        llmPacketCacheTtlSeconds={llmPacketCacheTtlSeconds}
        expandedContent={(slug) => {
          if (slug !== normalizedSelectedSlug) return null;
          return (
            <MarketRowExpanded
              selectedMarket={selectedMarket}
              selectedSignal={selectedSignal}
              recommendationLockedAt={recommendationLockedAt}
              startSnapshotMissing={startSnapshotMissing}
              signalHasSynth={signalHasSynth}
              livePMarketUp={livePMarketUp}
              llmPacket={llmPacket}
              llmPacketStale={llmPacketStale}
              nowMs={nowMs}
              llmUnsupportedReason={llmUnsupportedReason}
              llmGenerateLocked={llmGenerateLocked}
              llmGenerateButtonLabel={llmGenerateButtonLabel}
              llmGenerateCooldownRemainingSeconds={llmGenerateCooldownRemainingSeconds}
              llmGenerateMutation={llmGenerateMutation}
              normalizedSelectedSlug={normalizedSelectedSlug}
              executionSource={executionSource}
              setExecutionSource={setExecutionSource}
              effectiveExecutionSource={effectiveExecutionSource}
              executionPolicy={executionPolicy}
              executionBlockedReason={executionBlockedReason}
              executionPreview={executionPreview}
              applyExecutionPrefill={applyExecutionPrefill}
              rejectExecutionPrefill={rejectExecutionPrefill}
              canUseRecommendation={
                (effectiveExecutionSource === "llm"
                  ? !llmGenerateMutation.isPending && Boolean(llmPrefill)
                  : Boolean(selectedRecommendation && !selectedRecommendation.prefill.disabled)) &&
                !executionBlockedReason
              }
              canDismiss={
                effectiveExecutionSource === "llm"
                  ? Boolean(llmPacket)
                  : Boolean(selectedRecommendation)
              }
              selectedRecommendation={selectedRecommendation}
              prefillDriftBps={prefillDriftBps}
              prefillBlockReason={prefillBlockReason}
              integrityFailure={integrityFailure}
              selectedLiveMarket={selectedLiveMarket}
              prefill={prefill}
              onApplyQuickTicket={(nextPrefill) => {
                setPrefillBlockReason(null);
                setPrefill({
                  ...nextPrefill,
                  applyToken: String(Date.now()),
                });
              }}
            />
          );
        }}
      />
    </div>
  );
}

function UpDownToolbar({
  asset,
  setAsset,
  windowType,
  setWindowType,
  onRefresh,
}: {
  asset: (typeof ASSETS)[number];
  setAsset: (next: (typeof ASSETS)[number]) => void;
  windowType: (typeof WINDOWS)[number];
  setWindowType: (next: (typeof WINDOWS)[number]) => void;
  onRefresh: () => void;
}) {
  return (
    <div className="rounded-2xl border border-border/75 bg-gradient-to-b from-background to-background/70 p-4 shadow-[0_0_0_1px_hsl(var(--border)/0.15),0_20px_40px_hsl(var(--background)/0.5)]">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-start gap-3">
          <div className="mt-0.5 flex h-10 w-10 items-center justify-center rounded-xl border border-primary/30 bg-primary/10">
            <ArrowUpDown className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight text-foreground">Up/Down Pro</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Crypto prediction windows with strategy guidance
            </p>
          </div>
        </div>

        <Button
          size="sm"
          variant="outline"
          className="h-9 border-border/75 bg-background/40 px-3 font-mono text-[11px] uppercase tracking-wide"
          onClick={onRefresh}
        >
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Refresh
        </Button>
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-4">
        <div className="min-w-0">
          <FilterGroup>
            {ASSETS.map((option) => (
              <Button
                key={option}
                size="sm"
                variant={asset === option ? "default" : "ghost"}
                className={cn(
                  "h-8 px-3 font-mono text-[11px] uppercase tracking-wide",
                  asset === option
                    ? "bg-primary text-primary-foreground"
                    : "border border-border/65 bg-background/40 text-muted-foreground hover:text-foreground",
                )}
                onClick={() => setAsset(option)}
              >
                {option}
              </Button>
            ))}
          </FilterGroup>
        </div>

        <div className="ml-auto">
          <FilterGroup>
            {WINDOWS.map((option) => (
              <Button
                key={option}
                size="sm"
                variant={windowType === option ? "default" : "ghost"}
                className={cn(
                  "h-8 px-3 font-mono text-[11px] uppercase tracking-wide",
                  windowType === option
                    ? "bg-primary text-primary-foreground"
                    : "border border-border/65 bg-background/40 text-muted-foreground hover:text-foreground",
                )}
                onClick={() => setWindowType(option)}
              >
                {option.toUpperCase()}
              </Button>
            ))}
          </FilterGroup>
        </div>
      </div>
    </div>
  );
}

function FilterGroup({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-2">
      <div className="flex flex-wrap items-center gap-1.5">{children}</div>
    </div>
  );
}

function PerformanceStrip({
  trades,
  winRate,
  brier,
  actualReturn,
}: {
  trades: number;
  winRate: string;
  brier: string;
  actualReturn: string;
}) {
  return (
    <section className="rounded-xl border border-border/65 bg-background/60 px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="rounded-md border border-border/75 bg-background/50 px-2 py-1 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
          Session Performance
        </span>
        <div className="grid min-w-0 flex-1 grid-cols-2 gap-2 sm:grid-cols-4">
          <SummaryMetric label="Total Trades" value={String(trades)} />
          <SummaryMetric label="Win Rate" value={winRate} />
          <SummaryMetric label="Brier Score" value={brier} />
          <SummaryMetric label="Actual Return" value={actualReturn} />
        </div>
      </div>
    </section>
  );
}

function SummaryMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border/75 bg-background/50 px-2.5 py-1.5">
      <div className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-sm font-semibold text-foreground">{value}</div>
    </div>
  );
}

function MarketList({
  markets,
  selectedSlug,
  onSelect,
  nowMs,
  anchorMs,
  isLoading,
  isError,
  liveSignals,
  selectedSignal,
  recommendationMap,
  selectedRecommendation,
  llmPacketCache,
  selectedLLMPacket,
  llmPacketCacheTtlSeconds,
  expandedContent,
}: {
  markets: UpDownMarket[];
  selectedSlug: string | null;
  onSelect: (slug: string) => void;
  nowMs: number;
  anchorMs: number;
  isLoading: boolean;
  isError: boolean;
  liveSignals: Record<string, UpDownSignal>;
  selectedSignal: UpDownSignal | null;
  recommendationMap: Map<string, Recommendation>;
  selectedRecommendation: Recommendation | null;
  llmPacketCache: Map<string, LLMTradePacket>;
  selectedLLMPacket: LLMTradePacket | null;
  llmPacketCacheTtlSeconds: number;
  expandedContent: (slug: string) => ReactNode;
}) {
  const [sortKey, setSortKey] = useState<MarketSortKey | null>(null);
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");

  const toggleSort = (nextKey: MarketSortKey) => {
    if (sortKey === nextKey) {
      setSortDirection((current) => (current === "asc" ? "desc" : "asc"));
      return;
    }
    setSortKey(nextKey);
    setSortDirection("asc");
  };

  const rows = useMemo(() => {
    return markets.map((market, index) => {
      const isExpanded = selectedSlug === market.slug;
      const rowSignal = isExpanded ? selectedSignal : liveSignals[market.slug] ?? null;
      const rowRecommendation =
        (isExpanded
          ? selectedRecommendation
          : rowSignal?.locked_recommendation ??
            rowSignal?.recommendation ??
            recommendationMap.get(market.slug) ??
            null) ?? null;
      const rowPacket = (isExpanded ? selectedLLMPacket : llmPacketCache.get(market.slug)) ?? null;
      const rowPacketStale = (() => {
        if (!rowPacket?.generated_at) return false;
        const ts = toMillis(rowPacket.generated_at);
        if (!ts) return true;
        return nowMs - ts > llmPacketCacheTtlSeconds * 1000;
      })();

      const isLive = isMarketActiveAt(market, nowMs, anchorMs);
      const delayed = signalDelayed(rowSignal, nowMs);
      const riskNotice =
        !!rowSignal?.risk_flags?.data_integrity_failed ||
        !!rowSignal?.risk_flags?.kill_switch ||
        !!rowRecommendation?.prefill?.disabled;

      const statuses = uniqueStrings([
        isLive ? "Live" : "Upcoming",
        rowSignal?.recommendation_locked_at ? "Locked In" : null,
        delayed ? "Delayed" : null,
        riskNotice ? "Risk Notice" : null,
      ]);

      const summaryConfidence = rowRecommendation?.confidence ?? rowPacket?.confidence;
      const summaryEdge = rowRecommendation?.expected_value ?? rowPacket?.expected_value;

      return {
        index,
        market,
        isExpanded,
        rowSignal,
        rowRecommendation,
        rowPacket,
        rowPacketStale,
        statusLabel: statuses.join(" | "),
        countdownSeconds: marketCountdownSeconds(market, nowMs, anchorMs),
        strategyLabel: formatDecisionLabel(rowRecommendation?.decision),
        aiLabel: formatDecisionLabel(rowPacket?.decision ?? "NOT_GENERATED"),
        summaryConfidence,
        summaryEdge,
      };
    });
  }, [
    markets,
    selectedSlug,
    selectedSignal,
    liveSignals,
    selectedRecommendation,
    recommendationMap,
    selectedLLMPacket,
    llmPacketCache,
    nowMs,
    llmPacketCacheTtlSeconds,
    anchorMs,
  ]);

  const sortedRows = useMemo(() => {
    if (!sortKey) return rows;
    const direction = sortDirection === "asc" ? 1 : -1;
    return [...rows].sort((left, right) => {
      let comparison = 0;
      switch (sortKey) {
        case "market": {
          const leftLabel = left.market.market?.title || left.market.slug;
          const rightLabel = right.market.market?.title || right.market.slug;
          comparison = leftLabel.localeCompare(rightLabel, undefined, { sensitivity: "base" });
          break;
        }
        case "window": {
          const leftOrder = WINDOW_ORDER[left.market.window_type] ?? 99;
          const rightOrder = WINDOW_ORDER[right.market.window_type] ?? 99;
          comparison =
            leftOrder - rightOrder ||
            left.market.window_type.localeCompare(right.market.window_type, undefined, {
              sensitivity: "base",
            });
          break;
        }
        case "status":
          comparison = left.statusLabel.localeCompare(right.statusLabel, undefined, {
            sensitivity: "base",
          });
          break;
        case "countdown": {
          const leftCountdown = Number.isFinite(left.countdownSeconds)
            ? left.countdownSeconds
            : Number.POSITIVE_INFINITY;
          const rightCountdown = Number.isFinite(right.countdownSeconds)
            ? right.countdownSeconds
            : Number.POSITIVE_INFINITY;
          comparison = leftCountdown - rightCountdown;
          break;
        }
        case "strategy":
          comparison = left.strategyLabel.localeCompare(right.strategyLabel, undefined, {
            sensitivity: "base",
          });
          break;
        case "ai":
          comparison = left.aiLabel.localeCompare(right.aiLabel, undefined, {
            sensitivity: "base",
          });
          break;
        case "confidence":
          comparison = compareOptionalNumbers(left.summaryConfidence, right.summaryConfidence);
          break;
        case "edge":
          comparison = compareOptionalNumbers(left.summaryEdge, right.summaryEdge);
          break;
      }

      if (comparison === 0) {
        return left.index - right.index;
      }
      return comparison * direction;
    });
  }, [rows, sortDirection, sortKey]);

  return (
    <section className="overflow-hidden rounded-xl border border-border/75 bg-gradient-to-b from-background/80 to-background/55">
      <div className="border-b border-border/65 px-3 py-2">
        <div className="flex items-center gap-2">
          <span className="rounded-md border border-border/75 bg-background/60 px-2 py-1 font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
            Live Markets
          </span>
          <span className="font-mono text-[11px] uppercase tracking-wide text-muted-foreground">
            {markets.length} total
          </span>
        </div>
      </div>

      {isError ? (
        <div className="rounded-xl border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive">
          Failed to load up/down markets.
        </div>
      ) : null}

      {!isError && isLoading && !markets.length ? (
        <div className="rounded-xl border border-border/65 bg-background/40 p-4 text-sm text-muted-foreground">
          Loading markets...
        </div>
      ) : null}

      {!isError && !isLoading && !markets.length ? (
        <div className="rounded-xl border border-border/65 bg-background/40 p-4 text-sm text-muted-foreground">
          No up/down markets are available for this filter.
        </div>
      ) : null}

      {!isError && markets.length ? (
        <div className="relative max-h-[78vh] overflow-auto">
          <div
            className={cn(
              TABLE_ROW_COLUMNS,
              "sticky top-0 z-20 border-b border-border/75 bg-background/95 px-3 py-2 font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground backdrop-blur supports-[backdrop-filter]:bg-background/80",
            )}
          >
            <SortableHeader
              label="Market"
              sortKey="market"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <SortableHeader
              label="Window"
              sortKey="window"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <SortableHeader
              label="Status"
              sortKey="status"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <SortableHeader
              label="Countdown"
              sortKey="countdown"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <SortableHeader
              label="Strategy Engine"
              sortKey="strategy"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <SortableHeader
              label="AI Strategy"
              sortKey="ai"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <SortableHeader
              label="Confidence"
              sortKey="confidence"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <SortableHeader
              label="Expected Edge"
              sortKey="edge"
              activeKey={sortKey}
              direction={sortDirection}
              onToggle={toggleSort}
            />
            <span className="text-right">Action</span>
          </div>

          {sortedRows.map((row) => {
            return (
              <MarketRow
                key={row.market.slug}
                market={row.market}
                nowMs={nowMs}
                anchorMs={anchorMs}
                isExpanded={row.isExpanded}
                onSelect={onSelect}
                signal={row.rowSignal}
                recommendation={row.rowRecommendation}
                llmPacket={row.rowPacket}
                llmPacketStale={row.rowPacketStale}
                expandedContent={expandedContent(row.market.slug)}
              />
            );
          })}
        </div>
      ) : null}
    </section>
  );
}

function SortableHeader({
  label,
  sortKey,
  activeKey,
  direction,
  onToggle,
}: {
  label: string;
  sortKey: MarketSortKey;
  activeKey: MarketSortKey | null;
  direction: SortDirection;
  onToggle: (key: MarketSortKey) => void;
}) {
  const isActive = activeKey === sortKey;
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1 text-left transition-colors",
        isActive ? "text-foreground" : "text-muted-foreground hover:text-foreground",
      )}
      onClick={() => onToggle(sortKey)}
      aria-label={`Sort by ${label}`}
    >
      <span>{label}</span>
      {isActive ? (
        direction === "asc" ? (
          <ChevronUp className="h-3.5 w-3.5" />
        ) : (
          <ChevronDown className="h-3.5 w-3.5" />
        )
      ) : (
        <ArrowUpDown className="h-3.5 w-3.5 opacity-60" />
      )}
    </button>
  );
}

function AssetIcon({ asset }: { asset: string }) {
  const key = asset.toUpperCase();
  const src = ASSET_ICON_MAP[key];
  if (src) {
    return (
      <span className="inline-flex h-6 w-6 items-center justify-center overflow-hidden rounded-full border border-border/65 bg-background/70">
        <Image
          src={src}
          alt={`${key} icon`}
          width={24}
          height={24}
          className="h-full w-full object-cover"
        />
      </span>
    );
  }

  return (
    <span className="inline-flex h-6 w-6 items-center justify-center rounded-full border border-border/75 bg-background/70 font-mono text-[10px] uppercase text-muted-foreground">
      {key.slice(0, 1) || "?"}
    </span>
  );
}

function MarketRow({
  market,
  nowMs,
  anchorMs,
  isExpanded,
  onSelect,
  signal,
  recommendation,
  llmPacket,
  llmPacketStale,
  expandedContent,
}: {
  market: UpDownMarket;
  nowMs: number;
  anchorMs: number;
  isExpanded: boolean;
  onSelect: (slug: string) => void;
  signal: UpDownSignal | null;
  recommendation: Recommendation | null;
  llmPacket: LLMTradePacket | null;
  llmPacketStale: boolean;
  expandedContent: ReactNode;
}) {
  const isLive = isMarketActiveAt(market, nowMs, anchorMs);
  const delayed = signalDelayed(signal, nowMs);
  const riskNotice =
    !!signal?.risk_flags?.data_integrity_failed ||
    !!signal?.risk_flags?.kill_switch ||
    !!recommendation?.prefill?.disabled;

  const statuses = uniqueStrings([
    isLive ? "Live" : "Upcoming",
    signal?.recommendation_locked_at ? "Locked In" : null,
    delayed ? "Delayed" : null,
    riskNotice ? "Risk Notice" : null,
  ]);
  const summaryConfidence = recommendation?.confidence ?? llmPacket?.confidence;
  const summaryEdge = recommendation?.expected_value ?? llmPacket?.expected_value;
  const [shouldRenderExpanded, setShouldRenderExpanded] = useState(isExpanded);

  useEffect(() => {
    if (isExpanded) {
      setShouldRenderExpanded(true);
      return;
    }
    const timer = setTimeout(() => setShouldRenderExpanded(false), 260);
    return () => clearTimeout(timer);
  }, [isExpanded]);

  return (
    <div
      className={cn(
        "border-b border-border/65 transition-colors last:border-b-0",
        isExpanded
          ? "bg-primary/5"
          : isLive
            ? "bg-transparent hover:bg-background/45"
            : "bg-background/20 hover:bg-background/35",
      )}
    >
      <button
        type="button"
        className={cn(
          TABLE_ROW_COLUMNS,
          "w-full px-3 py-2.5 text-left transition",
          !isLive && !isExpanded && "opacity-90",
        )}
        onClick={() => onSelect(market.slug)}
      >
        <MarketRowSummary
          market={market}
          nowMs={nowMs}
          anchorMs={anchorMs}
          isLive={isLive}
          statuses={statuses}
          signal={signal}
          recommendation={recommendation}
          llmPacket={llmPacket}
          llmPacketStale={llmPacketStale}
          summaryConfidence={summaryConfidence}
          summaryEdge={summaryEdge}
          expanded={isExpanded}
        />
      </button>

      <div
        className={cn(
          "grid transition-[grid-template-rows] duration-300 ease-out",
          isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
        )}
      >
        <div
          className={cn(
            "overflow-hidden border-t border-border/65 transition-opacity duration-200",
            isExpanded ? "opacity-100" : "pointer-events-none opacity-0",
          )}
        >
          <div
            className={cn(
              "transition-transform duration-300 ease-out",
              isExpanded ? "translate-y-0" : "-translate-y-1",
            )}
          >
            {shouldRenderExpanded ? expandedContent : null}
          </div>
        </div>
      </div>
    </div>
  );
}

function MarketRowSummary({
  market,
  nowMs,
  anchorMs,
  isLive,
  statuses,
  signal,
  recommendation,
  llmPacket,
  llmPacketStale,
  summaryConfidence,
  summaryEdge,
  expanded,
}: {
  market: UpDownMarket;
  nowMs: number;
  anchorMs: number;
  isLive: boolean;
  statuses: string[];
  signal: UpDownSignal | null;
  recommendation: Recommendation | null;
  llmPacket: LLMTradePacket | null;
  llmPacketStale: boolean;
  summaryConfidence?: number;
  summaryEdge?: number;
  expanded: boolean;
}) {
  const countdown = marketCountdown(market, nowMs, anchorMs);

  return (
    <>
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <AssetIcon asset={market.asset} />
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold text-foreground">
              {market.market?.title || market.slug}
            </div>
            <div className="mt-0.5 flex items-center gap-1.5 text-[10px] text-muted-foreground">
              <span className="font-mono uppercase tracking-wide">{market.asset}</span>
              <span>•</span>
              <span className="truncate">{market.slug}</span>
            </div>
          </div>
        </div>
      </div>

      <div>
        <div className="font-mono text-xs font-semibold uppercase tracking-wide text-foreground">
          {market.window_type.toUpperCase()}
        </div>
        <div className="mt-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
          {(market.resolution_source_type || "unknown").toUpperCase()}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-1">
        {statuses.map((status) => {
          const tone: StatusTone =
            status === "Live"
              ? "live"
              : status === "Upcoming"
                ? "upcoming"
                : status === "Locked In"
                  ? "locked"
                  : status === "Delayed" || status === "Risk Notice"
                    ? "warning"
                    : "muted";
          return (
            <StatusBadge key={`${market.slug}-${status}`} tone={tone}>
              {status}
            </StatusBadge>
          );
        })}
      </div>

      <div
        className={cn(
          "font-mono text-[12px] font-semibold uppercase tracking-wide",
          isLive ? "text-foreground" : "text-muted-foreground",
        )}
      >
        {countdown}
      </div>

      <div>
        <div className="font-mono text-[12px] font-semibold text-foreground">
          {formatDecisionLabel(recommendation?.decision)}
        </div>
      </div>

      <div>
        <div
          className={cn(
            "font-mono text-[12px] font-semibold",
            llmPacketStale ? "text-amber-300" : "text-foreground",
          )}
        >
          {formatDecisionLabel(llmPacket?.decision ?? "NOT_GENERATED")}
        </div>
      </div>

      <div className="font-mono text-[12px] font-semibold text-foreground">
        {pct(summaryConfidence)}
      </div>

      <div className="font-mono text-[12px] font-semibold text-foreground">
        {money(summaryEdge)}
      </div>

      <div className="flex items-center justify-end">
        <span className="inline-flex items-center gap-1 rounded border border-border/75 bg-background/40 px-2 py-1 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
          {expanded ? "Hide" : "View"}
          {expanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
        </span>
      </div>
    </>
  );
}

function MarketRowExpanded({
  nowMs,
  selectedMarket,
  selectedSignal,
  recommendationLockedAt,
  startSnapshotMissing,
  signalHasSynth,
  livePMarketUp,
  llmPacket,
  llmPacketStale,
  llmUnsupportedReason,
  llmGenerateLocked,
  llmGenerateButtonLabel,
  llmGenerateCooldownRemainingSeconds,
  llmGenerateMutation,
  normalizedSelectedSlug,
  executionSource,
  setExecutionSource,
  effectiveExecutionSource,
  executionPolicy,
  executionBlockedReason,
  executionPreview,
  applyExecutionPrefill,
  rejectExecutionPrefill,
  canUseRecommendation,
  canDismiss,
  selectedRecommendation,
  prefillDriftBps,
  prefillBlockReason,
  integrityFailure,
  selectedLiveMarket,
  prefill,
  onApplyQuickTicket,
}: {
  nowMs: number;
  selectedMarket: UpDownMarket | null;
  selectedSignal: UpDownSignal | null;
  recommendationLockedAt: Date | null;
  startSnapshotMissing: boolean;
  signalHasSynth: boolean;
  livePMarketUp?: number;
  llmPacket: LLMTradePacket | null;
  llmPacketStale: boolean;
  llmUnsupportedReason: string | null;
  llmGenerateLocked: boolean;
  llmGenerateButtonLabel: string;
  llmGenerateCooldownRemainingSeconds: number;
  llmGenerateMutation: ReturnType<typeof useMutation<
    LLMTradePacket,
    unknown,
    { slug: string; force_refresh?: boolean }
  >>;
  normalizedSelectedSlug: string | null;
  executionSource: "llm" | "deterministic";
  setExecutionSource: (source: "llm" | "deterministic") => void;
  effectiveExecutionSource: "llm" | "deterministic";
  executionPolicy: string;
  executionBlockedReason: string | null;
  executionPreview: {
    side?: string;
    limit?: number;
    size?: number;
  } | null;
  applyExecutionPrefill: () => void;
  rejectExecutionPrefill: () => void;
  canUseRecommendation: boolean;
  canDismiss: boolean;
  selectedRecommendation: Recommendation | null;
  prefillDriftBps: number;
  prefillBlockReason: string | null;
  integrityFailure: boolean;
  selectedLiveMarket: UpDownMarket["market"] | null;
  prefill: TradeRecommendationPrefill | null;
  onApplyQuickTicket: (prefill: TradeRecommendationPrefill) => void;
}) {
  const warnings = uniqueStrings([
    executionBlockedReason,
    effectiveExecutionSource === "deterministic"
      ? selectedRecommendation?.prefill?.disabled_why
      : null,
    prefillBlockReason,
    llmPacketStale ? "AI packet is delayed." : null,
    prefillDriftBps > 0 ? `Drift ${prefillDriftBps.toFixed(0)} bps` : null,
    executionSource !== effectiveExecutionSource
      ? executionPolicy === "llm_only"
        ? "Policy restriction: AI Strategy only."
        : "Strategy Engine unavailable, using AI Strategy."
      : null,
  ]);

  const riskWarnings = uniqueStrings([
    selectedSignal?.risk_flags?.market_stale ? "Market data is delayed." : null,
    selectedSignal?.risk_flags?.synth_stale ? "Synthetic inputs are delayed." : null,
    selectedSignal?.risk_flags?.source_mismatch ? "Integrity warning: source mismatch detected." : null,
    selectedSignal?.risk_flags?.clock_drift ? "Integrity warning: clock drift detected." : null,
    selectedSignal?.risk_flags?.data_integrity_failed
      ? "Market data failed validation."
      : null,
    selectedSignal?.risk_flags?.kill_switch ? "Safety protection is active." : null,
    startSnapshotMissing ? "Opening price snapshot missing after window start." : null,
    signalDelayed(selectedSignal, nowMs) ? "Signal feed is delayed." : null,
  ]);
  const strategyReasonCodes = selectedRecommendation?.reason_codes ?? selectedSignal?.reason_codes ?? [];
  const aiGateReasons = llmPacket?.entry?.gate_reasons ?? [];
  const aiFreshness = llmPacket?.freshness;
  const [showAdvancedTradePanel, setShowAdvancedTradePanel] = useState(false);

  return (
    <div className="bg-background/55 px-3 pb-3 pt-3">
      {!selectedMarket ? (
        <div className="border border-border/75 bg-background/50 p-3 text-sm text-muted-foreground">
          Select a live market to view trade setup.
        </div>
      ) : !selectedSignal ? (
        <div className="border border-border/75 bg-background/50 p-3 text-sm text-muted-foreground">
          No live market selected.
        </div>
      ) : (
        <div className="space-y-3">
          <div className="grid gap-3 xl:grid-cols-2">
            <section className="border border-border/75 bg-background/50 p-3">
              <SectionTitle title="Market Snapshot" />
              <div className="mt-2 grid grid-cols-2 gap-2 lg:grid-cols-4">
                <MetricPill label="Opening Price" value={price(selectedSignal.reference_start_price)} />
                <MetricPill label="Live Price" value={price(selectedSignal.reference_current_price)} accent />
                <MetricPill label="Starts At" value={formatClock(selectedMarket.event_start_time)} />
                <MetricPill label="Ends At" value={formatClock(selectedMarket.event_end_time)} />
              </div>

              {riskWarnings.length ? (
                <div className="mt-2 space-y-1.5 rounded-md border border-amber-500/35 bg-amber-500/10 p-2.5 text-[12px] text-amber-200">
                  {riskWarnings.map((warning) => (
                    <div key={warning} className="flex items-start gap-1.5">
                      <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                      <span>{warning}</span>
                    </div>
                  ))}
                </div>
              ) : null}
            </section>

            <section className="border border-border/75 bg-background/50 p-3">
              <SectionTitle title="Strategy Engine Details" />
              {/* <p className="mt-1 text-[11px] text-muted-foreground">
                Market pricing context and strategy-adjusted probabilities.
              </p> */}
              <div className="mt-2 grid grid-cols-2 gap-2 lg:grid-cols-4">
                <MetricPill
                  label="Market Implied Probability"
                  value={pct(livePMarketUp)}
                  reserveLabelLines={2}
                />
                <MetricPill
                  label="Final Strategy Probability"
                  value={signalHasSynth ? pct(selectedSignal.p_final_up) : "--"}
                  accent={signalHasSynth}
                  reserveLabelLines={2}
                />
                <MetricPill
                  label="Locked In At"
                  value={recommendationLockedAt ? recommendationLockedAt.toLocaleTimeString() : "--"}
                  reserveLabelLines={3}
                />
                <MetricPill
                  label="Signal Timestamp"
                  value={formatClock(selectedSignal.timestamp)}
                  reserveLabelLines={3}
                />
              </div>
              {strategyReasonCodes.length ? (
                <div className="mt-2 flex flex-wrap gap-1">
                  {strategyReasonCodes.slice(0, 6).map((code) => (
                    <span
                      key={code}
                      className="rounded border border-border/75 bg-background/60 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide text-muted-foreground"
                    >
                      {code}
                    </span>
                  ))}
                </div>
              ) : null}
            </section>
          </div>

          <div className="grid gap-3 xl:grid-cols-2">
            <section className="flex h-full flex-col border border-border/75 bg-background/50 p-3">
              <SectionTitle title="AI Strategy Details" />
              <div className="mt-2">
                <div className="mb-2 flex flex-wrap items-center gap-2">
                  <Button
                    size="sm"
                    className="h-7 px-3 font-mono text-[10px] uppercase tracking-wide"
                    disabled={
                      !normalizedSelectedSlug ||
                      llmGenerateMutation.isPending ||
                      llmGenerateLocked ||
                      Boolean(llmUnsupportedReason)
                    }
                    onClick={() => {
                      if (!normalizedSelectedSlug) return;
                      llmGenerateMutation.mutate({
                        slug: normalizedSelectedSlug,
                        force_refresh: false,
                      });
                    }}
                  >
                    <BrainCircuit className="mr-1 h-3.5 w-3.5" />
                    {llmGenerateButtonLabel}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-7 px-3 font-mono text-[10px] uppercase tracking-wide"
                    disabled={
                      !normalizedSelectedSlug ||
                      llmGenerateMutation.isPending ||
                      Boolean(llmUnsupportedReason)
                    }
                    onClick={() => {
                      if (!normalizedSelectedSlug) return;
                      llmGenerateMutation.mutate({
                        slug: normalizedSelectedSlug,
                        force_refresh: true,
                      });
                    }}
                  >
                    Regenerate
                  </Button>
                </div>
                {llmGenerateLocked && !llmGenerateMutation.isPending ? (
                  <p className="text-[11px] text-muted-foreground">
                    Generate available in {llmGenerateCooldownRemainingSeconds}s.
                  </p>
                ) : null}
                {llmGenerateMutation.isError ? (
                  <p className="text-[11px] text-destructive">
                    {llmGenerateMutation.error instanceof Error
                      ? llmGenerateMutation.error.message
                      : "Failed to generate AI packet."}
                  </p>
                ) : null}
                {llmUnsupportedReason ? (
                  <p className="text-[11px] text-amber-300">{llmUnsupportedReason}</p>
                ) : null}
              </div>
              {(aiFreshness || aiGateReasons.length) ? (
                <div className="mt-2 grid gap-2 text-[11px] text-muted-foreground lg:grid-cols-2">
                  <div className="space-y-1">
                    <p>
                      Freshness: synth {aiFreshness?.synth_age_seconds ?? "--"}s, allora{" "}
                      {aiFreshness?.allora_age_seconds ?? "--"}s, market{" "}
                      {aiFreshness?.market_age_seconds ?? "--"}s
                    </p>
                    {llmPacket?.trace?.prompt_hash ? (
                      <p className="font-mono">Prompt {llmPacket.trace.prompt_hash.slice(0, 12)}</p>
                    ) : null}
                  </div>
                  {aiGateReasons.length ? (
                    <div className="space-y-1">
                      <p className="font-mono uppercase tracking-wide text-muted-foreground">Gate Reasons</p>
                      {aiGateReasons.slice(0, 4).map((reason) => (
                        <p key={reason}>{reason}</p>
                      ))}
                    </div>
                  ) : null}
                </div>
              ) : null}
              <div className="mt-auto grid grid-cols-2 gap-2 pt-2 lg:grid-cols-4">
                <MetricPill
                  label="Risk-Adjusted Score"
                  value={
                    typeof llmPacket?.entry?.sharpe_chosen_side === "number"
                      ? llmPacket.entry.sharpe_chosen_side.toFixed(3)
                      : "--"
                  }
                />
                <MetricPill
                  label="Entry Score"
                  value={
                    typeof llmPacket?.entry?.entry_score === "number"
                      ? llmPacket.entry.entry_score.toFixed(3)
                      : "--"
                  }
                />
                <MetricPill
                  label="Ready To Bet"
                  value={
                    typeof llmPacket?.entry?.ready_to_bet === "boolean"
                      ? llmPacket.entry.ready_to_bet
                        ? "YES"
                        : "NO"
                      : "--"
                  }
                />
                <MetricPill
                  label="Packet Time"
                  value={formatClock(llmPacket?.generated_at)}
                />
              </div>
            </section>

            <TradeSetupSection
              integrityFailure={integrityFailure}
              executionSource={executionSource}
              setExecutionSource={setExecutionSource}
              effectiveExecutionSource={effectiveExecutionSource}
              executionPolicy={executionPolicy}
              canUseDeterministic={Boolean(selectedRecommendation)}
              onUseRecommendation={applyExecutionPrefill}
              onDismiss={rejectExecutionPrefill}
              canUseRecommendation={canUseRecommendation}
              canDismiss={canDismiss}
              warnings={warnings}
              executionPreview={executionPreview}
            />
          </div>

          <section className="border border-border/75 bg-background/50 p-3">
            <SectionTitle title="Trade Panel" subtitle="Execution" />
            <div className="mt-2 space-y-2">
              {!selectedLiveMarket ? (
                <p className="text-sm text-muted-foreground">Select a live market to view trade setup.</p>
              ) : (
                <>
                  <CompactExecutionTicket
                    market={selectedMarket}
                    signal={selectedSignal}
                    prefill={prefill}
                    executionPreview={executionPreview}
                    blockedReason={prefillBlockReason}
                    onApplyQuickTicket={onApplyQuickTicket}
                  />

                  <div className="rounded-md border border-border/75 bg-background/45">
                    <button
                      type="button"
                      className="flex w-full items-center justify-between px-3 py-2 text-left"
                      onClick={() => setShowAdvancedTradePanel((prev) => !prev)}
                    >
                      <span className="font-mono text-[11px] uppercase tracking-[0.12em] text-muted-foreground">
                        Advanced settings
                      </span>
                      <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
                        <Info className="h-3.5 w-3.5" />
                        {showAdvancedTradePanel ? "Hide" : "More options"}
                      </span>
                    </button>

                    {showAdvancedTradePanel ? (
                      <div className="border-t border-border/75 p-2">
                        <TradeForm
                          market={selectedLiveMarket}
                          mode="compact"
                          recommendationPrefill={prefill}
                          externalBlockReason={prefillBlockReason}
                        />
                      </div>
                    ) : null}
                  </div>
                </>
              )}
            </div>
          </section>
        </div>
      )}
    </div>
  );
}

function SectionTitle({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <span className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
        {title}
      </span>
      {subtitle ? (
        <span className="text-xs text-muted-foreground">{subtitle}</span>
      ) : null}
    </div>
  );
}

function TradeSetupSection({
  integrityFailure,
  executionSource,
  setExecutionSource,
  effectiveExecutionSource,
  executionPolicy,
  canUseDeterministic,
  onUseRecommendation,
  onDismiss,
  canUseRecommendation,
  canDismiss,
  warnings,
  executionPreview,
}: {
  integrityFailure: boolean;
  executionSource: "llm" | "deterministic";
  setExecutionSource: (next: "llm" | "deterministic") => void;
  effectiveExecutionSource: "llm" | "deterministic";
  executionPolicy: string;
  canUseDeterministic: boolean;
  onUseRecommendation: () => void;
  onDismiss: () => void;
  canUseRecommendation: boolean;
  canDismiss: boolean;
  warnings: string[];
  executionPreview: {
    side?: string;
    limit?: number;
    size?: number;
  } | null;
}) {
  return (
    <section className="rounded-lg border border-border/75 bg-background/55 p-3">
      <div className="mb-2 flex items-start justify-between gap-2">
        <SectionTitle title="Trade Setup" />
        {integrityFailure ? (
          <StatusBadge tone="warning">Risk Notice</StatusBadge>
        ) : null}
      </div>

      <div className="rounded-md border border-border/65 bg-background/45 p-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
            Recommendation Source
          </span>
          <div className="inline-flex rounded-md border border-border/75 bg-background/40 p-0.5">
            <Button
              size="sm"
              variant={executionSource === "llm" ? "default" : "ghost"}
              className="h-6 px-2 font-mono text-[10px] uppercase tracking-wide"
              onClick={() => setExecutionSource("llm")}
            >
              AI Strategy
            </Button>
            <Button
              size="sm"
              variant={executionSource === "deterministic" ? "default" : "ghost"}
              className="h-6 px-2 font-mono text-[10px] uppercase tracking-wide"
              onClick={() => setExecutionSource("deterministic")}
              disabled={executionPolicy === "llm_only" || !canUseDeterministic}
            >
              Strategy Engine
            </Button>
          </div>
          {executionSource !== effectiveExecutionSource ? (
            <span className="text-[11px] text-amber-300">
              {executionPolicy === "llm_only"
                ? "Backend policy set to AI Strategy-only."
                : "Strategy Engine unavailable, using AI Strategy."}
            </span>
          ) : null}
        </div>

        <div className="mt-2 flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            className="h-7 px-3 font-mono text-[10px] uppercase tracking-wide"
            onClick={onUseRecommendation}
            disabled={!canUseRecommendation}
          >
            Use Recommendation
            <ArrowRight className="ml-1 h-3 w-3" />
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-3 font-mono text-[10px] uppercase tracking-wide"
            onClick={onDismiss}
            disabled={!canDismiss}
          >
            Dismiss
          </Button>
        </div>

        {warnings.length ? (
          <div className="mt-2 space-y-1 rounded-md border border-amber-500/35 bg-amber-500/10 p-2 text-[11px] text-amber-200">
            {warnings.map((warning) => (
              <div key={warning}>{warning}</div>
            ))}
          </div>
        ) : null}

        {executionPreview ? (
          <div className="mt-2 grid grid-cols-3 gap-2">
            <MetricPill label="Direction" value={executionPreview.side ?? "--"} compact />
            <MetricPill
              label="Limit Price"
              value={
                typeof executionPreview.limit === "number"
                  ? executionPreview.limit.toFixed(3)
                  : "--"
              }
              compact
            />
            <MetricPill
              label="Position Size"
              value={
                typeof executionPreview.size === "number"
                  ? executionPreview.size.toFixed(2)
                  : "--"
              }
              compact
            />
          </div>
        ) : null}
      </div>
    </section>
  );
}

function CompactExecutionTicket({
  market,
  signal,
  prefill,
  executionPreview,
  blockedReason,
  onApplyQuickTicket,
}: {
  market: UpDownMarket;
  signal: UpDownSignal;
  prefill: TradeRecommendationPrefill | null;
  executionPreview: {
    side?: string;
    limit?: number;
    size?: number;
  } | null;
  blockedReason: string | null;
  onApplyQuickTicket: (prefill: TradeRecommendationPrefill) => void;
}) {
  const resolveDirection = (): "UP" | "DOWN" => {
    if (prefill?.outcomeIndex === market.outcome_index_down) return "DOWN";
    if (prefill?.outcomeIndex === market.outcome_index_up) return "UP";
    if (executionPreview?.side === "DOWN") return "DOWN";
    return "UP";
  };

  const referenceLimit = (direction: "UP" | "DOWN") => {
    const quote = direction === "UP" ? signal.executable_ask_up : signal.executable_ask_down;
    if (typeof quote === "number" && quote > 0) return quote;
    const fallbackQuote = direction === "UP" ? signal.executable_bid_up : signal.executable_bid_down;
    if (typeof fallbackQuote === "number" && fallbackQuote > 0) return fallbackQuote;
    return 0;
  };

  const resolveLimit = (direction: "UP" | "DOWN") => {
    if (typeof prefill?.limitPrice === "number" && prefill.limitPrice > 0) return prefill.limitPrice;
    if (typeof executionPreview?.limit === "number" && executionPreview.limit > 0) {
      return executionPreview.limit;
    }
    return referenceLimit(direction);
  };

  const resolveSize = () => {
    if (typeof prefill?.sizeShares === "number" && prefill.sizeShares > 0) return prefill.sizeShares;
    if (typeof executionPreview?.size === "number" && executionPreview.size > 0) {
      return executionPreview.size;
    }
    return 25;
  };

  const [direction, setDirection] = useState<"UP" | "DOWN">(resolveDirection);
  const [limitInput, setLimitInput] = useState("");
  const [sizeInput, setSizeInput] = useState("");

  useEffect(() => {
    const nextDirection = resolveDirection();
    setDirection(nextDirection);
    const nextLimit = resolveLimit(nextDirection);
    setLimitInput(nextLimit > 0 ? nextLimit.toFixed(3) : "");
    const nextSize = resolveSize();
    setSizeInput(nextSize > 0 ? nextSize.toFixed(2) : "");
  }, [
    market.condition_id,
    prefill?.applyToken,
    prefill?.limitPrice,
    prefill?.outcomeIndex,
    prefill?.sizeShares,
    executionPreview?.limit,
    executionPreview?.side,
    executionPreview?.size,
    signal.executable_ask_down,
    signal.executable_ask_up,
    signal.executable_bid_down,
    signal.executable_bid_up,
  ]);

  const numericLimit = Number(limitInput);
  const numericSize = Number(sizeInput);
  const hasLimit = Number.isFinite(numericLimit) && numericLimit > 0;
  const hasSize = Number.isFinite(numericSize) && numericSize > 0;
  const orderValue = hasLimit && hasSize ? numericLimit * numericSize : 0;
  const canApply = hasLimit && hasSize && !blockedReason;

  const onDirectionChange = (nextDirection: "UP" | "DOWN") => {
    setDirection(nextDirection);
    const nextLimit = referenceLimit(nextDirection);
    if (nextLimit > 0) {
      setLimitInput(nextLimit.toFixed(3));
    }
  };

  return (
    <div className="rounded-md border border-border/75 bg-background/45 p-2.5">
      <div className="grid gap-2 xl:grid-cols-[220px_190px_190px_1fr_170px]">
        <div>
          <p className="mb-1 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
            Direction
          </p>
          <div className="inline-flex w-full rounded-md border border-border/75 bg-background/50 p-0.5">
            <button
              type="button"
              className={cn(
                "flex-1 rounded px-2.5 py-1.5 font-mono text-[11px] uppercase tracking-wide transition",
                direction === "UP"
                  ? "bg-constructive/80 text-black"
                  : "text-muted-foreground hover:text-foreground",
              )}
              onClick={() => onDirectionChange("UP")}
            >
              Up
            </button>
            <button
              type="button"
              className={cn(
                "flex-1 rounded px-2.5 py-1.5 font-mono text-[11px] uppercase tracking-wide transition",
                direction === "DOWN"
                  ? "bg-destructive/80 text-white"
                  : "text-muted-foreground hover:text-foreground",
              )}
              onClick={() => onDirectionChange("DOWN")}
            >
              Down
            </button>
          </div>
        </div>

        <div>
          <p className="mb-1 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
            Limit Price
          </p>
          <div className="relative">
            <Input
              type="number"
              min="0.001"
              max="0.999"
              step="0.001"
              value={limitInput}
              onChange={(event) => setLimitInput(event.target.value)}
              className="h-8 border-border/75 bg-background/55 pr-14 font-mono text-right text-sm"
              placeholder="0.000"
            />
            <span className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
              USDC
            </span>
          </div>
        </div>

        <div>
          <p className="mb-1 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
            Position Size
          </p>
          <div className="relative">
            <Input
              type="number"
              min="1"
              step="0.01"
              value={sizeInput}
              onChange={(event) => setSizeInput(event.target.value)}
              className="h-8 border-border/75 bg-background/55 pr-16 font-mono text-right text-sm"
              placeholder="0.00"
            />
            <span className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
              Shares
            </span>
          </div>
        </div>

        <div>
          <p className="mb-1 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
            Order Value
          </p>
          <div className="flex h-8 items-center rounded border border-border/75 bg-background/55 px-2.5">
            <p className="text-sm font-semibold text-foreground">
              {hasLimit && hasSize
                ? `$${orderValue.toLocaleString(undefined, {
                    minimumFractionDigits: 2,
                    maximumFractionDigits: 2,
                  })}`
                : "--"}
            </p>
          </div>
        </div>

        <Button
          type="button"
          className={cn(
            "h-8 self-end font-mono text-[11px] uppercase tracking-wide",
            direction === "UP"
              ? "bg-constructive text-black hover:bg-constructive/85"
              : "bg-destructive text-white hover:bg-destructive/85",
          )}
          disabled={!canApply}
          onClick={() => {
            onApplyQuickTicket({
              side: "BUY",
              outcomeIndex: direction === "UP" ? market.outcome_index_up : market.outcome_index_down,
              limitPrice: numericLimit,
              sizeShares: numericSize,
              disabled: false,
            });
          }}
        >
          Place Trade
        </Button>
      </div>

      {blockedReason ? (
        <p className="mt-2 text-[11px] text-amber-300">{blockedReason}</p>
      ) : null}
    </div>
  );
}

function MetricPill({
  label,
  value,
  accent,
  compact,
  priority,
  subdued,
  reserveLabelLines,
}: {
  label: string;
  value: string;
  accent?: boolean;
  compact?: boolean;
  priority?: boolean;
  subdued?: boolean;
  reserveLabelLines?: 1 | 2 | 3;
}) {
  return (
    <div
      className={cn(
        "rounded-md border border-border/70 bg-background/45 p-2",
        compact && "px-2 py-1.5",
        priority && "border-primary/30 bg-background/60",
        subdued && "opacity-85",
        accent && "border-primary/50",
      )}
    >
      <div
        className={cn(
          "font-mono text-[10px] uppercase tracking-wide text-muted-foreground",
          reserveLabelLines === 2 && "min-h-[2.3em]",
          reserveLabelLines === 3 && "min-h-[3.4em]",
        )}
      >
        {label}
      </div>
      <div
        className={cn(
          "mt-1 text-sm font-semibold text-foreground",
          priority && "text-base",
          accent && "text-primary",
        )}
      >
        {value}
      </div>
    </div>
  );
}

function StatusBadge({
  children,
  tone,
}: {
  children: ReactNode;
  tone: StatusTone;
}) {
  return (
    <span
      className={cn(
        "rounded-md border px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide",
        tone === "live" && "border-constructive/40 bg-constructive/15 text-constructive",
        tone === "upcoming" && "border-border/75 bg-muted/30 text-muted-foreground",
        tone === "warning" && "border-amber-500/35 bg-amber-500/10 text-amber-300",
        tone === "locked" && "border-primary/40 bg-primary/12 text-primary",
        tone === "muted" && "border-border/70 bg-background/50 text-muted-foreground",
      )}
    >
      {children}
    </span>
  );
}
