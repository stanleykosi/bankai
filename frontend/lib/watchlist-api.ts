/**
 * @description
 * API functions for watchlist/bookmark features.
 * Requires authentication via Clerk.
 *
 * @dependencies
 * - axios
 * - @clerk/nextjs
 * - @/lib/api
 */

import { api } from "./api";
import type {
  WatchlistResponse,
  BookmarkStatusResponse,
  BookmarkActionResponse,
} from "@/types";

/**
 * Get auth headers for protected routes
 */
async function getAuthHeaders(getToken: () => Promise<string | null>) {
  const token = await getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/**
 * Add market to watchlist
 */
export async function bookmarkMarket(
  marketId: string,
  getToken: () => Promise<string | null>
): Promise<BookmarkActionResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.post<BookmarkActionResponse>(
    "/watchlist/bookmark",
    { market_id: marketId },
    { headers }
  );
  return response.data;
}

/**
 * Remove market from watchlist
 */
export async function removeBookmark(
  marketId: string,
  getToken: () => Promise<string | null>
): Promise<BookmarkActionResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.delete<BookmarkActionResponse>(
    `/watchlist/${encodeURIComponent(marketId)}`,
    { headers }
  );
  return response.data;
}

/**
 * Toggle bookmark status
 */
export async function toggleBookmark(
  marketId: string,
  getToken: () => Promise<string | null>
): Promise<BookmarkActionResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.post<BookmarkActionResponse>(
    "/watchlist/toggle",
    { market_id: marketId },
    { headers }
  );
  return response.data;
}

/**
 * Get user's watchlist
 */
export async function fetchWatchlist(
  getToken: () => Promise<string | null>
): Promise<WatchlistResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.get<WatchlistResponse>("/watchlist", {
    headers,
  });
  return response.data;
}

/**
 * Check if a market is bookmarked
 */
export async function checkIsBookmarked(
  marketId: string,
  getToken: () => Promise<string | null>
): Promise<BookmarkStatusResponse> {
  const headers = await getAuthHeaders(getToken);
  const response = await api.get<BookmarkStatusResponse>(
    `/watchlist/check/${encodeURIComponent(marketId)}`,
    { headers }
  );
  return response.data;
}
