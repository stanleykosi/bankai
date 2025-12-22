/**
 * @description
 * API functions for social features (follow system, notifications).
 * Requires authentication via Clerk.
 *
 * @dependencies
 * - axios
 * - @clerk/nextjs
 * - @/lib/api
 */

import { api } from "./api";
import type {
  FollowingResponse,
  FollowStatusResponse,
  FollowActionResponse,
  NotificationsResponse,
} from "@/types";

/**
 * Get auth headers for protected routes
 */
async function getAuthHeaders(getToken: () => Promise<string | null>) {
  const token = await getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/**
 * Follow a trader
 */
export async function followTrader(
  targetAddress: string,
  getToken: () => Promise<string | null>
): Promise<FollowActionResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.post<FollowActionResponse>(
    "/social/follow",
    { target_address: targetAddress },
    { headers }
  );
  return response.data;
}

/**
 * Unfollow a trader
 */
export async function unfollowTrader(
  targetAddress: string,
  getToken: () => Promise<string | null>
): Promise<FollowActionResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.delete<FollowActionResponse>(
    `/social/follow/${targetAddress}`,
    { headers }
  );
  return response.data;
}

/**
 * Get list of traders user is following
 */
export async function fetchFollowing(
  getToken: () => Promise<string | null>
): Promise<FollowingResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.get<FollowingResponse>("/social/following", {
    headers,
  });
  return response.data;
}

/**
 * Check if user is following a specific trader
 */
export async function checkIsFollowing(
  targetAddress: string,
  getToken: () => Promise<string | null>
): Promise<FollowStatusResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.get<FollowStatusResponse>(
    `/social/following/${targetAddress}`,
    { headers }
  );
  return response.data;
}

/**
 * Fetch user notifications
 */
export async function fetchNotifications(
  getToken: () => Promise<string | null>,
  limit = 50,
  offset = 0
): Promise<NotificationsResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.get<NotificationsResponse>(
    "/social/notifications",
    { headers, params: { limit, offset } }
  );
  return response.data;
}

/**
 * Mark a notification as read
 */
export async function markNotificationRead(
  notificationId: string,
  getToken: () => Promise<string | null>
): Promise<{ success: boolean }> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.post<{ success: boolean }>(
    `/social/notifications/${notificationId}/read`,
    {},
    { headers }
  );
  return response.data;
}

/**
 * Mark all notifications as read
 */
export async function markAllNotificationsRead(
  getToken: () => Promise<string | null>
): Promise<{ success: boolean }> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.post<{ success: boolean }>(
    "/social/notifications/read-all",
    {},
    { headers }
  );
  return response.data;
}
