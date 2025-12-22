/**
 * @description
 * API functions for trader profile data.
 * Fetches profile, stats, positions, and activity from backend.
 *
 * @dependencies
 * - axios
 * - @/lib/api
 */

import { api } from "./api";
import type {
  TraderProfileResponse,
  TraderStats,
  PositionsResponse,
  ActivityResponse,
  TradesResponse,
} from "@/types";

/**
 * Fetch complete trader profile with stats
 */
export async function fetchTraderProfile(
  address: string
): Promise<TraderProfileResponse> {
  const response = await api.get<TraderProfileResponse>(
    `/profile/${address}`
  );
  return response.data;
}

/**
 * Fetch trader performance stats
 */
export async function fetchTraderStats(
  address: string
): Promise<{ stats: TraderStats }> {
  const response = await api.get<{ stats: TraderStats }>(
    `/profile/${address}/stats`
  );
  return response.data;
}

/**
 * Fetch trader open positions
 */
export async function fetchTraderPositions(
  address: string,
  limit = 50,
  offset = 0
): Promise<PositionsResponse> {
  const response = await api.get<PositionsResponse>(
    `/profile/${address}/positions`,
    { params: { limit, offset } }
  );
  return response.data;
}

/**
 * Fetch trader activity heatmap data
 */
export async function fetchActivityHeatmap(
  address: string
): Promise<ActivityResponse> {
  const response = await api.get<ActivityResponse>(
    `/profile/${address}/activity`
  );
  return response.data;
}

/**
 * Fetch trader recent trades
 */
export async function fetchRecentTrades(
  address: string,
  limit = 20
): Promise<TradesResponse> {
  const response = await api.get<TradesResponse>(
    `/profile/${address}/trades`,
    { params: { limit } }
  );
  return response.data;
}
