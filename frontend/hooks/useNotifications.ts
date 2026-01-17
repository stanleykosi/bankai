/**
 * @description
 * React Query hooks for notifications.
 *
 * @dependencies
 * - @tanstack/react-query
 * - @/lib/social-api
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  fetchNotifications,
  markNotificationRead,
  markAllNotificationsRead,
} from "@/lib/social-api";
import { useWallet } from "@/hooks/useWallet";

/**
 * Hook for fetching notifications
 */
export function useNotifications(limit = 50, offset = 0, enabled = true) {
  const { isAuthenticated } = useWallet();

  return useQuery({
    queryKey: ["notifications", limit, offset],
    queryFn: () => fetchNotifications(limit, offset),
    enabled: Boolean(enabled) && isAuthenticated,
    staleTime: 30_000,
    refetchInterval: 60_000, // Poll every minute
  });
}

/**
 * Hook for getting unread notification count
 */
export function useUnreadCount(enabled = true) {
  const { data } = useNotifications(50, 0, enabled);
  return data?.unread_count ?? 0;
}

/**
 * Hook for marking a notification as read
 */
export function useMarkReadMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (notificationId: string) =>
      markNotificationRead(notificationId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
    },
  });
}

/**
 * Hook for marking all notifications as read
 */
export function useMarkAllReadMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => markAllNotificationsRead(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
    },
  });
}

/**
 * Combined hook for notification management
 */
export function useNotificationManager() {
  const { data, isLoading, refetch } = useNotifications();
  const markReadMutation = useMarkReadMutation();
  const markAllReadMutation = useMarkAllReadMutation();

  return {
    notifications: data?.notifications ?? [],
    unreadCount: data?.unread_count ?? 0,
    isLoading,
    refetch,
    markAsRead: markReadMutation.mutateAsync,
    markAllAsRead: markAllReadMutation.mutateAsync,
    isMarkingRead: markReadMutation.isPending,
    isMarkingAllRead: markAllReadMutation.isPending,
  };
}
