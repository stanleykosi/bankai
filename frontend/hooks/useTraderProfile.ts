/**
 * @description
 * React Query hook for fetching trader profile data.
 *
 * @dependencies
 * - @tanstack/react-query
 * - @/lib/profile-api
 */

import { useQuery } from "@tanstack/react-query";
import {
  fetchTraderProfile,
  fetchTraderStats,
  fetchTraderPositions,
  fetchActivityHeatmap,
  fetchRecentTrades,
} from "@/lib/profile-api";

/**
 * Hook for fetching complete trader profile
 */
export function useTraderProfile(address: string | undefined) {
  return useQuery({
    queryKey: ["trader-profile", address],
    queryFn: () => fetchTraderProfile(address!),
    enabled: Boolean(address),
    staleTime: 60_000, // 1 minute
    gcTime: 300_000, // 5 minutes
  });
}

/**
 * Hook for fetching trader stats only
 */
export function useTraderStats(address: string | undefined) {
  return useQuery({
    queryKey: ["trader-stats", address],
    queryFn: () => fetchTraderStats(address!),
    enabled: Boolean(address),
    staleTime: 60_000,
  });
}

/**
 * Hook for fetching trader positions
 */
export function useTraderPositions(
  address: string | undefined,
  limit = 200,
  offset = 0,
  sortBy = "CASHPNL"
) {
  return useQuery({
    queryKey: ["trader-positions", address, limit, offset, sortBy],
    queryFn: () => fetchTraderPositions(address!, limit, offset, sortBy),
    enabled: Boolean(address),
    staleTime: 30_000, // 30 seconds
  });
}

/**
 * Hook for fetching activity heatmap data
 */
export function useActivityHeatmap(address: string | undefined) {
  return useQuery({
    queryKey: ["trader-activity", address],
    queryFn: () => fetchActivityHeatmap(address!),
    enabled: Boolean(address),
    staleTime: 300_000, // 5 minutes (activity doesn't change often)
  });
}

/**
 * Hook for fetching recent trades
 */
export function useRecentTrades(address: string | undefined, limit = 20) {
  return useQuery({
    queryKey: ["trader-trades", address, limit],
    queryFn: () => fetchRecentTrades(address!, limit),
    enabled: Boolean(address),
    staleTime: 30_000,
  });
}
