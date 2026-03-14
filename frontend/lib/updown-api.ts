import { API_BASE_URL, api } from "@/lib/api";
import type {
  DecisionLog,
  LLMTradePacket,
  PerformanceSummary,
  Recommendation,
  UpDownLLMHealth,
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

export type LLMGeneratePayload = {
  slug: string;
  force_refresh?: boolean;
};

const sanitizeUpDownSlug = (value: string): string => {
  const trimmed = value.trim();
  if (
    trimmed === "" ||
    trimmed === "null" ||
    trimmed === "undefined"
  ) {
    throw new Error("invalid up/down slug");
  }
  return trimmed;
};

export const fetchUpDownMarkets = async (
  params: UpDownQuery = {}
): Promise<UpDownMarket[]> => {
  const { data } = await api.get<UpDownMarket[]>("/updown/markets", { params });
  return data ?? [];
};

export const fetchUpDownMarket = async (slug: string): Promise<UpDownMarket> => {
  const normalizedSlug = sanitizeUpDownSlug(slug);
  const { data } = await api.get<UpDownMarket>(
    `/updown/market/${encodeURIComponent(normalizedSlug)}`
  );
  return data;
};

export const fetchUpDownSignal = async (slug: string): Promise<UpDownSignal> => {
  const normalizedSlug = sanitizeUpDownSlug(slug);
  const { data } = await api.get<UpDownSignal>(
    `/updown/signal/${encodeURIComponent(normalizedSlug)}`
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

export const generateUpDownLLMPacket = async (
  payload: LLMGeneratePayload
): Promise<LLMTradePacket> => {
  const normalizedSlug = sanitizeUpDownSlug(payload.slug);
  const { data } = await api.post<LLMTradePacket>("/updown/llm/generate", {
    slug: normalizedSlug,
    force_refresh: Boolean(payload.force_refresh),
  });
  return data;
};

export const fetchUpDownLLMPacket = async (
  slug: string
): Promise<LLMTradePacket | null> => {
  const normalizedSlug = sanitizeUpDownSlug(slug);
  try {
    const { data } = await api.get<LLMTradePacket>(
      `/updown/llm/packet/${encodeURIComponent(normalizedSlug)}`
    );
    return data;
  } catch (error: unknown) {
    if (
      typeof error === "object" &&
      error !== null &&
      "response" in error &&
      typeof (error as { response?: { status?: number } }).response?.status === "number" &&
      (error as { response?: { status?: number } }).response?.status === 404
    ) {
      return null;
    }
    throw error;
  }
};

export const fetchUpDownLLMHealth = async (): Promise<UpDownLLMHealth> => {
  const { data } = await api.get<UpDownLLMHealth>("/updown/llm/health");
  return data;
};
