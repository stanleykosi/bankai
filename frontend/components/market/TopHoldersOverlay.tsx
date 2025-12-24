"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ArrowUpRight,
  Loader2,
  TrendingDown,
  TrendingUp,
  Users,
} from "lucide-react";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { fetchMarketHolders } from "@/lib/market-data";
import { cn } from "@/lib/utils";
import type { Holder } from "@/types";

type Outcome = "YES" | "NO";

type LiveTrade = {
  id: string;
  assetId: string;
  outcome: Outcome | "UNKNOWN";
  side: "BUY" | "SELL";
  price: number;
  size: number;
  value: number;
  timestamp: number;
};

type ConnectionState = "idle" | "connecting" | "live" | "closed";

type TradeFilter = "ALL" | "YES" | "NO";

type TopHoldersOverlayProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  marketTitle?: string;
  conditionId: string;
  tokenYesId?: string;
  tokenNoId?: string;
};

const WSS_MARKET_URL = "wss://ws-subscriptions-clob.polymarket.com/ws/market";

const formatCurrency = (value: number): string => {
  if (value >= 1_000_000) {
    return `$${(value / 1_000_000).toFixed(2)}M`;
  }
  if (value >= 1_000) {
    return `$${(value / 1_000).toFixed(1)}K`;
  }
  return `$${value.toFixed(2)}`;
};

const formatSize = (value: number): string => {
  if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`;
  }
  if (value >= 1_000) {
    return `${(value / 1_000).toFixed(1)}K`;
  }
  return value.toFixed(2);
};

const formatPercent = (value: number): string => `${value.toFixed(2)}%`;

const formatPrice = (price: number): string => `${(price * 100).toFixed(1)}¢`;

const formatTime = (timestamp: number): string => {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return "--:--";
  return date.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
};

const truncateAddress = (address: string): string => {
  if (!address || address.length <= 12) return address;
  return `${address.slice(0, 6)}...${address.slice(-4)}`;
};

function HolderRow({ holder, rank }: { holder: Holder; rank: number }) {
  const displayName = holder.profileName || truncateAddress(holder.address);

  return (
    <tr className="border-b border-border/30 last:border-b-0 hover:bg-muted/20 transition-colors">
      <td className="py-2.5 text-center font-mono text-muted-foreground">
        {rank}
      </td>
      <td className="py-2.5">
        <Link
          href={`/profile/${holder.address}`}
          className="flex items-center gap-2 group"
        >
          {holder.profileImage ? (
            <img
              src={holder.profileImage}
              alt={displayName}
              className="h-7 w-7 rounded-full"
              loading="lazy"
            />
          ) : (
            <div className="h-7 w-7 rounded-full bg-primary/10 flex items-center justify-center">
              <span className="text-[10px] font-bold text-primary">
                {displayName.charAt(0).toUpperCase()}
              </span>
            </div>
          )}
          <span className="text-sm font-medium text-foreground group-hover:text-primary transition-colors line-clamp-1">
            {displayName}
          </span>
          <ArrowUpRight className="h-3 w-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
        </Link>
      </td>
      <td className="py-2.5 text-right font-mono">{formatSize(holder.size)}</td>
      <td className="py-2.5 text-right font-mono">{formatCurrency(holder.value)}</td>
      <td className="py-2.5 text-right font-mono text-muted-foreground">
        {formatPercent(holder.percentage)}
      </td>
    </tr>
  );
}

function HoldersTable({
  holders,
  isLoading,
}: {
  holders: Holder[];
  isLoading: boolean;
}) {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-10">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!holders || holders.length === 0) {
    return (
      <div className="py-10 text-center text-sm text-muted-foreground">
        No holders found for this outcome.
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border/50 text-xs uppercase tracking-wide text-muted-foreground">
            <th className="w-12 pb-2 text-center">#</th>
            <th className="pb-2 text-left">Holder</th>
            <th className="pb-2 text-right">Size</th>
            <th className="pb-2 text-right">Value</th>
            <th className="pb-2 text-right">%</th>
          </tr>
        </thead>
        <tbody>
          {holders.map((holder, index) => (
            <HolderRow key={holder.address} holder={holder} rank={index + 1} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function TopHoldersOverlay({
  open,
  onOpenChange,
  marketTitle,
  conditionId,
  tokenYesId,
  tokenNoId,
}: TopHoldersOverlayProps) {
  const [activeOutcome, setActiveOutcome] = useState<Outcome>("YES");
  const [tradeFilter, setTradeFilter] = useState<TradeFilter>("ALL");
  const [trades, setTrades] = useState<LiveTrade[]>([]);
  const [connectionState, setConnectionState] = useState<ConnectionState>("idle");
  const [lastTradeAt, setLastTradeAt] = useState<number | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const pingRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const activeTokenId = activeOutcome === "YES" ? tokenYesId : tokenNoId;

  const { data, isLoading } = useQuery({
    queryKey: ["market-holders", conditionId, activeTokenId, "overlay"],
    queryFn: () => fetchMarketHolders(conditionId, activeTokenId, 10),
    enabled: Boolean(conditionId && activeTokenId),
    staleTime: 60_000,
  });

  useEffect(() => {
    if (!open) {
      setConnectionState("idle");
      return;
    }

    if (!tokenYesId || !tokenNoId) {
      setConnectionState("closed");
      return;
    }

    setTrades([]);
    setLastTradeAt(null);
    setConnectionState("connecting");

    const ws = new WebSocket(WSS_MARKET_URL);
    wsRef.current = ws;

    ws.onopen = () => {
      setConnectionState("live");
      ws.send(
        JSON.stringify({
          assets_ids: [tokenYesId, tokenNoId],
          type: "market",
        })
      );
    };

    ws.onmessage = (event) => {
      if (typeof event.data !== "string") {
        return;
      }
      let payload: any;
      try {
        payload = JSON.parse(event.data);
      } catch {
        return;
      }
      if (payload?.event_type !== "last_trade_price") {
        return;
      }

      const price = Number(payload.price);
      const size = Number(payload.size);
      if (!Number.isFinite(price) || !Number.isFinite(size)) {
        return;
      }

      const assetId = String(payload.asset_id || "");
      const outcome: LiveTrade["outcome"] =
        assetId === tokenYesId ? "YES" : assetId === tokenNoId ? "NO" : "UNKNOWN";

      const timestamp = Number(payload.timestamp) || Date.now();
      const side = payload.side === "SELL" ? "SELL" : "BUY";
      const value = price * size;
      const id = `${timestamp}-${assetId}-${price}-${size}-${side}`;

      const trade: LiveTrade = {
        id,
        assetId,
        outcome,
        side,
        price,
        size,
        value,
        timestamp,
      };

      setTrades((prev) => [trade, ...prev].slice(0, 40));
      setLastTradeAt(timestamp);
    };

    ws.onerror = () => {
      setConnectionState("closed");
    };

    ws.onclose = () => {
      setConnectionState("closed");
    };

    pingRef.current = setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send("PING");
      }
    }, 10_000);

    return () => {
      if (pingRef.current) {
        clearInterval(pingRef.current);
        pingRef.current = null;
      }
      ws.close();
      wsRef.current = null;
    };
  }, [open, tokenNoId, tokenYesId]);

  const filteredTrades = useMemo(() => {
    if (tradeFilter === "ALL") return trades;
    return trades.filter((trade) => trade.outcome === tradeFilter);
  }, [tradeFilter, trades]);

  const connectionLabel = useMemo(() => {
    if (connectionState === "live") return "Live";
    if (connectionState === "connecting") return "Connecting";
    if (connectionState === "closed") return "Offline";
    return "Idle";
  }, [connectionState]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(96vw,1200px)] max-w-[1200px] max-h-[90vh] overflow-hidden border-border/60 bg-card/95 p-0 shadow-2xl">
        <DialogHeader className="border-b border-border/60 px-6 py-5">
          <DialogTitle className="flex items-center gap-2 text-xl">
            <Users className="h-5 w-5 text-primary" />
            Top Holders & Live Tape
          </DialogTitle>
          <DialogDescription className="text-sm text-muted-foreground">
            {marketTitle ? `${marketTitle} • ` : ""}
            Real-time trade prints via Polymarket market channel.
          </DialogDescription>
        </DialogHeader>

        <div className="grid max-h-[72vh] gap-6 overflow-y-auto px-6 py-6 lg:grid-cols-[1.6fr,1fr]">
          <Card className="border-border/60 bg-card/70">
            <CardHeader className="pb-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <CardTitle className="text-base">Top Holders</CardTitle>
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    variant={activeOutcome === "YES" ? "default" : "outline"}
                    onClick={() => setActiveOutcome("YES")}
                    className={cn(
                      "text-xs",
                      activeOutcome === "YES" && "bg-emerald-500 hover:bg-emerald-600"
                    )}
                  >
                    YES
                  </Button>
                  <Button
                    size="sm"
                    variant={activeOutcome === "NO" ? "default" : "outline"}
                    onClick={() => setActiveOutcome("NO")}
                    className={cn(
                      "text-xs",
                      activeOutcome === "NO" && "bg-rose-500 hover:bg-rose-600"
                    )}
                  >
                    NO
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent className="max-h-[520px] overflow-y-auto pr-2">
              <HoldersTable holders={data?.holders || []} isLoading={isLoading} />
            </CardContent>
          </Card>

          <Card className="border-border/60 bg-card/70">
            <CardHeader className="pb-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <CardTitle className="text-base">Live Trades</CardTitle>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span
                    className={cn(
                      "flex items-center gap-1 rounded-full border px-2 py-0.5 font-mono",
                      connectionState === "live" && "border-emerald-400/40 text-emerald-400",
                      connectionState === "connecting" && "border-yellow-400/40 text-yellow-400",
                      connectionState === "closed" && "border-rose-400/40 text-rose-400"
                    )}
                  >
                    <Activity className="h-3 w-3" />
                    {connectionLabel}
                  </span>
                  {lastTradeAt && (
                    <span className="font-mono text-[10px]">
                      {formatTime(lastTradeAt)}
                    </span>
                  )}
                </div>
              </div>
              <div className="mt-3 flex flex-wrap items-center gap-2">
                {(["ALL", "YES", "NO"] as const).map((filter) => (
                  <Button
                    key={filter}
                    size="sm"
                    variant={tradeFilter === filter ? "default" : "ghost"}
                    onClick={() => setTradeFilter(filter)}
                    className={cn(
                      "h-8 px-3 text-xs font-mono",
                      tradeFilter === filter && "bg-primary/10 text-primary"
                    )}
                  >
                    {filter}
                  </Button>
                ))}
              </div>
            </CardHeader>
            <CardContent className="max-h-[520px] overflow-y-auto pr-2">
              {!tokenYesId || !tokenNoId ? (
                <div className="py-10 text-center text-sm text-muted-foreground">
                  Token metadata unavailable for trade streaming.
                </div>
              ) : filteredTrades.length === 0 ? (
                <div className="py-10 text-center text-sm text-muted-foreground">
                  Waiting for new trades…
                </div>
              ) : (
                <div className="space-y-2">
                  {filteredTrades.map((trade) => {
                    const isBuy = trade.side === "BUY";
                    const isYes = trade.outcome === "YES";
                    return (
                      <div
                        key={trade.id}
                        className="flex flex-col gap-2 rounded-lg border border-border/50 bg-background/40 p-3"
                      >
                        <div className="flex items-center justify-between gap-3">
                          <div className="flex items-center gap-2">
                            <span
                              className={cn(
                                "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-mono",
                                isBuy
                                  ? "border-emerald-400/40 bg-emerald-400/10 text-emerald-400"
                                  : "border-rose-400/40 bg-rose-400/10 text-rose-400"
                              )}
                            >
                              {isBuy ? (
                                <TrendingUp className="h-3 w-3" />
                              ) : (
                                <TrendingDown className="h-3 w-3" />
                              )}
                              {trade.side}
                            </span>
                            <span
                              className={cn(
                                "inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-mono",
                                isYes
                                  ? "border-emerald-400/40 text-emerald-400"
                                  : "border-rose-400/40 text-rose-400"
                              )}
                            >
                              {trade.outcome}
                            </span>
                          </div>
                          <span className="text-[10px] font-mono text-muted-foreground">
                            {formatTime(trade.timestamp)}
                          </span>
                        </div>
                        <div className="grid grid-cols-3 gap-2 text-xs">
                          <div>
                            <p className="text-[10px] uppercase tracking-wide text-muted-foreground">
                              Price
                            </p>
                            <p className="font-mono text-foreground">
                              {formatPrice(trade.price)}
                            </p>
                          </div>
                          <div>
                            <p className="text-[10px] uppercase tracking-wide text-muted-foreground">
                              Size
                            </p>
                            <p className="font-mono text-foreground">
                              {formatSize(trade.size)}
                            </p>
                          </div>
                          <div>
                            <p className="text-[10px] uppercase tracking-wide text-muted-foreground">
                              Value
                            </p>
                            <p className="font-mono text-foreground">
                              {formatCurrency(trade.value)}
                            </p>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </DialogContent>
    </Dialog>
  );
}
