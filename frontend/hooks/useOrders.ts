"use client";

import { useAuth } from "@clerk/nextjs";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";
import type { OrderHistoryResponse } from "@/types";

export function useOrders(enabled = true) {
  const { isLoaded, isSignedIn, getToken } = useAuth();
  const queryClient = useQueryClient();

  const fetchOrders = async (): Promise<OrderHistoryResponse> => {
    const token = await getToken();
    if (!token) {
      throw new Error("Wallet authentication required");
    }
    const { data } = await api.get<OrderHistoryResponse>("/trade/orders", {
      headers: { Authorization: `Bearer ${token}` },
    });
    return data;
  };

  const ordersQuery = useQuery({
    queryKey: ["orders"],
    queryFn: fetchOrders,
    enabled: enabled && isLoaded && Boolean(isSignedIn),
  });

  const cancelOrderMutation = useMutation({
    mutationFn: async (orderId: string) => {
      const token = await getToken();
      if (!token) throw new Error("Wallet authentication required");
      await api.post(
        "/trade/cancel",
        { orderId },
        { headers: { Authorization: `Bearer ${token}` } }
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
    },
  });

  const cancelOrdersMutation = useMutation({
    mutationFn: async (orderIds: string[]) => {
      const token = await getToken();
      if (!token) throw new Error("Wallet authentication required");
      await api.post(
        "/trade/cancel/batch",
        { orderIds },
        { headers: { Authorization: `Bearer ${token}` } }
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["orders"] });
    },
  });

  return {
    orders: ordersQuery.data?.data ?? [],
    total: ordersQuery.data?.total ?? 0,
    limit: ordersQuery.data?.limit ?? 0,
    offset: ordersQuery.data?.offset ?? 0,
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


