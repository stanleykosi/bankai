"use client";

/**
 * @description
 * Activity Heatmap component - GitHub-style contribution graph.
 * Shows trading frequency over the past year.
 */

import { useMemo } from "react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import type { ActivityDataPoint } from "@/types";

interface ActivityHeatmapProps {
  activity: ActivityDataPoint[] | undefined;
  isLoading?: boolean;
}

// Generate dates for the past 52 weeks
function generateDateGrid(): string[][] {
  const weeks: string[][] = [];
  const today = new Date();
  const startDate = new Date(today);
  startDate.setDate(startDate.getDate() - 364); // ~52 weeks

  // Find the start of the week (Sunday)
  const dayOfWeek = startDate.getDay();
  startDate.setDate(startDate.getDate() - dayOfWeek);

  let currentDate = new Date(startDate);
  let currentWeek: string[] = [];

  while (currentDate <= today) {
    currentWeek.push(currentDate.toISOString().split("T")[0]);
    if (currentWeek.length === 7) {
      weeks.push(currentWeek);
      currentWeek = [];
    }
    currentDate.setDate(currentDate.getDate() + 1);
  }

  if (currentWeek.length > 0) {
    weeks.push(currentWeek);
  }

  return weeks;
}

const LEVEL_COLORS = [
  "bg-muted/30",     // Level 0 - no activity
  "bg-primary/10",   // Level 1
  "bg-primary/20",   // Level 2
  "bg-primary/30",   // Level 3
  "bg-primary/50",   // Level 4 - max activity
];

const MONTH_LABELS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function ActivityHeatmap({ activity, isLoading }: ActivityHeatmapProps) {
  const weeks = useMemo(() => generateDateGrid(), []);

  const activityMap = useMemo(() => {
    if (!activity) return new Map<string, ActivityDataPoint>();
    return new Map(activity.map((dp) => [dp.date, dp]));
  }, [activity]);

  const summary = useMemo(() => {
    if (!activity || activity.length === 0) {
      return { activeDays: 0, totalTrades: 0, totalVolume: 0 };
    }
    let activeDays = 0;
    let totalTrades = 0;
    let totalVolume = 0;
    for (const dp of activity) {
      totalTrades += dp.trade_count;
      totalVolume += dp.volume;
      if (dp.trade_count > 0) {
        activeDays += 1;
      }
    }
    return { activeDays, totalTrades, totalVolume };
  }, [activity]);

  // Calculate month labels
  const monthLabels = useMemo(() => {
    const labels: { month: string; weekIndex: number }[] = [];
    let lastMonth = -1;

    weeks.forEach((week, weekIndex) => {
      const date = new Date(week[0]);
      const month = date.getMonth();
      if (month !== lastMonth) {
        labels.push({ month: MONTH_LABELS[month], weekIndex });
        lastMonth = month;
      }
    });

    return labels;
  }, [weeks]);

  if (isLoading) {
    return (
      <Card className="border-border/60 bg-card/70">
        <CardHeader className="pb-3">
          <div className="h-4 w-40 animate-pulse rounded bg-muted/50" />
        </CardHeader>
        <CardContent className="pt-0">
          <div className="h-[140px] animate-pulse rounded bg-muted/30" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-border/60 bg-card/70">
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Trading cadence
            </p>
            <h3 className="text-lg font-semibold text-foreground">Activity Heatmap</h3>
          </div>
          <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
            <span className="font-mono">{summary.activeDays} active days</span>
            <span className="font-mono">{summary.totalTrades.toLocaleString()} trades</span>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">

      {/* Month labels */}
      <div className="mb-1 flex text-xs text-muted-foreground">
        <div className="w-6" /> {/* Spacer for day labels */}
        <div className="flex flex-1">
          {monthLabels.map(({ month, weekIndex }, i) => (
            <div
              key={i}
              className="text-[10px]"
              style={{
                marginLeft: i === 0 ? `${weekIndex * 12}px` : undefined,
                width: i < monthLabels.length - 1
                  ? `${(monthLabels[i + 1].weekIndex - weekIndex) * 12}px`
                  : undefined,
              }}
            >
              {month}
            </div>
          ))}
        </div>
      </div>

      {/* Heatmap grid */}
      <div className="flex gap-[2px]">
        {/* Day labels */}
        <div className="flex flex-col gap-[2px] text-[10px] text-muted-foreground pr-1">
          <div className="h-[10px]" />
          <div className="h-[10px]">Mon</div>
          <div className="h-[10px]" />
          <div className="h-[10px]">Wed</div>
          <div className="h-[10px]" />
          <div className="h-[10px]">Fri</div>
          <div className="h-[10px]" />
        </div>

        {/* Grid */}
        <div className="flex gap-[2px] overflow-x-auto">
          {weeks.map((week, weekIndex) => (
            <div key={weekIndex} className="flex flex-col gap-[2px]">
              {week.map((date) => {
                const dp = activityMap.get(date);
                const level = dp?.level ?? 0;
                const tradeCount = dp?.trade_count ?? 0;

                return (
                  <div
                    key={date}
                    className={`h-[10px] w-[10px] rounded-sm ${LEVEL_COLORS[level]} cursor-pointer hover:ring-1 hover:ring-primary/70 transition-all`}
                    title={`${date}: ${tradeCount} trades`}
                  />
                );
              })}
            </div>
          ))}
        </div>
      </div>

      {/* Legend */}
      <div className="mt-4 flex items-center justify-end gap-2 text-[10px] text-muted-foreground">
        <span>Less</span>
        {LEVEL_COLORS.map((color, i) => (
          <div
            key={i}
            className={`h-[10px] w-[10px] rounded-sm ${color}`}
          />
        ))}
        <span>More</span>
      </div>
      </CardContent>
    </Card>
  );
}
