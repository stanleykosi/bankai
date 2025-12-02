"use client";

import { useMemo, useState } from "react";
import { AlertCircle, Loader2, RefreshCw, X } from "lucide-react";

import { useOrders } from "@/hooks/useOrders";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { OrderRecord, OrderStatus } from "@/types";
import { cn } from "@/lib/utils";

const cancellableStatuses: OrderStatus[] = ["PENDING", "OPEN"];

const statusColorMap: Record<OrderStatus, string> = {
  PENDING: "bg-amber-500/10 text-amber-400",
  OPEN: "bg-blue-500/10 text-blue-400",
  FILLED: "bg-emerald-500/10 text-emerald-400",
  CANCELED: "bg-muted text-muted-foreground",
  FAILED: "bg-destructive/10 text-destructive",
};

type FilterValue = "all" | "active";

const formatNumber = (value: number) => {
  if (Number.isNaN(value)) return "--";
  return value.toFixed(2);
};

const formatDate = (input: string) => {
  if (!input) return "--";
  return new Date(input).toLocaleString();
};

export function OrdersPanel() {
  const {
    orders,
    isLoading,
    error,
    cancelOrder,
    cancelOrders,
    isCancelling,
    isBatchCancelling,
    refresh,
  } = useOrders(true);

  const [filter, setFilter] = useState<FilterValue>("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [cancelingId, setCancelingId] = useState<string | null>(null);

  const filteredOrders = useMemo(() => {
    if (filter === "active") {
      return orders.filter((order) =>
          cancellableStatuses.includes(order.status)
      );
    }
    return orders;
  }, [filter, orders]);

  const allVisibleSelected =
    filteredOrders.length > 0 &&
    filteredOrders.every((order) => selected.has(order.clob_order_id));

  const toggleSelect = (orderId: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(orderId)) {
        next.delete(orderId);
      } else {
        next.add(orderId);
      }
      return next;
    });
  };

  const toggleAll = () => {
    if (allVisibleSelected) {
      setSelected((prev) => {
        const next = new Set(prev);
        filteredOrders.forEach((order) => next.delete(order.clob_order_id));
        return next;
      });
    } else {
      setSelected((prev) => {
        const next = new Set(prev);
        filteredOrders.forEach((order) => next.add(order.clob_order_id));
        return next;
      });
    }
  };

  const handleCancelSelected = async () => {
    if (selected.size === 0) return;
    await cancelOrders(Array.from(selected));
    setSelected(new Set());
  };

  const handleCancelSingle = async (orderId: string) => {
    setCancelingId(orderId);
    try {
      await cancelOrder(orderId);
      setSelected((prev) => {
        const next = new Set(prev);
        next.delete(orderId);
        return next;
      });
    } finally {
      setCancelingId(null);
    }
  };

  const renderStatusBadge = (order: OrderRecord) => {
    const color =
      statusColorMap[order.status] ?? "bg-muted text-muted-foreground";
    const label = order.status_detail
      ? `${order.status} · ${order.status_detail}`
      : order.status;
    return (
      <span
        className={cn(
          "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-mono uppercase tracking-wide",
          color
        )}
      >
        {label}
      </span>
    );
  };

  return (
    <Card className="border-border bg-card/60 backdrop-blur-md shadow-xl">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-3">
        <CardTitle className="text-sm font-mono uppercase tracking-widest">
          Orders
        </CardTitle>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => refresh()}
            disabled={isLoading}
            className="h-8 w-8"
          >
            {isLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4" />
            )}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex gap-2">
            <Button
              size="sm"
              variant={filter === "all" ? "default" : "secondary"}
              onClick={() => setFilter("all")}
              className="font-mono text-xs"
            >
              All
            </Button>
            <Button
              size="sm"
              variant={filter === "active" ? "default" : "secondary"}
              onClick={() => setFilter("active")}
              className="font-mono text-xs"
            >
              Active
            </Button>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="destructive"
              size="sm"
              disabled={selected.size === 0 || isBatchCancelling}
              onClick={handleCancelSelected}
              className="font-mono text-xs"
            >
              {isBatchCancelling ? (
                <Loader2 className="mr-2 h-3 w-3 animate-spin" />
              ) : (
                <X className="mr-2 h-3 w-3" />
              )}
              Cancel Selected ({selected.size})
            </Button>
          </div>
        </div>

        {error && (
          <div className="flex items-center gap-2 rounded border border-destructive/30 bg-destructive/5 p-3 text-destructive">
            <AlertCircle className="h-4 w-4" />
            <p className="text-xs font-mono">{error.message}</p>
          </div>
        )}

        {filteredOrders.length === 0 && !isLoading ? (
          <p className="text-center text-xs font-mono text-muted-foreground">
            No orders yet.
          </p>
        ) : (
          <div className="overflow-x-auto rounded-md border border-border/60">
            <table className="w-full min-w-[600px] table-fixed text-sm">
              <thead className="bg-muted/20 text-[11px] font-mono uppercase tracking-wider text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 text-left">
                    <input
                      type="checkbox"
                      checked={allVisibleSelected}
                      onChange={toggleAll}
                      className="h-3 w-3 accent-primary"
                    />
                  </th>
                  <th className="px-3 py-2 text-left">Outcome</th>
                  <th className="px-3 py-2 text-left">Side</th>
                  <th className="px-3 py-2 text-left">Price</th>
                  <th className="px-3 py-2 text-left">Size</th>
                  <th className="px-3 py-2 text-left">Type</th>
                  <th className="px-3 py-2 text-left">Status</th>
                  <th className="px-3 py-2 text-left">Placed</th>
                  <th className="px-3 py-2 text-left">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/60">
                {filteredOrders.map((order) => {
                  const isSelected = selected.has(order.clob_order_id);
                  const cancellable = cancellableStatuses.includes(
                    order.status
                  );
                  return (
                    <tr key={order.id} className="text-[13px]">
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          className="h-3 w-3 accent-primary"
                          checked={isSelected}
                          onChange={() => toggleSelect(order.clob_order_id)}
                        />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">
                        <div className="flex flex-col">
                          <span className="font-semibold text-foreground">
                            {order.outcome || "—"}
                          </span>
                          <span className="text-[10px] text-muted-foreground">
                            {order.market_id ?? "Unlinked"}
                          </span>
                        </div>
                      </td>
                      <td className="px-3 py-2 font-mono">
                        <span
                          className={cn(
                            "rounded px-2 py-0.5 text-[10px] font-bold uppercase",
                            order.side === "BUY"
                              ? "bg-constructive/10 text-constructive"
                              : "bg-destructive/10 text-destructive"
                          )}
                        >
                          {order.side}
                        </span>
                      </td>
                      <td className="px-3 py-2 font-mono">
                        ${formatNumber(order.price)}
                      </td>
                      <td className="px-3 py-2 font-mono">
                        {formatNumber(order.size)}
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">
                        {order.order_type}
                      </td>
                      <td className="px-3 py-2">{renderStatusBadge(order)}</td>
                      <td className="px-3 py-2 font-mono text-[10px] text-muted-foreground">
                        {formatDate(order.created_at)}
                      </td>
                      <td className="px-3 py-2">
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={!cancellable || isCancelling}
                          onClick={() => handleCancelSingle(order.clob_order_id)}
                          className="text-[11px] font-mono"
                        >
                          {cancelingId === order.clob_order_id ? (
                            <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                          ) : (
                            <X className="mr-2 h-3 w-3" />
                          )}
                          Cancel
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}


