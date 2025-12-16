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

const isValidNumber = (value: unknown): value is number =>
  typeof value === "number" && Number.isFinite(value);

const clampPrice = (value: unknown) => {
  if (!isValidNumber(value)) {
    return null;
  }
  const clamped = Math.min(1, Math.max(0, value));
  return isValidNumber(clamped) ? clamped : null;
};

/**
 * Transforms backend history points to Lightweight Charts line data.
 */
export function transformHistoryData(history: HistoryPoint[]): ChartDataPoint[] {
  if (!Array.isArray(history)) return [];

  const validPoints = history.filter(
    (point) => isValidNumber(point.t) && isValidNumber(point.p)
  );

  const sorted = [...validPoints].sort((a, b) => a.t - b.t);

  return sorted
    .map((point) => {
      const value = clampPrice(point.p);
      if (value === null) return null;
      return {
        time: point.t as Time,
        value,
      };
    })
    .filter((point): point is ChartDataPoint => Boolean(point) && isValidNumber(point.value));
}

/**
 * Derives the "NO" outcome price series from "YES" outcome data.
 * Assumption: Price(NO) = 1 - Price(YES) for binary markets.
 */
export function deriveInverseSeries(data: ChartDataPoint[]): ChartDataPoint[] {
  return data
    .filter((point) => isValidNumber(point.time) && isValidNumber(point.value))
    .map((point) => ({
      time: point.time,
      value: clampPrice(1 - point.value) ?? 0,
    }))
    .filter((point) => isValidNumber(point.value));
}

/**
 * Merges a live tick into the existing dataset.
 * Updates the last point if the timestamp matches, or appends a new one.
 */
export function mergeLiveTick(
  currentData: ChartDataPoint[],
  tick: { time: number; price: number }
): ChartDataPoint[] {
  if (!isValidNumber(tick.time) || !isValidNumber(tick.price)) {
    return currentData;
  }

  const value = clampPrice(tick.price);
  if (value === null) {
    return currentData;
  }

  const nextPoint = { time: tick.time as Time, value };

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
