"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Trade as ClobTrade } from "@polymarket/clob-client";

import { useClobClient } from "@/hooks/useClobClient";
import { useUserApiCredentials } from "@/hooks/useUserApiCredentials";
import { useWallet } from "@/hooks/useWallet";
import type { FillRecord } from "@/types";

const toNumber = (input?: string | number | null): number => {
  if (input === null || input === undefined) return 0;
  const num = typeof input === "number" ? input : parseFloat(input);
  return Number.isFinite(num) ? num : 0;
};

const toTimestamp = (input?: string | null): string => {
  if (!input) return new Date().toISOString();
  const parsed = Date.parse(input);
  if (Number.isNaN(parsed)) {
    return new Date().toISOString();
  }
  return new Date(parsed).toISOString();
};

const mapTradeToFill = (trade: ClobTrade): FillRecord => ({
  id: trade.id,
  market_id: trade.market || null,
  outcome: trade.outcome || trade.asset_id || "",
  side: trade.side?.toUpperCase() === "SELL" ? "SELL" : "BUY",
  price: toNumber(trade.price),
  size: toNumber(trade.size),
  matched_at: toTimestamp(trade.match_time || trade.last_update || null),
});

export function useFills(enabled = true) {
  const queryClient = useQueryClient();
  const { user, isAuthenticated, isLoading: isWalletLoading } = useWallet();
  const { credentials, getCredentials, isLoading: credsLoading } = useUserApiCredentials();

  const { clobClient } = useClobClient({
    credentials,
    vaultAddress: user?.vault_address ?? null,
    walletType: user?.wallet_type ?? null,
  });

  const clobClientRef = useRef(clobClient);
  useEffect(() => {
    clobClientRef.current = clobClient;
  }, [clobClient]);

  const ensureClient = async () => {
    if (clobClientRef.current) return clobClientRef.current;
    if (!credentials) {
      await getCredentials();
    }

    for (let attempt = 0; attempt < 20; attempt++) {
      if (clobClientRef.current) {
        return clobClientRef.current;
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }

    throw new Error("Trading client not ready. Connect wallet to fetch fills.");
  };

  const fetchFills = async (): Promise<FillRecord[]> => {
    const client = await ensureClient();
    const trades = await client.getTrades(undefined, true);
    const fills = (trades || []).map(mapTradeToFill);
    return fills.sort(
      (a, b) => Date.parse(b.matched_at) - Date.parse(a.matched_at)
    );
  };

  const fillsQuery = useQuery({
    queryKey: ["fills", user?.id || user?.eoa_address || "anon"],
    queryFn: fetchFills,
    enabled:
      enabled &&
      isAuthenticated &&
      !isWalletLoading &&
      !credsLoading &&
      Boolean(clobClient),
    staleTime: 15_000,
    refetchInterval: 20_000,
    retry: 2,
  });

  const fills = useMemo(() => fillsQuery.data ?? [], [fillsQuery.data]);

  return {
    fills,
    total: fills.length,
    isLoading: fillsQuery.isLoading,
    isFetching: fillsQuery.isFetching,
    error: fillsQuery.error as Error | null,
    refresh: () => queryClient.invalidateQueries({ queryKey: ["fills"] }),
  };
}
