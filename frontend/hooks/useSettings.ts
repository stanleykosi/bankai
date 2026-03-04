/**
 * @description
 * React Query hooks for user settings.
 *
 * @dependencies
 * - @tanstack/react-query
 * - @/lib/settings-api
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { fetchSettings, resetSettings, updateSettings } from "@/lib/settings-api";
import type { UpdateSettingsPayload, UserSettings } from "@/types";
import { useWallet } from "@/hooks/useWallet";
import { useTerminalStore } from "@/lib/store";

export function useSettings() {
  const { isAuthenticated, isLoading, hasSession } = useWallet();
  const setSettings = useTerminalStore((state) => state.setSettings);

  useEffect(() => {
    if (!isLoading && !isAuthenticated && !hasSession) {
      setSettings(null);
    }
  }, [hasSession, isAuthenticated, isLoading, setSettings]);

  const query = useQuery<UserSettings, Error>({
    queryKey: ["settings"],
    queryFn: fetchSettings,
    enabled: !isLoading && (isAuthenticated || hasSession),
    staleTime: 5 * 60_000,
  });

  useEffect(() => {
    if (query.data) {
      setSettings(query.data);
      return;
    }
    if (query.isError) {
      setSettings(null);
    }
  }, [query.data, query.isError, setSettings]);

  return query;
}

export function useUpdateSettingsMutation() {
  const queryClient = useQueryClient();
  const setSettings = useTerminalStore((state) => state.setSettings);

  return useMutation({
    mutationFn: (payload: UpdateSettingsPayload) => updateSettings(payload),
    onSuccess: (data) => {
      setSettings(data);
      queryClient.setQueryData(["settings"], data);
    },
  });
}

export function useResetSettingsMutation() {
  const queryClient = useQueryClient();
  const setSettings = useTerminalStore((state) => state.setSettings);

  return useMutation({
    mutationFn: () => resetSettings(),
    onSuccess: (data) => {
      setSettings(data);
      queryClient.setQueryData(["settings"], data);
    },
  });
}

export function useSettingsManager() {
  const settingsQuery = useSettings();
  const updateMutation = useUpdateSettingsMutation();
  const resetMutation = useResetSettingsMutation();
  const settings = (settingsQuery.data as UserSettings | undefined) ?? null;

  return {
    settings,
    isLoading: settingsQuery.isLoading,
    error: settingsQuery.error,
    refetch: settingsQuery.refetch,
    update: updateMutation.mutateAsync,
    reset: resetMutation.mutateAsync,
    isUpdating: updateMutation.isPending,
    isResetting: resetMutation.isPending,
  };
}
