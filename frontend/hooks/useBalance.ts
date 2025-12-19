/**
 * @description
 * Custom hook to fetch and manage USDC balance for the user's vault address.
 * Uses React Query for caching and automatic refetching.
 *
 * @dependencies
 * - @tanstack/react-query
 * - @clerk/nextjs
 */

"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@clerk/nextjs";
import { useWallet } from "@/hooks/useWallet";
import { api } from "@/lib/api";

export interface BalanceResponse {
  balance: string;
  balance_formatted: string;
  vault_address: string;
  token: string;
}

export function useBalance() {
  const { getToken, isSignedIn } = useAuth();
  const { vaultAddress } = useWallet();

  return useQuery({
    queryKey: ["balance", vaultAddress],
    queryFn: async (): Promise<BalanceResponse | null> => {
      const token = await getToken();
      if (!token) {
        return null;
      }

      try {
        const { data } = await api.get<BalanceResponse>("/wallet/balance", {
          headers: {
            Authorization: `Bearer ${token}`,
          },
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
    enabled: !!isSignedIn && !!vaultAddress, // Only fetch when signed in and wallet exists
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
