"use client";

/**
 * @description
 * Stats Cards component showing "Moneyball" performance metrics.
 * Displays Win Rate, Total Volume, Realized PnL, and other stats.
 */

import { Card, CardContent } from "@/components/ui/card";
import type { TraderStats } from "@/types";

interface StatsCardsProps {
  stats: TraderStats | undefined;
  isLoading?: boolean;
}

function formatCurrency(value: number): string {
  if (Math.abs(value) >= 1_000_000_000) {
    return `$${(value / 1_000_000_000).toFixed(2)}B`;
  }
  if (Math.abs(value) >= 1_000_000) {
    return `$${(value / 1_000_000).toFixed(2)}M`;
  }
  if (Math.abs(value) >= 1_000) {
    return `$${(value / 1_000).toFixed(1)}K`;
  }
  return `$${value.toFixed(2)}`;
}

function formatPercent(value: number): string {
  return `${value.toFixed(1)}%`;
}

interface StatCardProps {
  title: string;
  value: string;
  subtitle?: string;
  tone?: "positive" | "negative" | "neutral";
}

function StatCard({ title, value, subtitle, tone }: StatCardProps) {
  const toneClass =
    tone === "positive"
      ? "text-emerald-400"
      : tone === "negative"
        ? "text-rose-400"
        : "text-foreground";

  return (
    <Card className="border-border/60 bg-card/70 backdrop-blur transition-colors hover:border-primary/40">
      <CardContent className="p-4">
        <div className="flex flex-col gap-2">
          <span className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
            {title}
          </span>
          <span className={`text-2xl font-semibold font-mono ${toneClass}`}>
            {value}
          </span>
          {subtitle && (
            <span className="text-xs text-muted-foreground">{subtitle}</span>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export function StatsCards({ stats, isLoading }: StatsCardsProps) {
  if (isLoading || !stats) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {[...Array(6)].map((_, i) => (
          <Card key={i} className="border-border/60 bg-card/70 animate-pulse">
            <CardContent className="p-4">
              <div className="h-16 rounded bg-muted/50" />
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  const pnlTone = stats.realized_pnl >= 0 ? "positive" : "negative";
  const winRateTone = stats.win_rate >= 50 ? "positive" : "negative";

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatCard
        title="Win Rate"
        value={formatPercent(stats.win_rate)}
        subtitle={`${stats.winning_trades}W / ${stats.losing_trades}L`}
        tone={winRateTone}
      />
      <StatCard
        title="Total Volume"
        value={formatCurrency(stats.total_volume)}
        subtitle={`${stats.total_trades} trades`}
        tone="neutral"
      />
      <StatCard
        title="Realized PnL"
        value={formatCurrency(stats.realized_pnl)}
        tone={pnlTone}
      />
      <StatCard
        title="Avg Trade Volume"
        value={formatCurrency(stats.avg_trade_size)}
        subtitle="Per trade"
        tone="neutral"
      />
      <StatCard
        title="Open Positions"
        value={stats.open_positions.toString()}
        tone="neutral"
      />
      <StatCard
        title="Closed Positions"
        value={stats.closed_positions.toString()}
        tone="neutral"
      />
    </div>
  );
}
