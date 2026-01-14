"use client";

import { useMemo, useState } from "react";
import { AlertCircle, ChevronDown, ChevronUp, Loader2, X } from "lucide-react";

import { useOrders } from "@/hooks/useOrders";
import { useWallet } from "@/hooks/useWallet";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { OrderStatus } from "@/types";

const cancellableStatuses: OrderStatus[] = ["PENDING", "OPEN"];

const formatNumber = (value: number) => {
  if (Number.isNaN(value)) return "--";
  return value.toFixed(2);
};

export function PersistentOrdersWidget() {
  const { isAuthenticated } = useWallet();
  const {
    orders,
    isLoading,
    error,
    cancelOrder,
    cancelOrders,
    isCancelling,
    isBatchCancelling,
  } = useOrders(true);

  const [expanded, setExpanded] = useState(false);
  const [cancelingId, setCancelingId] = useState<string | null>(null);

  const activeOrders = useMemo(
    () => orders.filter((order) => cancellableStatuses.includes(order.status)),
    [orders]
  );

  const handleCancelAll = async () => {
    if (activeOrders.length === 0) return;
    await cancelOrders(activeOrders.map((order) => order.clob_order_id));
  };

  const handleCancelSingle = async (orderId: string) => {
    setCancelingId(orderId);
    try {
      await cancelOrder(orderId);
    } finally {
      setCancelingId(null);
    }
  };

  if (!isAuthenticated) {
    return null;
  }

  return (
    <div className="fixed bottom-0 left-0 right-0 z-40 pointer-events-none">
      <div className="mx-auto w-full max-w-6xl px-4 pb-4 pointer-events-auto">
        <div
          className={cn(
            "overflow-hidden rounded-t-xl border border-border/60 bg-card/80 shadow-2xl backdrop-blur",
            !expanded && "rounded-b-xl"
          )}
        >
          <button
            type="button"
            onClick={() => setExpanded((prev) => !prev)}
            className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left"
            aria-expanded={expanded}
          >
            <div className="flex items-center gap-3">
              <div className="flex h-8 w-8 items-center justify-center rounded-full border border-border/60 bg-muted/40">
                {isLoading ? (
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                ) : (
                  <span className="text-xs font-mono text-muted-foreground">
                    {activeOrders.length}
                  </span>
                )}
              </div>
              <div>
                <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground">
                  Open Orders
                </p>
                <p className="text-sm font-medium text-foreground">
                  {activeOrders.length === 0
                    ? "No active orders"
                    : `${activeOrders.length} active across markets`}
                </p>
              </div>
            </div>
            {expanded ? (
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
            ) : (
              <ChevronUp className="h-4 w-4 text-muted-foreground" />
            )}
          </button>

          {expanded && (
            <div className="space-y-3 border-t border-border/60 px-4 py-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground">
                  Active Orders
                </p>
                <Button
                  variant="destructive"
                  size="sm"
                  disabled={activeOrders.length === 0 || isBatchCancelling}
                  onClick={handleCancelAll}
                  className="font-mono text-xs"
                >
                  {isBatchCancelling ? (
                    <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                  ) : (
                    <X className="mr-2 h-3 w-3" />
                  )}
                  Cancel All
                </Button>
              </div>

              {error && (
                <div className="flex items-center gap-2 rounded border border-destructive/30 bg-destructive/5 p-3 text-destructive">
                  <AlertCircle className="h-4 w-4" />
                  <p className="text-xs font-mono">{error.message}</p>
                </div>
              )}

              {activeOrders.length === 0 && !isLoading ? (
                <p className="text-center text-xs font-mono text-muted-foreground">
                  No active orders yet.
                </p>
              ) : (
                <div className="max-h-[260px] overflow-x-auto rounded-md border border-border/60">
                  <table className="w-full min-w-[600px] table-fixed text-sm">
                    <thead className="bg-muted/20 text-[11px] font-mono uppercase tracking-wider text-muted-foreground">
                      <tr>
                        <th className="px-3 py-2 text-left">Outcome</th>
                        <th className="px-3 py-2 text-left">Side</th>
                        <th className="px-3 py-2 text-left">Price</th>
                        <th className="px-3 py-2 text-left">Size</th>
                        <th className="px-3 py-2 text-left">Market</th>
                        <th className="px-3 py-2 text-left">Action</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border/60">
                      {activeOrders.map((order) => {
                        const cancellable = cancellableStatuses.includes(
                          order.status
                        );
                        return (
                          <tr key={order.id} className="text-[13px]">
                            <td className="px-3 py-2 font-mono text-xs">
                              {order.outcome || "—"}
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
                            <td className="px-3 py-2 font-mono text-[10px] text-muted-foreground">
                              {order.market_id ?? "Unlinked"}
                            </td>
                            <td className="px-3 py-2">
                              <Button
                                size="sm"
                                variant="outline"
                                disabled={!cancellable || isCancelling}
                                onClick={() =>
                                  handleCancelSingle(order.clob_order_id)
                                }
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
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
