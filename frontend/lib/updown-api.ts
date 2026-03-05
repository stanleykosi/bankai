import { API_BASE_URL, api } from "@/lib/api";
import type {
  DecisionLog,
  PerformanceSummary,
  Recommendation,
  UpDownMarket,
  UpDownSignal,
} from "@/types";

export type UpDownQuery = {
  asset?: string;
  window?: "5m" | "15m" | "1h" | "4h";
};

export type DecisionPayload = {
  slug: string;
  recommendation_id?: string;
  action: "accepted" | "rejected" | "manual_override" | "placed";
  chosen_side?: string;
  override_price?: number;
  override_size?: number;
  notes?: string;
};

export const fetchUpDownMarkets = async (
  params: UpDownQuery = {}
): Promise<UpDownMarket[]> => {
  const { data } = await api.get<UpDownMarket[]>("/updown/markets", { params });
  return data ?? [];
};

export const fetchUpDownMarket = async (slug: string): Promise<UpDownMarket> => {
  const { data } = await api.get<UpDownMarket>(
    `/updown/market/${encodeURIComponent(slug)}`
  );
  return data;
};

export const fetchUpDownSignal = async (slug: string): Promise<UpDownSignal> => {
  const { data } = await api.get<UpDownSignal>(
    `/updown/signal/${encodeURIComponent(slug)}`
  );
  return data;
};

export const fetchUpDownRecommendations = async (params: {
  asset?: string;
  limit?: number;
} = {}): Promise<Recommendation[]> => {
  const { data } = await api.get<Recommendation[]>("/updown/recommendations", {
    params,
  });
  return data ?? [];
};

export const logUpDownDecision = async (
  payload: DecisionPayload
): Promise<DecisionLog> => {
  const { data } = await api.post<DecisionLog>("/updown/decisions", payload);
  return data;
};

export const fetchUpDownPerformance = async (params: {
  from?: string;
  to?: string;
} = {}): Promise<PerformanceSummary> => {
  const { data } = await api.get<PerformanceSummary>("/updown/performance", {
    params,
  });
  return data;
};

export const createUpDownEventSource = (): EventSource => {
  return new EventSource(`${API_BASE_URL}/api/v1/updown/stream`);
};

