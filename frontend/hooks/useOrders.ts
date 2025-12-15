"use client";

import { useCallback, useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { OpenOrder, Trade as ClobTrade } from "@polymarket/clob-client";
import { useAuth } from "@clerk/nextjs";

import { useClobClient } from "@/hooks/useClobClient";
import { useUserApiCredentials } from "@/hooks/useUserApiCredentials";
import { useWallet } from "@/hooks/useWallet";
import { api } from "@/lib/api";
import type { OrderRecord, OrderStatus } from "@/types";

const mapStatus = (raw?: string): OrderStatus => {
  const value = (raw || "").toLowerCase();
  if (value.includes("pending")) return "PENDING";
  if (value.includes("live") || value.includes("open")) return "OPEN";
  if (value.includes("matched") || value.includes("filled")) return "FILLED";
  if (value.includes("cancel")) return "CANCELED";
  return "FAILED";
};

const toNumber = (input?: string | number | null): number => {
  if (input === null || input === undefined) return 0;
  const num = typeof input === "number" ? input : parseFloat(input);
  return Number.isFinite(num) ? num : 0;
};

const mapOpenOrderToRecord = (order: OpenOrder): OrderRecord => {
  const created =
    typeof order.created_at === "number"
      ? new Date(order.created_at * 1000)
      : new Date();

  return {
    id: order.id,
    user_id: "",
    clob_order_id: order.id,
    market_id: order.market || null,
    side: order.side?.toUpperCase() === "SELL" ? "SELL" : "BUY",
    outcome: order.outcome || order.asset_id || "",
    outcome_token_id: order.asset_id || "",
    price: toNumber(order.price),
    size: toNumber(order.original_size || order.size_matched),
    order_type: order.order_type || "GTC",
    status: mapStatus(order.status),
    status_detail: order.status || null,
    order_hashes:
      order.associate_trades && order.associate_trades.length > 0
        ? order.associate_trades
        : null,
    source: "UNKNOWN",
    maker_address: order.maker_address || order.owner || "",
    error_msg: null,
    tx_hash: null,
    created_at: created.toISOString(),
    updated_at: created.toISOString(),
  };
};

const mapTradeToRecord = (trade: ClobTrade): OrderRecord => {
  const created =
    trade.match_time && !Number.isNaN(Date.parse(trade.match_time))
      ? new Date(trade.match_time)
      : new Date();
  const updated =
    trade.last_update && !Number.isNaN(Date.parse(trade.last_update))
      ? new Date(trade.last_update)
      : created;

  return {
    id: trade.id,
    user_id: "",
    clob_order_id: trade.id,
    market_id: trade.market || null,
    side: trade.side?.toUpperCase() === "SELL" ? "SELL" : "BUY",
    outcome: trade.outcome || trade.asset_id || "",
    outcome_token_id: trade.asset_id || "",
    price: toNumber(trade.price),
    size: toNumber(trade.size),
    order_type: "TRADE",
    status: mapStatus(trade.status),
    status_detail: trade.status || null,
    order_hashes: null,
    source: "UNKNOWN",
    maker_address: trade.maker_address || trade.owner || "",
    error_msg: null,
    tx_hash: trade.transaction_hash || null,
    created_at: created.toISOString(),
    updated_at: updated.toISOString(),
  };
};

export function useOrders(enabled = true) {
  const queryClient = useQueryClient();
  const { user, isAuthenticated } = useWallet();
  const { isLoaded, isSignedIn, getToken } = useAuth();
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
    // allow hook to re-render with new credentials
    await new Promise((resolve) => setTimeout(resolve, 200));
    if (!clobClientRef.current) {
      throw new Error("Trading client not ready. Connect wallet to fetch orders.");
    }
    return clobClientRef.current;
  };

  const syncOrders = useCallback(
    async (orders: OrderRecord[]) => {
      if (!orders.length) return;
      try {
        const token = await getToken();
        if (!token) return;
        await api.post(
          "/trade/sync",
          {
            orders: orders.map((o) => ({
              orderId: o.clob_order_id,
              marketId: o.market_id ?? "",
              outcome: o.outcome ?? "",
              outcomeTokenId: o.outcome_token_id ?? "",
              side: o.side,
              price: o.price,
              size: o.size,
              orderType: o.order_type,
              status: o.status,
              statusDetail: o.status_detail ?? "",
              orderHashes: o.order_hashes ?? [],
              source: o.source ?? "UNKNOWN",
              makerAddress: o.maker_address ?? "",
              createdAt: o.created_at,
              updatedAt: o.updated_at,
            })),
          },
          { headers: { Authorization: `Bearer ${token}` } }
        );
      } catch (err) {
        console.error("Order sync failed", err);
      }
    },
    [getToken]
  );

  const fetchOrders = async (): Promise<OrderRecord[]> => {
    const client = await ensureClient();
    const [openOrders, trades] = await Promise.all([
      client.getOpenOrders(),
      client.getTrades(),
    ]);

    const openRecords = (openOrders || []).map(mapOpenOrderToRecord);
    const tradeRecords = (trades || []).map(mapTradeToRecord);

    // Merge by clob_order_id with preference for most recent updated_at
    const merged = new Map<string, OrderRecord>();
    [...openRecords, ...tradeRecords].forEach((rec) => {
      const existing = merged.get(rec.clob_order_id);
      if (!existing) {
        merged.set(rec.clob_order_id, rec);
        return;
      }
      const existingTime = Date.parse(existing.updated_at);
      const newTime = Date.parse(rec.updated_at);
      merged.set(
        rec.clob_order_id,
        Number.isFinite(newTime) && newTime > existingTime ? rec : existing
      );
    });

    const records = Array.from(merged.values());
    // Fire-and-forget sync to backend for persistence
    void syncOrders(records);
    return records;
  };

  const ordersQuery = useQuery({
    queryKey: ["orders"],
    queryFn: fetchOrders,
    enabled:
      enabled &&
      isAuthenticated &&
      isLoaded &&
      isSignedIn &&
      !credsLoading &&
      Boolean(clobClientRef.current),
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
