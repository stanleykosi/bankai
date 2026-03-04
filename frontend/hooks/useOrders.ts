"use client";

import { useCallback, useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useClobClient } from "@/hooks/useClobClient";
import { useUserApiCredentials } from "@/hooks/useUserApiCredentials";
import { useWallet } from "@/hooks/useWallet";
import { api } from "@/lib/api";
import { reconcileOrderLifecycle } from "@/lib/order-lifecycle";
import type { OrderRecord } from "@/types";

export function useOrders(enabled = true) {
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

    // Allow the hook to re-render with fresh credentials and client wiring.
    for (let attempt = 0; attempt < 20; attempt++) {
      if (clobClientRef.current) {
        return clobClientRef.current;
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }

    throw new Error("Trading client not ready. Connect wallet to fetch orders.");
  };

  const syncOrders = useCallback(async (orders: OrderRecord[]) => {
    if (!orders.length) return;
    try {
      await api.post(
        "/trade/sync",
        {
          orders: orders.map((o) => ({
            orderId: o.clob_order_id,
            marketId: o.market_id ?? "",
            outcome: o.outcome ?? "",
            outcomeTokenId: o.outcome_token_id ?? "",
            makerAddress: o.maker_address ?? "",
            side: o.side,
            price: o.price,
            size: o.size,
            status: o.status_detail ?? o.status,
            createdAt: o.created_at,
            updatedAt: o.updated_at,
          })),
        },
        { withCredentials: true }
      );
    } catch (err) {
      console.error("Order sync failed", err);
    }
  }, []);

  const fetchOrders = async (): Promise<OrderRecord[]> => {
    const client = await ensureClient();
    const [openOrders, trades] = await Promise.all([
      client.getOpenOrders(),
      client.getTrades(undefined, true),
    ]);
    const records = reconcileOrderLifecycle(openOrders || [], trades || []);
    void syncOrders(records);
    return records;
  };

  const ordersQuery = useQuery({
    queryKey: ["orders", user?.id || user?.eoa_address || "anon"],
    queryFn: fetchOrders,
    enabled:
      enabled &&
      isAuthenticated &&
      !isWalletLoading &&
      !credsLoading &&
      Boolean(clobClient),
    staleTime: 5_000,
    refetchInterval: 15_000,
    retry: 2,
  });

  const cancelOrderMutation = useMutation({
    mutationFn: async (orderId: string) => {
      const client = await ensureClient();
      return client.cancelOrder({ orderID: orderId });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
    },
  });

  const cancelOrdersMutation = useMutation({
    mutationFn: async (orderIds: string[]) => {
      const client = await ensureClient();
      await Promise.allSettled(
        orderIds.map((id) => client.cancelOrder({ orderID: id }))
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
    },
  });

  return {
    orders: ordersQuery.data ?? [],
    total: ordersQuery.data?.length ?? 0,
    limit: ordersQuery.data?.length ?? 0,
    offset: 0,
    isLoading: ordersQuery.isLoading,
    isFetching: ordersQuery.isFetching,
    error: ordersQuery.error as Error | null,
    refresh: () => queryClient.invalidateQueries({ queryKey: ["orders"] }),
    cancelOrder: cancelOrderMutation.mutateAsync,
    cancelOrders: cancelOrdersMutation.mutateAsync,
    isCancelling: cancelOrderMutation.isPending,
    isBatchCancelling: cancelOrdersMutation.isPending,
  };
}
