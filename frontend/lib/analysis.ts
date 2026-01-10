import api from "./api";
import type { AIResponse, AlphaSnapshot, SmartMoneyResponse, WhaleEvent } from "@/types/analysis";

export async function fetchSmartMoney(windowMinutes = 60): Promise<SmartMoneyResponse> {
  const resp = await api.get<SmartMoneyResponse>(`/analysis/smart-money`, {
    params: { window: windowMinutes },
  });
  return resp.data;
}

export async function fetchAIPicks(windowMinutes = 60): Promise<AIResponse> {
  const resp = await api.get<AIResponse>(`/analysis/ai-picks`, {
    params: { window: windowMinutes },
  });
  return resp.data;
}

export async function fetchAnalysisSnapshot(windowMinutes = 60): Promise<AlphaSnapshot> {
  const resp = await api.get<AlphaSnapshot>(`/analysis/snapshot`, {
    params: { window: windowMinutes },
  });
  return resp.data;
}

export async function fetchRecentWhales(limit = 15): Promise<WhaleEvent[]> {
  const resp = await api.get<WhaleEvent[]>(`/analysis/whales/recent`, {
    params: { limit },
  });
  return resp.data;
}
