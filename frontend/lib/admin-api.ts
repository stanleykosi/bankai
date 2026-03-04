/**
 * @description
 * API functions for admin moderation and operations.
 * Endpoints require authenticated wallet session and backend allowlist access.
 */

import { api } from "./api";

export interface BlockedAccount {
  user_id?: string;
  wallet?: string;
  blocked: boolean;
  reason?: string;
  blocked_by?: string;
  blocked_at?: string;
  expires_at?: string;
}

export interface ModerationAction {
  id: string;
  action: string;
  actor_wallet: string;
  user_id?: string;
  wallet?: string;
  reason?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface ListBlockedAccountsResponse {
  accounts: BlockedAccount[];
  count: number;
}

export interface ActionLogResponse {
  actions: ModerationAction[];
  count: number;
}

export interface BlockAccountPayload {
  user_id?: string;
  wallet?: string;
  reason?: string;
  duration_minutes?: number;
}

export interface UnblockAccountPayload {
  user_id?: string;
  wallet?: string;
}

export interface ModerateMarketPayload {
  restricted?: boolean;
  featured?: boolean;
  archived?: boolean;
}

export interface BroadcastPayload {
  title: string;
  message: string;
  data?: Record<string, unknown>;
  async?: boolean;
}

export interface MutationSuccess {
  success: boolean;
}

export interface BroadcastResponse {
  queued: boolean;
  job_id?: string;
  delivered?: number;
}

export async function fetchBlockedAccounts(): Promise<ListBlockedAccountsResponse> {
  const response = await api.get<ListBlockedAccountsResponse>("/admin/moderation/blocks");
  return response.data;
}

export async function fetchActionLog(limit = 100): Promise<ActionLogResponse> {
  const response = await api.get<ActionLogResponse>("/admin/moderation/actions", {
    params: { limit },
  });
  return response.data;
}

export async function blockAccount(payload: BlockAccountPayload): Promise<MutationSuccess> {
  const response = await api.post<MutationSuccess>("/admin/moderation/block", payload);
  return response.data;
}

export async function unblockAccount(payload: UnblockAccountPayload): Promise<MutationSuccess> {
  const response = await api.post<MutationSuccess>("/admin/moderation/unblock", payload);
  return response.data;
}

export async function moderateMarket(
  conditionId: string,
  payload: ModerateMarketPayload
): Promise<{ success: boolean; condition_id: string }> {
  const response = await api.patch<{ success: boolean; condition_id: string }>(
    `/admin/markets/${encodeURIComponent(conditionId)}`,
    payload
  );
  return response.data;
}

export async function broadcastSystemNotification(
  payload: BroadcastPayload
): Promise<BroadcastResponse> {
  const response = await api.post<BroadcastResponse>("/admin/notifications/broadcast", payload);
  return response.data;
}
