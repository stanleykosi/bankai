/**
 * @description
 * React Query hooks for watchlist/bookmark operations.
 *
 * @dependencies
 * - @tanstack/react-query
 * - @/lib/watchlist-api
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  bookmarkMarket,
  removeBookmark,
  toggleBookmark,
  fetchWatchlist,
  checkIsBookmarked,
} from "@/lib/watchlist-api";
import { useWallet } from "@/hooks/useWallet";

/**
 * Hook for checking if a market is bookmarked
 */
export function useIsBookmarked(marketId: string | undefined) {
  const { isAuthenticated, isLoading, hasSession } = useWallet();

  return useQuery({
    queryKey: ["is-bookmarked", marketId],
    queryFn: () => checkIsBookmarked(marketId!),
    enabled: Boolean(marketId) && !isLoading && (isAuthenticated || hasSession),
    staleTime: 30_000,
  });
}

/**
 * Hook for getting user's watchlist
 */
export function useWatchlist() {
  const { isAuthenticated, isLoading, hasSession } = useWallet();

  return useQuery({
    queryKey: ["watchlist"],
    queryFn: () => fetchWatchlist(),
    enabled: !isLoading && (isAuthenticated || hasSession),
    staleTime: 30_000,
    refetchInterval: 30_000, // Refresh prices every 30 seconds
  });
}

/**
 * Hook for bookmark mutation
 */
export function useBookmarkMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (marketId: string) => bookmarkMarket(marketId),
    onSuccess: (data, marketId) => {
      queryClient.invalidateQueries({ queryKey: ["is-bookmarked", marketId] });
      queryClient.invalidateQueries({ queryKey: ["watchlist"] });
    },
  });
}

/**
 * Hook for remove bookmark mutation
 */
export function useRemoveBookmarkMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (marketId: string) => removeBookmark(marketId),
    onSuccess: (data, marketId) => {
      queryClient.invalidateQueries({ queryKey: ["is-bookmarked", marketId] });
      queryClient.invalidateQueries({ queryKey: ["watchlist"] });
    },
  });
}

/**
 * Hook for toggle bookmark mutation
 */
export function useToggleBookmarkMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (marketId: string) => toggleBookmark(marketId),
    onSuccess: (data, marketId) => {
      queryClient.invalidateQueries({ queryKey: ["is-bookmarked", marketId] });
      queryClient.invalidateQueries({ queryKey: ["watchlist"] });
    },
  });
}

/**
 * Combined hook for bookmark toggle with optimistic updates
 */
export function useBookmarkToggle(marketId: string | undefined) {
  const { data: bookmarkStatus, isLoading: isChecking } = useIsBookmarked(marketId);
  const toggleMutation = useToggleBookmarkMutation();
  const queryClient = useQueryClient();

  const isBookmarked = bookmarkStatus?.is_bookmarked ?? false;
  const isLoading = isChecking || toggleMutation.isPending;

  const toggle = async () => {
    if (!marketId) return;

    // Optimistic update
    queryClient.setQueryData(["is-bookmarked", marketId], {
      is_bookmarked: !isBookmarked,
      market_id: marketId,
    });

    try {
      await toggleMutation.mutateAsync(marketId);
    } catch (error) {
      // Revert on error
      queryClient.setQueryData(["is-bookmarked", marketId], {
        is_bookmarked: isBookmarked,
        market_id: marketId,
      });
      throw error;
    }
  };

  return {
    isBookmarked,
    isLoading,
    toggle,
  };
}
