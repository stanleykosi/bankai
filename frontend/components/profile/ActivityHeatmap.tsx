"use client";

/**
 * @description
 * Activity Heatmap component - GitHub-style contribution graph.
 * Shows trading frequency over the past year.
 */

import { useMemo } from "react";
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
  "bg-muted/30",           // Level 0 - no activity
  "bg-green-500/30",       // Level 1
  "bg-green-500/50",       // Level 2
  "bg-green-500/70",       // Level 3
  "bg-green-500",          // Level 4 - max activity
];

const MONTH_LABELS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function ActivityHeatmap({ activity, isLoading }: ActivityHeatmapProps) {
  const weeks = useMemo(() => generateDateGrid(), []);

  const activityMap = useMemo(() => {
    if (!activity) return new Map<string, ActivityDataPoint>();
    return new Map(activity.map((dp) => [dp.date, dp]));
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
      <div className="rounded-lg border border-border/50 bg-card/60 p-4">
        <div className="mb-2 h-4 w-32 animate-pulse rounded bg-muted/50" />
        <div className="h-[120px] animate-pulse rounded bg-muted/30" />
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-border/50 bg-card/60 p-4">
      <h3 className="mb-4 text-sm font-medium text-muted-foreground">
        Trading Activity (Last 52 Weeks)
      </h3>

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
                    className={`h-[10px] w-[10px] rounded-sm ${LEVEL_COLORS[level]} cursor-pointer hover:ring-1 hover:ring-primary transition-all`}
                    title={`${date}: ${tradeCount} trades`}
                  />
                );
              })}
            </div>
          ))}
        </div>
      </div>

      {/* Legend */}
      <div className="mt-4 flex items-center justify-end gap-2 text-xs text-muted-foreground">
        <span>Less</span>
        {LEVEL_COLORS.map((color, i) => (
          <div
            key={i}
            className={`h-[10px] w-[10px] rounded-sm ${color}`}
          />
        ))}
        <span>More</span>
      </div>
    </div>
  );
}
