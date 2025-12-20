/**
 * @description
 * MarketTicker Component.
 * Displays a high-density feed of markets categorized by "Fresh Drops" and "High Velocity".
 * Uses CSS Grid for layout and Tailwind for "Cyber-Terminal" styling.
 *
 * @dependencies
 * - framer-motion: For entry animations
 * - lucide-react: For icons
 * - frontend/types: Market interface
 */

"use client";

import React from "react";
import { motion } from "framer-motion";
import { TrendingUp, Zap, Clock, ArrowUpRight, CalendarDays } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Market } from "@/types";
import { useRouter } from "next/navigation";
import { calculateDisplayPrice } from "@/lib/price-utils";

// Temporary Badge component if Shadcn one isn't fully set up yet in previous steps, 
// but usually 'npx shadcn-ui@latest add badge' would add it. 
// I'll implement a simple inline one to be safe or assume standard class usage.
const SimpleBadge = ({ children, className }: { children: React.ReactNode; className?: string }) => (
  <span className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 ${className}`}>
    {children}
  </span>
);

interface MarketLaneProps {
  title: string;
  icon: React.ReactNode;
  markets: Market[];
  colorClass: string; // e.g., "text-constructive"
  volumeMode?: "all_time" | "24h";
  newCount?: number;
}

const compactCurrencyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  notation: "compact",
  maximumFractionDigits: 1,
});

const preciseCurrencyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 2,
});

const formatCurrency = (value?: number) => {
  if (!Number.isFinite(value ?? NaN) || !value) {
    return "$0.00";
  }
  if (Math.abs(value) >= 1000) {
    return compactCurrencyFormatter.format(value);
  }
  if (Math.abs(value) >= 1) {
    return preciseCurrencyFormatter.format(value);
  }
  return `$${value.toFixed(4)}`;
};

const formatCents = (value?: number) => {
  if (!Number.isFinite(value ?? NaN) || value === undefined) {
    return "--";
  }
  return `${(value * 100).toFixed(1)}¢`;
};

const fallbackOutcomes = ["Yes", "No"];

const parseOutcomes = (outcomes?: string): string[] => {
  if (!outcomes) {
    return fallbackOutcomes;
  }
  try {
    const parsed = JSON.parse(outcomes);
    if (Array.isArray(parsed) && parsed.length) {
      return parsed.map((value) => String(value));
    }
  } catch {
    // ignore
  }
  return fallbackOutcomes;
};

const formatSpread = (spread?: number) => {
  if (spread === undefined || spread === null) return "--";
  return `${(spread * 100).toFixed(1)}¢`;
};

const formatDate = (date?: string | null) => {
  if (!date) return "--";
  const parsed = new Date(date);
  if (Number.isNaN(parsed.getTime())) return "--";
  return parsed.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
};

const MarketCard = ({
  market,
  volumeMode = "all_time",
}: {
  market: Market;
  volumeMode?: "all_time" | "24h";
}) => {
  const router = useRouter();
  const outcomes = React.useMemo(() => parseOutcomes(market.outcomes), [market.outcomes]);
  const primaryOutcome = outcomes[0] ?? fallbackOutcomes[0];
  const secondaryOutcome = outcomes[1] ?? fallbackOutcomes[1];
  const coverImage = market.image_url || market.icon_url;
  const volumeValue = volumeMode === "24h" ? market.volume_24h : market.volume_all_time ?? market.volume_24h;
  const volumeLabel = volumeMode === "24h" ? "24h Volume" : "Total Volume";

  return (
    <motion.div
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      whileHover={{ scale: 1.02, backgroundColor: "hsl(var(--muted))" }}
      className="group cursor-pointer rounded-md border border-border bg-card/50 p-3 transition-all hover:border-primary"
      onClick={() => router.push(`/market/${market.slug}`)}
    >
      <div className="flex justify-between gap-3 items-start mb-2">
        <div className="flex-1">
          <h4 className="text-sm font-medium leading-tight line-clamp-2 text-foreground/90 group-hover:text-primary">
            {market.title}
          </h4>
        </div>
        <div className="flex items-center gap-2">
          {coverImage ? (
            <div className="h-10 w-10 rounded-md overflow-hidden border border-border/40 bg-background/40">
              <img
                src={coverImage}
                alt={market.title}
                className="h-full w-full object-cover"
                loading="lazy"
              />
            </div>
          ) : null}
          <ArrowUpRight className="h-4 w-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
        </div>
      </div>

      <div className="mt-3 grid grid-cols-2 gap-3 text-[11px] font-mono text-muted-foreground">
        <div className="rounded-md border border-border/60 bg-background/50 p-2">
          <div className="flex items-center text-[10px] uppercase tracking-wide opacity-70">
            <span className="truncate pr-2">{primaryOutcome}</span>
          </div>
          <div className="text-sm font-semibold text-constructive">
            {formatCents(
              calculateDisplayPrice(
                market.yes_best_bid,
                market.yes_best_ask,
                market.yes_price,
                `${market.condition_id}:yes`
              )
            )}
          </div>
        </div>
        <div className="rounded-md border border-border/60 bg-background/50 p-2">
          <div className="flex items-center text-[10px] uppercase tracking-wide opacity-70">
            <span className="truncate pr-2">{secondaryOutcome}</span>
          </div>
          <div className="text-sm font-semibold text-destructive">
            {formatCents(
              calculateDisplayPrice(
                market.no_best_bid,
                market.no_best_ask,
                market.no_price,
                `${market.condition_id}:no`
              )
            )}
          </div>
        </div>
      </div>

      <div className="mt-3 grid grid-cols-2 gap-2 text-[11px] font-mono text-muted-foreground">
        <div className="flex flex-col gap-1">
          <span className="text-[10px] uppercase tracking-wide opacity-70">{volumeLabel}</span>
          <span className="text-xs text-primary flex items-center gap-1">
            <TrendingUp className="h-3 w-3" />
            {formatCurrency(volumeValue)}
          </span>
        </div>
        <div className="flex flex-col items-end gap-1">
          <span className="text-[10px] uppercase tracking-wide opacity-70">Total Liquidity</span>
          <span className="text-xs text-constructive">{formatCurrency(market.liquidity)}</span>
        </div>
      </div>
      <div className="mt-3 flex flex-col gap-1 text-[10px] font-mono text-muted-foreground/80">
        <div className="flex items-center justify-between">
          <span>Spread</span>
          <span>{formatSpread(market.spread)}</span>
        </div>
        <div className="flex items-center justify-between gap-2">
          <span className="inline-flex items-center gap-1">
            <CalendarDays className="h-3 w-3" />
            Starts {formatDate(market.start_date)}
          </span>
          <span className="inline-flex items-center gap-1">
            <CalendarDays className="h-3 w-3" />
            Ends {formatDate(market.end_date)}
          </span>
        </div>
      </div>
    </motion.div>
  );
};

const MarketLane = ({ title, icon, markets, colorClass, volumeMode, newCount }: MarketLaneProps) => {
  return (
    <div className="flex flex-col h-full overflow-hidden border-r border-border/50 last:border-r-0 bg-background/30 backdrop-blur-sm">
      <div className={`flex items-center gap-2 p-4 border-b border-border/50 font-mono uppercase tracking-wider text-sm ${colorClass}`}>
        {icon}
        <h3>{title}</h3>
        <div className="ml-auto flex items-center gap-2">
          {newCount && newCount > 0 ? (
            <SimpleBadge className="border-primary/40 bg-primary/10 text-primary">
              +{newCount} new
            </SimpleBadge>
          ) : null}
          <SimpleBadge className="border-border bg-background text-foreground/70">
            {markets.length}
          </SimpleBadge>
        </div>
      </div>
      <div className="flex-1 overflow-y-auto p-3 space-y-3 custom-scrollbar">
        {markets.map((market) => (
          <MarketCard key={market.condition_id} market={market} volumeMode={volumeMode} />
        ))}
        {markets.length === 0 && (
          <div className="text-center text-muted-foreground text-xs py-10">
            No active markets found in this lane.
          </div>
        )}
      </div>
    </div>
  );
};

interface MarketTickerProps {
  freshDrops: Market[];
  highVelocity: Market[];
  deepLiquidity: Market[];
  newCounts?: {
    fresh: number;
    high_velocity: number;
    deep_liquidity: number;
  };
}

export const MarketTicker = ({ freshDrops, highVelocity, deepLiquidity, newCounts }: MarketTickerProps) => {
  return (
    <div className="grid grid-cols-1 md:grid-cols-3 h-[600px] border border-border rounded-lg overflow-hidden shadow-2xl shadow-black/50">
      <MarketLane
        title="Fresh Drops"
        icon={<Clock className="h-4 w-4" />}
        markets={freshDrops}
        colorClass="text-blue-400"
        newCount={newCounts?.fresh}
      />
      <MarketLane
        title="High Velocity"
        icon={<Zap className="h-4 w-4" />}
        markets={highVelocity}
        colorClass="text-yellow-400"
        newCount={newCounts?.high_velocity}
      />
      <MarketLane
        title="Deep Liquidity"
        icon={<TrendingUp className="h-4 w-4" />}
        markets={deepLiquidity}
        colorClass="text-constructive"
        newCount={newCounts?.deep_liquidity}
      />
    </div>
  );
};
