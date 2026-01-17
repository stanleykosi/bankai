/**
 * @description
 * API functions for user settings.
 *
 * @dependencies
 * - axios
 * - @/lib/api
 */

import { api } from "./api";
import type { UpdateSettingsPayload, UserSettings } from "@/types";

export async function fetchSettings(): Promise<UserSettings> {
  const response = await api.get<UserSettings>("/settings");
  return response.data;
}

export async function updateSettings(
  payload: UpdateSettingsPayload
): Promise<UserSettings> {
  const response = await api.patch<UserSettings>("/settings", payload);
  return response.data;
}

export async function resetSettings(): Promise<UserSettings> {
  const response = await api.post<UserSettings>("/settings/reset");
  return response.data;
}

