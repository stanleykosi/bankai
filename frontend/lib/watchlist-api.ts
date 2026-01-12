/**
 * @description
 * API functions for watchlist/bookmark features.
 * Requires authentication via wallet session cookie.
 *
 * @dependencies
 * - axios
 * - @/lib/api
 */

import { api } from "./api";
import type {
  WatchlistResponse,
  BookmarkStatusResponse,
  BookmarkActionResponse,
} from "@/types";

/**
 * Add market to watchlist
 */
export async function bookmarkMarket(
  marketId: string
): Promise<BookmarkActionResponse> {
  const response = await api.post<BookmarkActionResponse>(
    "/watchlist/bookmark",
    { market_id: marketId },
    { withCredentials: true }
  );
  return response.data;
}

/**
 * Remove market from watchlist
 */
export async function removeBookmark(
  marketId: string
): Promise<BookmarkActionResponse> {
  const response = await api.delete<BookmarkActionResponse>(
    `/watchlist/${encodeURIComponent(marketId)}`,
    { withCredentials: true }
  );
  return response.data;
}

/**
 * Toggle bookmark status
 */
export async function toggleBookmark(
  marketId: string
): Promise<BookmarkActionResponse> {
  const response = await api.post<BookmarkActionResponse>(
    "/watchlist/toggle",
    { market_id: marketId },
    { withCredentials: true }
  );
  return response.data;
}

/**
 * Get user's watchlist
 */
export async function fetchWatchlist(
): Promise<WatchlistResponse> {
  const response = await api.get<WatchlistResponse>("/watchlist");
  return response.data;
}

/**
 * Check if a market is bookmarked
 */
export async function checkIsBookmarked(
  marketId: string
): Promise<BookmarkStatusResponse> {
  const response = await api.get<BookmarkStatusResponse>(
    `/watchlist/check/${encodeURIComponent(marketId)}`
  );
  return response.data;
}
