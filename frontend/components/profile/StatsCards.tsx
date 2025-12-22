"use client";

/**
 * @description
 * Stats Cards component showing "Moneyball" performance metrics.
 * Displays Win Rate, Total Volume, Realized PnL, and other stats.
 */

import { TrendingUp, TrendingDown, DollarSign, Target, BarChart3, Award } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import type { TraderStats } from "@/types";

interface StatsCardsProps {
  stats: TraderStats | undefined;
  isLoading?: boolean;
}

function formatCurrency(value: number): string {
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
  icon: React.ReactNode;
  trend?: "up" | "down" | "neutral";
  color?: string;
}

function StatCard({ title, value, subtitle, icon, trend, color }: StatCardProps) {
  const trendColor =
    trend === "up"
      ? "text-green-500"
      : trend === "down"
        ? "text-red-500"
        : "text-muted-foreground";

  return (
    <Card className="border-border/50 bg-card/60 backdrop-blur hover:border-primary/30 transition-colors">
      <CardContent className="p-4">
        <div className="flex items-start justify-between">
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {title}
            </span>
            <span className={`text-2xl font-bold ${color || trendColor}`}>
              {value}
            </span>
            {subtitle && (
              <span className="text-xs text-muted-foreground">{subtitle}</span>
            )}
          </div>
          <div className={`rounded-lg bg-primary/10 p-2 ${color || "text-primary"}`}>
            {icon}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

export function StatsCards({ stats, isLoading }: StatsCardsProps) {
  if (isLoading || !stats) {
    return (
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-3 xl:grid-cols-6">
        {[...Array(6)].map((_, i) => (
          <Card key={i} className="border-border/50 bg-card/60 animate-pulse">
            <CardContent className="p-4">
              <div className="h-16 rounded bg-muted/50" />
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  const pnlTrend = stats.realized_pnl >= 0 ? "up" : "down";
  const pnlColor = stats.realized_pnl >= 0 ? "text-green-500" : "text-red-500";

  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-3 xl:grid-cols-6">
      <StatCard
        title="Win Rate"
        value={formatPercent(stats.win_rate)}
        subtitle={`${stats.winning_trades}W / ${stats.losing_trades}L`}
        icon={<Target className="h-5 w-5" />}
        trend={stats.win_rate >= 50 ? "up" : "down"}
      />
      <StatCard
        title="Total Volume"
        value={formatCurrency(stats.total_volume)}
        subtitle={`${stats.total_trades} trades`}
        icon={<BarChart3 className="h-5 w-5" />}
        trend="neutral"
        color="text-blue-500"
      />
      <StatCard
        title="Realized PnL"
        value={formatCurrency(stats.realized_pnl)}
        icon={
          pnlTrend === "up" ? (
            <TrendingUp className="h-5 w-5" />
          ) : (
            <TrendingDown className="h-5 w-5" />
          )
        }
        trend={pnlTrend}
        color={pnlColor}
      />
      <StatCard
        title="Avg Trade Size"
        value={formatCurrency(stats.avg_trade_size)}
        icon={<DollarSign className="h-5 w-5" />}
        trend="neutral"
        color="text-purple-500"
      />
      <StatCard
        title="Open Positions"
        value={stats.open_positions.toString()}
        icon={<Award className="h-5 w-5" />}
        trend="neutral"
        color="text-orange-500"
      />
      <StatCard
        title="Closed Positions"
        value={stats.closed_positions.toString()}
        icon={<Award className="h-5 w-5" />}
        trend="neutral"
        color="text-cyan-500"
      />
    </div>
  );
}
