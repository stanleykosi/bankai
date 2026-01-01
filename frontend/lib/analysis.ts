import api from "./api";
import type { AIResponse, SmartMoneyResponse } from "@/types/analysis";

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
