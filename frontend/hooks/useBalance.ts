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
import { api } from "@/lib/api";

export interface BalanceResponse {
  balance: string;
  balance_formatted: string;
  vault_address: string;
  token: string;
}

export function useBalance() {
  const { getToken, isSignedIn } = useAuth();

  return useQuery({
    queryKey: ["balance"],
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
    enabled: !!isSignedIn, // Only fetch when signed in
    // Reduce chatter: no interval; refresh on demand via queryClient.invalidateQueries(["balance"])
    refetchInterval: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchOnMount: true,
    staleTime: 60_000, // 1 minute
  });
}
