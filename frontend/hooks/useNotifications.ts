/**
 * @description
 * React Query hooks for notifications.
 *
 * @dependencies
 * - @tanstack/react-query
 * - @clerk/nextjs
 * - @/lib/social-api
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@clerk/nextjs";
import {
  fetchNotifications,
  markNotificationRead,
  markAllNotificationsRead,
} from "@/lib/social-api";

/**
 * Hook for fetching notifications
 */
export function useNotifications(limit = 50, offset = 0) {
  const { getToken, isSignedIn } = useAuth();

  return useQuery({
    queryKey: ["notifications", limit, offset],
    queryFn: () => fetchNotifications(getToken, limit, offset),
    enabled: isSignedIn,
    staleTime: 30_000,
    refetchInterval: 60_000, // Poll every minute
  });
}

/**
 * Hook for getting unread notification count
 */
export function useUnreadCount() {
  const { data } = useNotifications();
  return data?.unread_count ?? 0;
}

/**
 * Hook for marking a notification as read
 */
export function useMarkReadMutation() {
  const { getToken } = useAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (notificationId: string) =>
      markNotificationRead(notificationId, getToken),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notifications"] });
    },
  });
}

/**
 * Hook for marking all notifications as read
 */
export function useMarkAllReadMutation() {
  const { getToken } = useAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => markAllNotificationsRead(getToken),
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
