/**
 * @description
 * React Query hooks for follow/unfollow operations.
 *
 * @dependencies
 * - @tanstack/react-query
 * - @clerk/nextjs
 * - @/lib/social-api
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@clerk/nextjs";
import {
  followTrader,
  unfollowTrader,
  fetchFollowing,
  checkIsFollowing,
} from "@/lib/social-api";

/**
 * Hook for checking if user is following a trader
 */
export function useIsFollowing(targetAddress: string | undefined) {
  const { getToken, isSignedIn } = useAuth();

  return useQuery({
    queryKey: ["is-following", targetAddress],
    queryFn: () => checkIsFollowing(targetAddress!, getToken),
    enabled: Boolean(targetAddress) && isSignedIn,
    staleTime: 30_000,
  });
}

/**
 * Hook for getting list of followed traders
 */
export function useFollowing() {
  const { getToken, isSignedIn } = useAuth();

  return useQuery({
    queryKey: ["following-list"],
    queryFn: () => fetchFollowing(getToken),
    enabled: isSignedIn,
    staleTime: 60_000,
  });
}

/**
 * Hook for follow mutation
 */
export function useFollowMutation() {
  const { getToken } = useAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (targetAddress: string) => followTrader(targetAddress, getToken),
    onSuccess: (data, targetAddress) => {
      // Invalidate related queries
      queryClient.invalidateQueries({ queryKey: ["is-following", targetAddress] });
      queryClient.invalidateQueries({ queryKey: ["following-list"] });
      queryClient.invalidateQueries({ queryKey: ["trader-profile", targetAddress] });
    },
  });
}

/**
 * Hook for unfollow mutation
 */
export function useUnfollowMutation() {
  const { getToken } = useAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (targetAddress: string) => unfollowTrader(targetAddress, getToken),
    onSuccess: (data, targetAddress) => {
      // Invalidate related queries
      queryClient.invalidateQueries({ queryKey: ["is-following", targetAddress] });
      queryClient.invalidateQueries({ queryKey: ["following-list"] });
      queryClient.invalidateQueries({ queryKey: ["trader-profile", targetAddress] });
    },
  });
}

/**
 * Combined hook for follow/unfollow with optimistic updates
 */
export function useFollowToggle(targetAddress: string | undefined) {
  const { data: followStatus, isLoading: isChecking } = useIsFollowing(targetAddress);
  const followMutation = useFollowMutation();
  const unfollowMutation = useUnfollowMutation();

  const isFollowing = followStatus?.is_following ?? false;
  const isLoading = isChecking || followMutation.isPending || unfollowMutation.isPending;

  const toggle = async () => {
    if (!targetAddress) return;

    if (isFollowing) {
      await unfollowMutation.mutateAsync(targetAddress);
    } else {
      await followMutation.mutateAsync(targetAddress);
    }
  };

  return {
    isFollowing,
    isLoading,
    toggle,
    follow: followMutation.mutateAsync,
    unfollow: unfollowMutation.mutateAsync,
  };
}
