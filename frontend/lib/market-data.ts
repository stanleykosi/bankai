/**
 * @description
 * Shared data fetchers for market endpoints.
 */

import { api } from "@/lib/api";
import type { Market } from "@/types";
import type { SortOption } from "@/components/terminal/MarketFilters";

export type ActiveMarketParams = Partial<{
  category: string;
  tag: string;
  sort: SortOption;
}>;

export type MarketMetaResponse = {
  total: number;
  categories: { value: string; count: number }[];
  tags: { value: string; count: number }[];
};

export type MarketLaneResponse = {
  fresh: Market[];
  high_velocity: Market[];
  deep_liquidity: Market[];
};

export type MarketsPageResponse = {
  markets: Market[];
  total: number;
  nextOffset: number;
};

const buildQueryParams = (params: ActiveMarketParams, limit?: number, offset?: number) => {
  const requestParams: Record<string, string | number> = {};

  if (params.category) {
    requestParams.category = params.category;
  }
  if (params.tag) {
    requestParams.tag = params.tag;
  }
  if (params.sort && params.sort !== "all") {
    requestParams.sort = params.sort;
  }
  if (typeof limit === "number") {
    requestParams.limit = limit;
  }
  if (typeof offset === "number") {
    requestParams.offset = offset;
  }

  return requestParams;
};

export const fetchMarketMeta = async (): Promise<MarketMetaResponse | null> => {
  try {
    const { data } = await api.get<MarketMetaResponse>("/markets/meta");
    return data;
  } catch (error) {
    console.error("Failed to fetch market metadata:", error);
    return null;
  }
};

export const fetchMarketLanes = async (params: ActiveMarketParams = {}): Promise<MarketLaneResponse> => {
  try {
    const requestParams = buildQueryParams(params);
    const { data } = await api.get<MarketLaneResponse>("/markets/lanes", { params: requestParams });
    return data;
  } catch (error) {
    console.error("Failed to fetch market lanes:", error);
    return {
      fresh: [],
      high_velocity: [],
      deep_liquidity: [],
    };
  }
};

export const fetchMarketsPage = async (
  params: ActiveMarketParams,
  limit: number,
  offset: number
): Promise<MarketsPageResponse> => {
  try {
    const requestParams = buildQueryParams(params, limit, offset);
    const response = await api.get<Market[]>("/markets/active", { params: requestParams });
    const totalHeader = response.headers["x-total-count"];
    const total = typeof totalHeader === "string" ? parseInt(totalHeader, 10) : response.data.length;
    return {
      markets: response.data || [],
      total: Number.isNaN(total) ? response.data.length : total,
      nextOffset: offset + (response.data?.length ?? 0),
    };
  } catch (error) {
    console.error("Failed to fetch markets:", error);
    return {
      markets: [],
      total: 0,
      nextOffset: offset,
    };
  }
};

