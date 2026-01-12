/**
 * @description
 * API functions for social features (follow system, notifications).
 * Requires authentication via wallet session cookie.
 *
 * @dependencies
 * - axios
 * - @/lib/api
 */

import { api } from "./api";
import type {
  FollowingResponse,
  FollowStatusResponse,
  FollowActionResponse,
  NotificationsResponse,
  FollowingPerformanceResponse,
} from "@/types";

/**
 * Follow a trader
 */
export async function followTrader(
  targetAddress: string
): Promise<FollowActionResponse> {
  const response = await api.post<FollowActionResponse>(
    "/social/follow",
    { target_address: targetAddress },
    { withCredentials: true }
  );
  return response.data;
}

/**
 * Unfollow a trader
 */
export async function unfollowTrader(
  targetAddress: string
): Promise<FollowActionResponse> {
  const response = await api.delete<FollowActionResponse>(
    `/social/follow/${targetAddress}`
  );
  return response.data;
}

/**
 * Get list of traders user is following
 */
export async function fetchFollowing(
): Promise<FollowingResponse> {
  const response = await api.get<FollowingResponse>("/social/following");
  return response.data;
}

/**
 * Get list of followed traders with performance stats
 */
export async function fetchFollowingPerformance(
): Promise<FollowingPerformanceResponse> {
  const response = await api.get<FollowingPerformanceResponse>(
    "/social/following/performance"
  );
  return response.data;
}

/**
 * Check if user is following a specific trader
 */
export async function checkIsFollowing(
  targetAddress: string
): Promise<FollowStatusResponse> {
  const response = await api.get<FollowStatusResponse>(
    `/social/following/${targetAddress}`
  );
  return response.data;
}

/**
 * Fetch user notifications
 */
export async function fetchNotifications(
  limit = 50,
  offset = 0
): Promise<NotificationsResponse> {
  const response = await api.get<NotificationsResponse>(
    "/social/notifications",
    { params: { limit, offset } }
  );
  return response.data;
}

/**
 * Mark a notification as read
 */
export async function markNotificationRead(
  notificationId: string
): Promise<{ success: boolean }> {
  const response = await api.post<{ success: boolean }>(
    `/social/notifications/${notificationId}/read`,
    {},
    { withCredentials: true }
  );
  return response.data;
}

/**
 * Mark all notifications as read
 */
export async function markAllNotificationsRead(
): Promise<{ success: boolean }> {
  const response = await api.post<{ success: boolean }>(
    "/social/notifications/read-all",
    {},
    { withCredentials: true }
  );
  return response.data;
}
