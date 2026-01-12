/**
 * @description
 * Custom hook to fetch and manage USDC balance for the user's vault address.
 * Uses React Query for caching and automatic refetching.
 *
 * @dependencies
 * - @tanstack/react-query
 */

"use client";

import { useQuery } from "@tanstack/react-query";
import { useWallet } from "@/hooks/useWallet";
import { api } from "@/lib/api";

export interface BalanceResponse {
  balance: string;
  balance_formatted: string;
  vault_address: string;
  token: string;
}

export function useBalance() {
  const { vaultAddress, isAuthenticated } = useWallet();

  return useQuery({
    queryKey: ["balance", vaultAddress],
    queryFn: async (): Promise<BalanceResponse | null> => {
      try {
        const { data } = await api.get<BalanceResponse>("/wallet/balance", {
          withCredentials: true,
        });
        return data;
      } catch (error: any) {
        // If vault doesn't exist yet, return null (not an error)
        if (error.response?.status === 400 || error.response?.status === 404) {
          return null;
        }
        throw error;
      }
    },
    enabled: isAuthenticated && !!vaultAddress, // Only fetch when signed in and wallet exists
    // Reduce chatter: no interval; refresh on demand via queryClient.invalidateQueries(["balance"])
    refetchInterval: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchOnMount: false,
    retry: false,
    staleTime: 5 * 60_000, // 5 minutes
    gcTime: 30 * 60_000, // 30 minutes
  });
}
