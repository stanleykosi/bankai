/**
 * @description
 * Trade Form Component.
 * The primary interface for executing trades (Buy/Sell) on a specific market.
 * 
 * Features:
 * - Switch between Buy/Sell.
 * - Input Limit Price and Shares amount.
 * - Real-time total calculation.
 * - Wallet connection check.
 * - EIP-712 Signing & Order Submission.
 * 
 * @dependencies
 * - wagmi: Signing
 * - @clerk/nextjs: Auth
 * - api: Backend communication
 */

"use client";

import React, { useState, useMemo, useEffect } from "react";
import Link from "next/link";
import { useAuth } from "@clerk/nextjs";
import { useSignTypedData, useAccount, useSwitchChain } from "wagmi";
import { polygon } from "viem/chains";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Loader2,
  AlertCircle,
  Wallet,
  ListPlus,
  Send,
  Trash2,
} from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { Input } from "@/components/ui/input";
import { useWallet } from "@/hooks/useWallet";
import { useBalance } from "@/hooks/useBalance";
import { buildOrderTypedData } from "@/lib/signing";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { fetchDepthEstimate } from "@/lib/market-data";
import type { DepthEstimate, Market } from "@/types";

interface TradeFormProps {
  market: Market;
}

type OrderSide = "BUY" | "SELL";
type OrderTypeValue = "GTC" | "GTD" | "FOK" | "FAK";

const OUTCOME_FALLBACKS = ["Yes", "No"];
const MIN_GTD_BUFFER_SECONDS = 90; // Polymarket enforces ~60s security threshold, keep 90s for safety.

type OutcomeOption = {
  label: string;
  tokenId: string | null;
  lastPrice?: number;
  bestBid?: number;
  bestAsk?: number;
  accent: "constructive" | "destructive";
};

type OrderTypeOption = {
  value: OrderTypeValue;
  label: string;
  description: string;
};

const ORDER_TYPE_OPTIONS: OrderTypeOption[] = [
  {
    value: "GTC",
    label: "GTC",
    description: "Rest on book until cancelled.",
  },
  {
    value: "GTD",
    label: "GTD",
    description: "Set an explicit expiration timestamp.",
  },
  {
    value: "FOK",
    label: "FOK",
    description: "Fill the entire size immediately or cancel.",
  },
  {
    value: "FAK",
    label: "FAK",
    description: "Fill what you can immediately, cancel remainder.",
  },
];

const toDateTimeLocalValue = (date: Date) => {
  const tzOffsetMs = date.getTimezoneOffset() * 60 * 1000;
  const local = new Date(date.getTime() - tzOffsetMs);
  return local.toISOString().slice(0, 16);
};

type SerializedOrderPayload = {
  salt: string;
  maker: `0x${string}`;
  signer: `0x${string}`;
  taker: `0x${string}`;
  tokenId: string;
  makerAmount: string;
  takerAmount: string;
  expiration: string;
  nonce: string;
  feeRateBps: string;
  side: OrderSide;
  signatureType: number;
  signature: string;
};

type PreparedBatchOrder = {
  id: string;
  order: SerializedOrderPayload;
  orderType: OrderTypeValue;
  summary: {
    outcomeLabel: string;
    side: OrderSide;
    price: number;
    shares: number;
  };
};

const formatPriceLabel = (value?: number) => {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "--";
  }
  return `${(value * 100).toFixed(1)}¢`;
};

const parseOutcomeLabels = (outcomes?: string | null): string[] => {
  if (!outcomes) {
    return OUTCOME_FALLBACKS;
  }
  try {
    const parsed = JSON.parse(outcomes);
    if (Array.isArray(parsed) && parsed.length) {
      return parsed.map((entry) => String(entry));
    }
  } catch {
    // ignore parsing errors
  }
  return OUTCOME_FALLBACKS;
};

export function TradeForm({ market }: TradeFormProps) {
  const { getToken } = useAuth();
  const { user, eoaAddress, isAuthenticated, refreshUser } = useWallet();
  const { data: balanceData, isLoading: isBalanceLoading } = useBalance();
  const { signTypedDataAsync } = useSignTypedData();
  const { chainId } = useAccount();
  const { switchChainAsync } = useSwitchChain();
  const queryClient = useQueryClient();

  const [side, setSide] = useState<OrderSide>("BUY");
  const [orderType, setOrderType] = useState<OrderTypeValue>("GTC");
  const [gtdExpiration, setGtdExpiration] = useState<string>("");
  const [price, setPrice] = useState<string>("");
  const [shares, setShares] = useState<string>("");
  const [amountMode, setAmountMode] = useState<"shares" | "dollars">("shares");
  const [dollarAmount, setDollarAmount] = useState<string>("");
  const [isPlacingOrder, setIsPlacingOrder] = useState(false);
  const [isAddingToBatch, setIsAddingToBatch] = useState(false);
  const [isSubmittingBatch, setIsSubmittingBatch] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);
  const [selectedOutcomeIndex, setSelectedOutcomeIndex] = useState(0);
  const [batchOrders, setBatchOrders] = useState<PreparedBatchOrder[]>([]);

  const outcomeLabels = useMemo(
    () => parseOutcomeLabels(market?.outcomes),
    [market?.outcomes]
  );

  const outcomeOptions = useMemo<OutcomeOption[]>(() => {
    const labels =
      outcomeLabels.length > 0 ? outcomeLabels : OUTCOME_FALLBACKS;

    return [
      {
        label: labels[0] ?? OUTCOME_FALLBACKS[0],
        tokenId: market?.token_id_yes ?? null,
        lastPrice: market?.yes_price,
        bestBid: market?.yes_best_bid,
        bestAsk: market?.yes_best_ask,
        accent: "constructive",
      },
      {
        label: labels[1] ?? OUTCOME_FALLBACKS[1],
        tokenId: market?.token_id_no ?? null,
        lastPrice: market?.no_price,
        bestBid: market?.no_best_bid,
        bestAsk: market?.no_best_ask,
        accent: "destructive",
      },
    ];
  }, [
    market?.no_best_ask,
    market?.no_best_bid,
    market?.no_price,
    market?.token_id_no,
    market?.token_id_yes,
    market?.yes_best_ask,
    market?.yes_best_bid,
    market?.yes_price,
    outcomeLabels,
  ]);

  const selectedOutcome =
    outcomeOptions[selectedOutcomeIndex] ?? outcomeOptions[0];
  const selectedOutcomeLabel = selectedOutcome?.label ?? "Outcome";
  const hasBatchOrders = batchOrders.length > 0;

  const gtdExpirationSeconds = useMemo(() => {
    if (orderType !== "GTD" || !gtdExpiration) return null;
    const parsed = Date.parse(gtdExpiration);
    if (Number.isNaN(parsed)) return null;
    return Math.floor(parsed / 1000);
  }, [orderType, gtdExpiration]);

  const gtdExpirationError = useMemo(() => {
    if (orderType !== "GTD") return null;
    if (!gtdExpiration) return "Expiration date/time is required for GTD.";
    if (gtdExpirationSeconds === null) return "Invalid expiration value.";
    const minAllowed = Math.floor(Date.now() / 1000) + MIN_GTD_BUFFER_SECONDS;
    if (gtdExpirationSeconds < minAllowed) {
      return `Expiration must be at least ${MIN_GTD_BUFFER_SECONDS} seconds from now to satisfy Polymarket's security buffer.`;
    }
    return null;
  }, [orderType, gtdExpiration, gtdExpirationSeconds]);

  useEffect(() => {
    if (orderType !== "GTD") {
      setGtdExpiration("");
      return;
    }
    if (!gtdExpiration) {
      const defaultDate = new Date(Date.now() + 2 * 60 * 60 * 1000);
      setGtdExpiration(toDateTimeLocalValue(defaultDate));
    }
  }, [orderType, gtdExpiration]);

  // Pre-fill price based on market data when switching sides or loading
  useEffect(() => {
    setSelectedOutcomeIndex(0);
  }, [market?.condition_id]);

  useEffect(() => {
    if (!selectedOutcome) {
      setPrice("");
      return;
    }

    const { bestAsk, bestBid, lastPrice } = selectedOutcome;
    if (side === "BUY") {
      if (typeof bestAsk === "number") {
        setPrice(bestAsk.toString());
      } else if (typeof lastPrice === "number") {
        setPrice(lastPrice.toString());
      }
    } else {
      if (typeof bestBid === "number") {
        setPrice(bestBid.toString());
      } else if (typeof lastPrice === "number") {
        setPrice(lastPrice.toString());
      }
    }
  }, [side, selectedOutcome]);

  const numericPrice = parseFloat(price) || 0;
  const roiPrice = useMemo(() => {
    const last = selectedOutcome?.lastPrice ?? 0;
    const fallback = numericPrice || last;
    if (side === "BUY") {
      return typeof selectedOutcome?.bestAsk === "number" && selectedOutcome.bestAsk > 0
        ? selectedOutcome.bestAsk
        : fallback;
    }
    return typeof selectedOutcome?.bestBid === "number" && selectedOutcome.bestBid > 0
      ? selectedOutcome.bestBid
      : fallback;
  }, [numericPrice, selectedOutcome?.bestAsk, selectedOutcome?.bestBid, selectedOutcome?.lastPrice, side]);
  const numericShares =
    amountMode === "shares"
      ? parseFloat(shares) || 0
      : numericPrice > 0
        ? (parseFloat(dollarAmount) || 0) / numericPrice
        : 0;
  const totalCost =
    amountMode === "dollars"
      ? parseFloat(dollarAmount) || 0
      : numericPrice * numericShares;

  const currentBalance = parseFloat(balanceData?.balance ?? "0") / 1_000_000;
  const vaultAddress = user?.vault_address;
  const isBusy = isPlacingOrder || isAddingToBatch;
  const depthEnabled =
    Boolean(selectedOutcome?.tokenId) &&
    numericShares > 0 &&
    Number.isFinite(numericShares);
  const {
    data: depthEstimate,
    isFetching: isDepthLoading,
  } = useQuery<DepthEstimate | null>({
    queryKey: [
      "depth-estimate",
      market.condition_id,
      selectedOutcome?.tokenId,
      side,
      numericShares,
    ],
    queryFn: () =>
      fetchDepthEstimate(
        market.condition_id,
        selectedOutcome?.tokenId as string,
        side,
        numericShares
      ),
    enabled: depthEnabled,
    staleTime: 10_000,
  });
  const depthHeadline =
    side === "BUY" ? "Estimated Cost" : "Estimated Proceeds";
  const depthFillPercent = depthEstimate?.requestedSize
    ? Math.min(
        (depthEstimate.fillableSize / depthEstimate.requestedSize) * 100,
        100
      )
    : 0;
  const isLimitOrderType = orderType === "GTC" || orderType === "GTD";
  const isImmediateOrderType = orderType === "FOK" || orderType === "FAK";
  const leadDepthLevel = depthEstimate?.levels?.[0];

  // Validation
  const insufficientBalance =
    side === "BUY" && totalCost > currentBalance && currentBalance > 0;

  const canSubmit = useMemo(() => {
    if (!isAuthenticated || !eoaAddress || !vaultAddress) return false;
    if (!selectedOutcome?.tokenId) return false;
    if (numericPrice <= 0 || numericPrice >= 1) return false;
    if (numericShares <= 0) return false;
    if (isBusy) return false;
    if (gtdExpirationError) return false;
    if (side === "BUY" && totalCost > currentBalance && currentBalance > 0)
      return false;
    return true;
  }, [
    isAuthenticated,
    eoaAddress,
    vaultAddress,
    numericPrice,
    numericShares,
    isBusy,
    side,
    totalCost,
    currentBalance,
    selectedOutcome?.tokenId,
    gtdExpirationError,
  ]);

  const prepareOrderPayload = async () => {
      if (!eoaAddress || !vaultAddress) {
        throw new Error("Wallet not connected or vault not deployed.");
      }

    if (chainId !== polygon.id) {
      if (switchChainAsync) {
        await switchChainAsync({ chainId: polygon.id });
      } else {
        throw new Error("Switch wallet to Polygon (137) before trading.");
      }
    }

    const tokenId = selectedOutcome?.tokenId;
    if (!tokenId) {
      throw new Error("Selected outcome does not have a tradable token.");
    }

    const expirationSeconds =
      orderType === "GTD" ? gtdExpirationSeconds ?? 0 : 0;

    if (
      orderType === "GTD" &&
      (!gtdExpirationSeconds || gtdExpirationSeconds <= 0)
    ) {
      throw new Error(
        gtdExpirationError ??
          "Invalid expiration provided for GTD order. Choose a timestamp at least 90 seconds from now."
      );
    }

      const typedData = buildOrderTypedData({
      maker: vaultAddress as `0x${string}`,
      signer: eoaAddress as `0x${string}`,
        tokenId,
        price: numericPrice,
        size: numericShares,
        side,
      expiration: expirationSeconds,
      });

      const signature = await signTypedDataAsync({
        domain: typedData.domain,
        types: typedData.types,
        primaryType: typedData.primaryType,
        message: typedData.message,
      });

    const orderPayload: SerializedOrderPayload = {
        salt: typedData.message.salt.toString(),
        maker: typedData.message.maker,
        signer: typedData.message.signer,
        taker: typedData.message.taker,
        tokenId: typedData.message.tokenId.toString(),
        makerAmount: typedData.message.makerAmount.toString(),
        takerAmount: typedData.message.takerAmount.toString(),
        expiration: typedData.message.expiration.toString(),
        nonce: typedData.message.nonce.toString(),
        feeRateBps: typedData.message.feeRateBps.toString(),
      side,
      signatureType: user?.wallet_type === "SAFE" ? 2 : 1,
        signature,
      };

    return {
      order: orderPayload,
      orderType,
      summary: {
        outcomeLabel: selectedOutcomeLabel,
        side,
        price: numericPrice,
        shares: numericShares,
      },
    };
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccessMsg(null);
    setIsPlacingOrder(true);

    try {
      const prepared = await prepareOrderPayload();
      const token = await getToken();
      if (!token) throw new Error("Wallet authentication required.");

      await api.post(
        "/trade",
        {
          order: prepared.order,
          orderType: prepared.orderType,
        },
        {
          headers: { Authorization: `Bearer ${token}` },
        }
      );

      setSuccessMsg(
        `Order placed for ${prepared.summary.side} ${prepared.summary.outcomeLabel}!`
      );
      setShares("");
      await queryClient.invalidateQueries({ queryKey: ["orders"] });
      setTimeout(() => refreshUser(), 1500);
    } catch (err: any) {
      console.error("Trade failed:", err);
      setError(
        err?.response?.data?.error ||
          err.message ||
          "Failed to place order"
      );
    } finally {
      setIsPlacingOrder(false);
    }
  };

  const handleAddToBatch = async () => {
    if (!canSubmit) return;
    if (batchOrders.length >= 15) {
      setError("Batch queue can hold at most 15 orders.");
      return;
    }
    setError(null);
    setSuccessMsg(null);
    setIsAddingToBatch(true);
    try {
      const prepared = await prepareOrderPayload();
      const batchId =
        typeof crypto !== "undefined" && "randomUUID" in crypto
          ? crypto.randomUUID()
          : `${Date.now()}-${Math.random()}`;
      setBatchOrders((prev) => [
        ...prev,
        {
          id: batchId,
          order: prepared.order,
          orderType: prepared.orderType,
          summary: prepared.summary,
        },
      ]);
      setShares("");
      setSuccessMsg(
        `Added ${prepared.summary.side} ${prepared.summary.outcomeLabel} to batch queue.`
      );
    } catch (err: any) {
      console.error("Add to batch failed:", err);
      setError(
        err?.response?.data?.error ||
          err.message ||
          "Failed to add order to batch"
      );
    } finally {
      setIsAddingToBatch(false);
    }
  };

  const handleSubmitBatch = async () => {
    if (!hasBatchOrders) return;
    setError(null);
    setSuccessMsg(null);
    setIsSubmittingBatch(true);
    try {
      const token = await getToken();
      if (!token) throw new Error("Wallet authentication required.");
      await api.post(
        "/trade/batch",
        {
          orders: batchOrders.map((entry) => ({
            order: entry.order,
            orderType: entry.orderType,
          })),
        },
        { headers: { Authorization: `Bearer ${token}` } }
      );
      setSuccessMsg(`Submitted ${batchOrders.length} batched orders.`);
      setBatchOrders([]);
      await queryClient.invalidateQueries({ queryKey: ["orders"] });
      setTimeout(() => refreshUser(), 1500);
    } catch (err: any) {
      console.error("Batch submit failed:", err);
      setError(
        err?.response?.data?.error ||
          err.message ||
          "Failed to submit batched orders"
      );
    } finally {
      setIsSubmittingBatch(false);
    }
  };

  const handleRemoveBatchOrder = (id: string) => {
    setBatchOrders((prev) => prev.filter((order) => order.id !== id));
  };

  const handleClearBatchOrders = () => setBatchOrders([]);

  const primaryAction = useMemo(() => {
    const baseClasses = cn(
      "flex-1 font-mono font-bold tracking-wider",
      side === "BUY"
        ? "bg-constructive text-black hover:bg-constructive/90"
        : "bg-destructive text-white hover:bg-destructive/90"
    );

    if (!isAuthenticated) {
      return (
        <Button asChild className={baseClasses}>
          <Link href="/sign-in">Sign in</Link>
        </Button>
      );
    }

    if (!eoaAddress) {
      return (
        <div className="flex-1">
          <WalletConnectButton />
        </div>
      );
    }

    if (!vaultAddress) {
      return (
        <Button type="button" className={baseClasses} disabled>
          Connecting…
        </Button>
      );
    }

    const submitting = isPlacingOrder || !canSubmit;

    return (
      <Button
        type="submit"
        className={baseClasses}
        disabled={submitting}
      >
        {isPlacingOrder ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : !selectedOutcome?.tokenId ? (
          "Outcome unavailable"
        ) : (
          <>
            <Send className="mr-2 h-4 w-4" />
            {`${side} ${selectedOutcomeLabel} • ${orderType}`}
          </>
        )}
      </Button>
    );
  }, [
    canSubmit,
    eoaAddress,
    isAuthenticated,
    isPlacingOrder,
    orderType,
    selectedOutcome?.tokenId,
    selectedOutcomeLabel,
    side,
    vaultAddress,
  ]);

  return (
    <Card className="w-full h-full border-border bg-card/60 backdrop-blur-md shadow-xl">
      <CardHeader className="pb-3 border-b border-border/50">
        <CardTitle className="text-sm font-mono uppercase tracking-widest flex justify-between items-center">
          <span>Execution</span>
          <div className="flex gap-2">
             <span className={cn(
               "px-2 py-0.5 rounded-sm text-[10px] cursor-pointer transition-colors",
               side === "BUY" ? "bg-constructive text-black font-bold" : "bg-muted text-muted-foreground hover:text-foreground"
             )} onClick={() => setSide("BUY")}>
               BUY
             </span>
             <span className={cn(
               "px-2 py-0.5 rounded-sm text-[10px] cursor-pointer transition-colors",
               side === "SELL" ? "bg-destructive text-white font-bold" : "bg-muted text-muted-foreground hover:text-foreground"
             )} onClick={() => setSide("SELL")}>
               SELL
             </span>
          </div>
        </CardTitle>
      </CardHeader>
      
      <CardContent className="pt-4 space-y-4">
        {/* Balance Row */}
        <div className="flex justify-between text-xs font-mono text-muted-foreground">
          <span>Available</span>
          <span className="text-foreground flex items-center gap-1">
            <Wallet className="h-3 w-3" />
            {isBalanceLoading ? "..." : `${currentBalance.toFixed(2)} USDC`}
          </span>
        </div>

        {/* Form Inputs */}
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
              Outcome
            </label>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {outcomeOptions.map((option, idx) => {
                const isSelected = idx === selectedOutcomeIndex;
                const disabled = !option.tokenId;
                return (
                  <button
                    key={`${option.label}-${idx}`}
                    type="button"
                    disabled={disabled}
                    onClick={() => setSelectedOutcomeIndex(idx)}
                    className={cn(
                      "rounded-md border px-3 py-2 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2",
                      isSelected
                        ? "border-primary bg-primary/10 text-foreground"
                        : "border-border bg-background/40 text-muted-foreground hover:border-primary/40 hover:text-foreground",
                      disabled && "cursor-not-allowed opacity-50"
                    )}
                  >
                    <span className="text-[11px] font-mono uppercase tracking-wide block">
                      {option.label}
                    </span>
                    <span className="text-xs font-semibold text-foreground">
                      {formatPriceLabel(option.lastPrice)}
                    </span>
                    <span className="text-[10px] text-muted-foreground block">
                      Bid {formatPriceLabel(option.bestBid)} • Ask{" "}
                      {formatPriceLabel(option.bestAsk)}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
              Order Type
            </label>
            <div className="grid grid-cols-2 gap-2">
              {ORDER_TYPE_OPTIONS.map((option) => {
                const isSelected = option.value === orderType;
                return (
                  <button
                    type="button"
                    key={option.value}
                    onClick={() => setOrderType(option.value)}
                    className={cn(
                      "rounded-md border px-3 py-2 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2",
                      isSelected
                        ? "border-primary bg-primary/10 text-foreground"
                        : "border-border bg-background/40 text-muted-foreground hover:border-primary/40 hover:text-foreground"
                    )}
                  >
                    <span className="text-[11px] font-mono uppercase tracking-wide block">
                      {option.label}
                    </span>
                    <span className="text-[10px] text-muted-foreground block">
                      {option.description}
                    </span>
                  </button>
                );
              })}
            </div>
            {isLimitOrderType && (
              <p className="text-[10px] text-muted-foreground font-mono">
                Good-til orders rest on the book at your limit price until filled or cancelled.
              </p>
            )}
            {isImmediateOrderType && (
              <p className="text-[10px] text-muted-foreground font-mono">
                {orderType} sweeps available liquidity instantly—size depends on the current depth.
              </p>
            )}
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            {isLimitOrderType && (
              <div className="space-y-2 rounded border border-border/50 bg-background/60 p-3">
                <div className="space-y-1.5">
                  <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                    Limit Price ({selectedOutcomeLabel})
                  </label>
            <Input 
              type="number" 
              step="0.01"
              min="0.01" 
              max="0.99"
              placeholder="0.00"
              className="font-mono text-right border-border bg-background/50 focus:bg-background transition-colors"
              value={price}
              onChange={(e) => setPrice(e.target.value)}
            />
                </div>
                <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
                  <span>Min $0.01</span>
                  <span>Max $0.99</span>
                </div>
                {orderType === "GTD" && (
                  <div className="space-y-1.5">
                    <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                      Expiration (UTC)
                    </label>
                    <Input
                      type="datetime-local"
                      min={toDateTimeLocalValue(
                        new Date(Date.now() + MIN_GTD_BUFFER_SECONDS * 1000)
                      )}
                      value={gtdExpiration}
                      onChange={(e) => setGtdExpiration(e.target.value)}
                      className="font-mono text-right border-border bg-background/50 focus:bg-background transition-colors"
                    />
                    {gtdExpirationError ? (
                      <p className="text-[10px] text-destructive font-mono">
                        {gtdExpirationError}
                      </p>
                    ) : (
                      <p className="text-[10px] text-muted-foreground">
                        Pick a time at least {MIN_GTD_BUFFER_SECONDS} seconds out.
                      </p>
                    )}
                  </div>
                )}
              </div>
            )}

            <div
              className={cn(
                "space-y-2 rounded border border-border/50 bg-background/60 p-3",
                !isLimitOrderType && "sm:col-span-2"
              )}
            >
              <div className="flex items-center justify-between text-[10px] font-mono uppercase tracking-wide">
                <span>Depth Snapshot</span>
                {depthEnabled ? (
                  isDepthLoading ? (
                    <span className="text-muted-foreground">Syncing...</span>
                  ) : (
                    <span className="text-foreground">{depthFillPercent.toFixed(0)}% cover</span>
                  )
                ) : (
                  <span className="text-muted-foreground">Enter size</span>
                )}
              </div>
              {depthEstimate ? (
                <>
                  <div className="flex justify-between text-xs font-mono">
                    <span className="text-muted-foreground">{depthHeadline}</span>
                    <span className="font-semibold">
                      ${depthEstimate.estimatedTotalValue.toFixed(2)}
                    </span>
                  </div>
                  <div className="h-2 w-full rounded bg-muted">
                    <div
                      className={cn(
                        "h-2 rounded transition-all",
                        depthEstimate.insufficientLiquidity ? "bg-destructive" : "bg-primary"
                      )}
                      style={{ width: `${depthFillPercent}%` }}
                    />
                  </div>
                  <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
                    <span>
                      {depthEstimate.fillableSize.toFixed(0)}/{depthEstimate.requestedSize.toFixed(0)}{" "}
                      shares
                    </span>
                    <span>VWAP {depthEstimate.estimatedAveragePrice.toFixed(3)}</span>
                  </div>
                  {leadDepthLevel && (
                    <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
                      <span>Top level</span>
                      <span>
                        @ {leadDepthLevel.price.toFixed(3)} • {leadDepthLevel.used.toFixed(0)}/
                        {leadDepthLevel.available.toFixed(0)}
                      </span>
                    </div>
                  )}
                  {depthEstimate.insufficientLiquidity && (
                    <p className="text-[10px] text-destructive font-mono">
                      Only {depthEstimate.fillableSize.toFixed(0)} shares currently available.
                    </p>
                  )}
                  {isImmediateOrderType && (
                    <p className="text-[10px] text-muted-foreground font-mono">
                      {orderType} submits against these levels immediately.
                    </p>
                  )}
                </>
              ) : depthEnabled ? (
                <p className="text-[10px] text-muted-foreground font-mono">
                  Order book snapshot unavailable. Retrying shortly.
                </p>
              ) : (
                <p className="text-[10px] text-muted-foreground font-mono">
                  Set a share size to preview liquidity.
                </p>
              )}
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2 items-start">
          <div className="space-y-1.5">
            <div className="flex items-center justify-between text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
              <span>{amountMode === "shares" ? "Shares" : "Dollars"}</span>
              <div className="flex gap-1 text-[10px]">
                <Button
                  type="button"
                  variant={amountMode === "shares" ? "default" : "ghost"}
                  size="sm"
                  className="h-5 px-2 text-[10px]"
                  onClick={() => setAmountMode("shares")}
                >
                  Shares
                </Button>
                <Button
                  type="button"
                  variant={amountMode === "dollars" ? "default" : "ghost"}
                  size="sm"
                  className="h-5 px-2 text-[10px]"
                  onClick={() => setAmountMode("dollars")}
                >
                  USD
                </Button>
              </div>
            </div>
            {amountMode === "shares" ? (
              <Input
                type="number"
                step="1"
                min="1"
                placeholder="0"
                className="font-mono text-right border-border bg-background/50 focus:bg-background transition-colors"
                value={shares}
                onChange={(e) => setShares(e.target.value)}
              />
            ) : (
              <Input
                type="number"
                step="0.01"
                min="1"
                placeholder="0"
                className="font-mono text-right border-border bg-background/50 focus:bg-background transition-colors"
                value={dollarAmount}
                onChange={(e) => setDollarAmount(e.target.value)}
              />
            )}
          </div>

            <div className="p-3 rounded bg-muted/20 border border-border/50 space-y-2">
            <div className="flex justify-between text-xs font-mono">
              <span className="text-muted-foreground">Est. Total</span>
              <span className="text-foreground font-semibold">${totalCost.toFixed(2)}</span>
            </div>
            {side === "BUY" && (
              <div className="flex justify-between text-xs font-mono">
                <span className="text-muted-foreground">Potential ROI</span>
                <span className="text-constructive">
                  {roiPrice > 0 ? (((1 - roiPrice) / roiPrice) * 100).toFixed(0) : 0}%
                </span>
              </div>
            )}
            </div>
          </div>

          {/* Status Messages */}
          {error && (
            <div className="p-2 rounded bg-destructive/10 border border-destructive/20 flex items-start gap-2">
              <AlertCircle className="h-4 w-4 text-destructive shrink-0 mt-0.5" />
              <p className="text-[10px] text-destructive font-mono leading-tight">{error}</p>
            </div>
          )}
          {insufficientBalance && (
            <div className="p-2 rounded bg-amber-100/10 border border-amber-500/30">
              <p className="text-[10px] text-amber-400 font-mono text-center">
                Insufficient USDC balance for this size.
              </p>
            </div>
          )}
          
          {successMsg && (
            <div className="p-2 rounded bg-constructive/10 border border-constructive/20">
              <p className="text-[10px] text-constructive font-mono text-center">{successMsg}</p>
            </div>
          )}

          <div className="flex flex-col gap-2 sm:flex-row">
            {primaryAction}
          <Button 
              type="button"
              variant="secondary"
              className="flex-1 font-mono font-bold tracking-wider"
              disabled={!isAuthenticated || !eoaAddress || !vaultAddress || !canSubmit}
              onClick={handleAddToBatch}
            >
              {isAddingToBatch ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
                <>
                  <ListPlus className="mr-2 h-4 w-4" />
                  Add To Batch
                </>
            )}
          </Button>
          </div>
        </form>

        <div className="space-y-2 rounded-md border border-dashed border-border/60 bg-muted/5 p-3">
          <div className="flex items-center justify-between">
            <p className="text-[10px] font-mono uppercase tracking-wide text-muted-foreground">
              Batch Queue ({batchOrders.length}/15)
            </p>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={!hasBatchOrders || isSubmittingBatch}
                onClick={handleSubmitBatch}
                className="text-[11px] font-mono"
              >
                {isSubmittingBatch ? (
                  <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                ) : (
                  <Send className="mr-2 h-3 w-3" />
                )}
                Submit Batch
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                disabled={!hasBatchOrders}
                onClick={handleClearBatchOrders}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          </div>
          {batchOrders.length === 0 ? (
            <p className="text-[11px] font-mono text-muted-foreground">
              Queue multiple signed orders, then submit them together for faster placement.
            </p>
          ) : (
            <div className="space-y-2">
              {batchOrders.map((entry) => (
                <div
                  key={entry.id}
                  className="flex items-center justify-between rounded border border-border/50 bg-background/60 px-3 py-2 text-xs font-mono"
                >
                  <div className="flex flex-col">
                    <span className="font-semibold text-foreground">
                      {entry.summary.side} {entry.summary.outcomeLabel}
                    </span>
                    <span className="text-[10px] text-muted-foreground">
                      {entry.summary.shares} @{" "}
                      {(entry.summary.price || 0).toFixed(2)} • {entry.orderType}
                    </span>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => handleRemoveBatchOrder(entry.id)}
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
