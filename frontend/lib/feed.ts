/**
 * @description
 * Chart data helpers for transforming history responses and merging live ticks.
 * Converts backend history points ({ t, p }) into Lightweight Charts line data.
 */

import { Time, LineData } from "lightweight-charts";

export type HistoryPoint = {
  t: number; // unix timestamp (seconds)
  p: number; // price (0-1)
};

export type ChartDataPoint = LineData;

const clampPrice = (value: number) => Math.min(1, Math.max(0, value));

/**
 * Transforms backend history points to Lightweight Charts line data.
 */
export function transformHistoryData(history: HistoryPoint[]): ChartDataPoint[] {
  if (!Array.isArray(history)) return [];

  const validPoints = history.filter(
    (point) => Number.isFinite(point.t) && Number.isFinite(point.p)
  );

  const sorted = [...validPoints].sort((a, b) => a.t - b.t);

  return sorted.map((point) => ({
    time: point.t as Time,
    value: clampPrice(point.p),
  }));
}

/**
 * Derives the "NO" outcome price series from "YES" outcome data.
 * Assumption: Price(NO) = 1 - Price(YES) for binary markets.
 */
export function deriveInverseSeries(data: ChartDataPoint[]): ChartDataPoint[] {
  return data.map((point) => ({
    time: point.time,
    value: clampPrice(1 - point.value),
  }));
}

/**
 * Merges a live tick into the existing dataset.
 * Updates the last point if the timestamp matches, or appends a new one.
 */
export function mergeLiveTick(
  currentData: ChartDataPoint[],
  tick: { time: number; price: number }
): ChartDataPoint[] {
  if (!Number.isFinite(tick.time) || !Number.isFinite(tick.price)) {
    return currentData;
  }

  const nextPoint = { time: tick.time as Time, value: clampPrice(tick.price) };

  if (currentData.length === 0) {
    return [nextPoint];
  }

  const lastPoint = currentData[currentData.length - 1];

  if ((tick.time as number) < (lastPoint.time as number)) {
    return currentData;
  }

  if ((tick.time as number) === (lastPoint.time as number)) {
    const updated = [...currentData];
    updated[updated.length - 1] = nextPoint;
    return updated;
  }

  return [...currentData, nextPoint];
}
