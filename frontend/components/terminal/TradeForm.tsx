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

import React, { useState, useMemo, useEffect, useCallback, useRef } from "react";
import Link from "next/link";
import { useAuth } from "@clerk/nextjs";
import { useAccount, useSwitchChain } from "wagmi";
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
import { useUserApiCredentials } from "@/hooks/useUserApiCredentials";
import { useClobClient } from "@/hooks/useClobClient";
import { Side, OrderType } from "@polymarket/clob-client";
import type { UserOrder, UserMarketOrder } from "@polymarket/clob-client";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { fetchDepthEstimate } from "@/lib/market-data";
import { ensurePolygonChain } from "@/lib/chain-utils";
import type { DepthEstimate, Market } from "@/types";
import { calculateDisplayPrice } from "@/lib/price-utils";

interface TradeFormProps {
  market: Market;
  selectedOutcomeIndex?: number;
  onOutcomeChange?: (index: number) => void;
}

type OrderSide = "BUY" | "SELL";
type OrderTypeValue = "GTC" | "GTD" | "FOK" | "FAK";
type ExecutionType = "LIMIT" | "MARKET";

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


const toDateTimeLocalValue = (date: Date) => {
  const tzOffsetMs = date.getTimezoneOffset() * 60 * 1000;
  const local = new Date(date.getTime() - tzOffsetMs);
  return local.toISOString().slice(0, 16);
};

type PreparedBatchOrder = {
  id: string;
  orderParams: {
    tokenId: string;
    price: number;
    size: number;
    side: Side;
    expiration: number;
    orderType: OrderType;
    isNegRisk: boolean;
  };
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

const formatUsd = (value?: number) => {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return "--";
  }
  return `$${value.toFixed(2)}`;
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

export function TradeForm({
  market,
  selectedOutcomeIndex: controlledOutcomeIndex,
  onOutcomeChange,
}: TradeFormProps) {
  const { getToken } = useAuth();
  const { user, eoaAddress, isAuthenticated, refreshUser } = useWallet();
  const { data: balanceData, isLoading: isBalanceLoading } = useBalance();
  const { chainId } = useAccount();
  const { switchChainAsync } = useSwitchChain();
  const queryClient = useQueryClient();

  // Get user API credentials for CLOB authentication
  const {
    credentials,
    getCredentials,
    isLoading: isCredentialsLoading,
    error: credentialsError,
  } = useUserApiCredentials();

  // Initialize ClobClient with credentials and builder config
  const { clobClient } = useClobClient({
    credentials,
    vaultAddress: user?.vault_address ?? null,
    walletType: user?.wallet_type ?? null,
  });

  const syncOrderToBackend = useCallback(
    async (order: {
      orderId: string;
      marketId: string;
      outcome: string;
      outcomeTokenId: string;
      side: OrderSide;
      price: number;
      size: number;
      orderType: OrderTypeValue;
      status: string;
      orderHashes: string[];
      source: "BANKAI" | "EXTERNAL" | "UNKNOWN";
      makerAddress?: string | null;
    }) => {
      try {
        const token = await getToken();
        if (!token) return;
        await api.post(
          "/trade/sync",
          {
            orders: [
              {
                orderId: order.orderId,
                marketId: order.marketId,
                outcome: order.outcome,
                outcomeTokenId: order.outcomeTokenId,
                side: order.side,
                price: order.price,
                size: order.size,
                orderType: order.orderType,
                status: order.status,
                statusDetail: order.status,
                orderHashes: order.orderHashes,
                source: order.source,
                makerAddress: order.makerAddress ?? "",
                createdAt: new Date().toISOString(),
                updatedAt: new Date().toISOString(),
              },
            ],
          },
          { headers: { Authorization: `Bearer ${token}` } }
        );
      } catch (err) {
        console.error("Failed to sync order to backend", err);
      }
    },
    [getToken]
  );

  // Use ref to track latest clobClient (avoids stale closure issues)
  const clobClientRef = useRef(clobClient);
  useEffect(() => {
    clobClientRef.current = clobClient;
  }, [clobClient]);

  const [side, setSide] = useState<OrderSide>("BUY");
  const [orderType, setOrderType] = useState<OrderTypeValue>("GTC");
  const [executionType, setExecutionType] = useState<ExecutionType>("LIMIT");
  const [limitExpires, setLimitExpires] = useState(false);
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
  const [localOutcomeIndex, setLocalOutcomeIndex] = useState(0);
  const [batchOrders, setBatchOrders] = useState<PreparedBatchOrder[]>([]);

  const outcomeLabels = useMemo(
    () => parseOutcomeLabels(market?.outcomes),
    [market?.outcomes]
  );

  const outcomeOptions = useMemo<OutcomeOption[]>(() => {
    const labels =
      outcomeLabels.length > 0 ? outcomeLabels : OUTCOME_FALLBACKS;

    // Calculate current display price for each outcome (midpoint unless spread > 10c)
    const yesCurrentPrice = calculateDisplayPrice(
      market?.yes_best_bid,
      market?.yes_best_ask,
      market?.yes_price,
      market?.condition_id ? `${market.condition_id}:yes` : undefined
    );
    const noCurrentPrice = calculateDisplayPrice(
      market?.no_best_bid,
      market?.no_best_ask,
      market?.no_price,
      market?.condition_id ? `${market.condition_id}:no` : undefined
    );

    return [
      {
        label: labels[0] ?? OUTCOME_FALLBACKS[0],
        tokenId: market?.token_id_yes ?? null,
        lastPrice: yesCurrentPrice, // Use display price per Polymarket rule
        bestBid: market?.yes_best_bid,
        bestAsk: market?.yes_best_ask,
        accent: "constructive",
      },
      {
        label: labels[1] ?? OUTCOME_FALLBACKS[1],
        tokenId: market?.token_id_no ?? null,
        lastPrice: noCurrentPrice, // Use display price per Polymarket rule
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

  const selectedOutcomeIndex =
    typeof controlledOutcomeIndex === "number"
      ? controlledOutcomeIndex
      : localOutcomeIndex;
  const selectedOutcome =
    outcomeOptions[selectedOutcomeIndex] ?? outcomeOptions[0];
  const selectedOutcomeLabel = selectedOutcome?.label ?? "Outcome";
  const hasBatchOrders = batchOrders.length > 0;

  // Market rule metadata
  const tickSize = useMemo(() => {
    const raw = market?.order_price_min_tick;
    if (typeof raw === "number" && raw > 0) return raw;
    return 0.01; // Polymarket default tick if not provided
  }, [market?.order_price_min_tick]);

  const minSize = useMemo(() => {
    const raw = market?.order_min_size;
    if (typeof raw === "number" && raw > 0) return raw;
    return 1; // 1 share fallback
  }, [market?.order_min_size]);

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

  useEffect(() => {
    if (executionType === "MARKET") {
      setOrderType("FAK");
      return;
    }
    setOrderType(limitExpires ? "GTD" : "GTC");
  }, [executionType, limitExpires]);

  // Pre-fill price based on market data when switching sides or loading
  const updateOutcomeIndex = useCallback(
    (nextIndex: number) => {
      if (typeof controlledOutcomeIndex === "number") {
        onOutcomeChange?.(nextIndex);
        return;
      }
      setLocalOutcomeIndex(nextIndex);
      onOutcomeChange?.(nextIndex);
    },
    [controlledOutcomeIndex, onOutcomeChange]
  );

  useEffect(() => {
    updateOutcomeIndex(0);
  }, [market?.condition_id, updateOutcomeIndex]);

  useEffect(() => {
    if (!selectedOutcome) {
      setPrice("");
      return;
    }

    // Use display price as the default for limit orders
    // For market orders, we'll use best bid/ask directly
    const { bestAsk, bestBid, lastPrice } = selectedOutcome;
    const displayPrice = calculateDisplayPrice(bestBid, bestAsk, lastPrice, selectedOutcome?.tokenId ?? undefined);
    
    if (side === "BUY") {
      // For BUY: prefer best ask (what you'd pay), fall back to display price
      if (typeof bestAsk === "number" && bestAsk > 0) {
        setPrice(bestAsk.toString());
      } else if (typeof displayPrice === "number") {
        setPrice(displayPrice.toString());
      }
    } else {
      // For SELL: prefer best bid (what you'd receive), fall back to display price
      if (typeof bestBid === "number" && bestBid > 0) {
        setPrice(bestBid.toString());
      } else if (typeof displayPrice === "number") {
        setPrice(displayPrice.toString());
      }
    }
  }, [side, selectedOutcome]);

  const rawPrice = parseFloat(price) || 0;
  const isLimitOrderType = executionType === "LIMIT";
  const isMarketOrderType = executionType === "MARKET";
  const marketReferencePrice = useMemo(() => {
    const last = selectedOutcome?.lastPrice ?? 0;
    if (side === "BUY") {
      if (typeof selectedOutcome?.bestAsk === "number" && selectedOutcome.bestAsk > 0) {
        return selectedOutcome.bestAsk;
      }
    } else if (typeof selectedOutcome?.bestBid === "number" && selectedOutcome.bestBid > 0) {
      return selectedOutcome.bestBid;
    }
    return last;
  }, [selectedOutcome?.bestAsk, selectedOutcome?.bestBid, selectedOutcome?.lastPrice, side]);
  const conversionPrice = isLimitOrderType ? rawPrice : marketReferencePrice;
  const numericShares =
    amountMode === "shares"
      ? parseFloat(shares) || 0
      : conversionPrice > 0
        ? (parseFloat(dollarAmount) || 0) / conversionPrice
        : 0;

  const currentBalance = parseFloat(balanceData?.balance ?? "0") / 1_000_000;
  const vaultAddress = user?.vault_address;
  const isBusy = isPlacingOrder || isAddingToBatch;
  const depthEnabled =
    isMarketOrderType &&
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
  const depthFillPercent = depthEstimate?.requestedSize
    ? Math.min(
      (depthEstimate.fillableSize / depthEstimate.requestedSize) * 100,
      100
    )
    : 0;
  const executionPrice = useMemo(() => {
    if (isLimitOrderType) {
      return rawPrice;
    }
    if (typeof depthEstimate?.estimatedAveragePrice === "number" && depthEstimate.estimatedAveragePrice > 0) {
      return depthEstimate.estimatedAveragePrice;
    }
    return marketReferencePrice;
  }, [depthEstimate?.estimatedAveragePrice, isLimitOrderType, marketReferencePrice, rawPrice]);
  const numericPrice = executionPrice;
  const inputDollars = parseFloat(dollarAmount) || 0;
  const totalCost =
    amountMode === "dollars" ? inputDollars : numericPrice * numericShares;
  const displayPrice = useMemo(() => {
    if (isLimitOrderType) {
      return rawPrice > 0 ? rawPrice : marketReferencePrice;
    }
    return numericPrice;
  }, [isLimitOrderType, marketReferencePrice, numericPrice, rawPrice]);
  const roiPrice = numericPrice > 0 ? numericPrice : marketReferencePrice;
  const payoutShares =
    amountMode === "shares"
      ? numericShares
      : numericPrice > 0
        ? inputDollars / numericPrice
        : 0;
  const potentialWin = useMemo(() => {
    if (payoutShares <= 0 || roiPrice <= 0) return 0;
    if (side === "BUY") {
      return (1 - roiPrice) * payoutShares;
    }
    return roiPrice * payoutShares;
  }, [payoutShares, roiPrice, side]);
  const conversionHint = useMemo(() => {
    if (!conversionPrice || conversionPrice <= 0 || numericShares <= 0) {
      return amountMode === "shares"
        ? "Enter shares to preview cost."
        : "Enter USD to preview shares.";
    }
    if (amountMode === "shares") {
      return `≈ ${formatUsd(conversionPrice * numericShares)} @ $${conversionPrice.toFixed(3)}`;
    }
    return `≈ ${numericShares.toFixed(2)} shares @ $${conversionPrice.toFixed(3)}`;
  }, [amountMode, conversionPrice, numericShares]);
  const executionLabel = executionType === "LIMIT" ? "Limit" : "Market";

  // Validation
  const insufficientBalance =
    side === "BUY" && totalCost > currentBalance && currentBalance > 0;

  const onTick = useMemo(() => {
    if (!isLimitOrderType) return true;
    if (!rawPrice || tickSize <= 0) return true;
    const steps = rawPrice / tickSize;
    return Math.abs(steps - Math.round(steps)) < 1e-6;
  }, [isLimitOrderType, rawPrice, tickSize]);

  const sizeTooSmall = useMemo(() => {
    return numericShares > 0 && numericShares < minSize;
  }, [numericShares, minSize]);

  const canSubmit = useMemo(() => {
    if (!isAuthenticated || !eoaAddress || !vaultAddress) return false;
    if (!selectedOutcome?.tokenId) return false;
    if (numericPrice <= 0 || numericPrice >= 1) return false;
    if (!onTick) return false;
    if (numericShares <= 0 || sizeTooSmall) return false;
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
    onTick,
    numericShares,
    sizeTooSmall,
    isBusy,
    side,
    totalCost,
    currentBalance,
    selectedOutcome?.tokenId,
    gtdExpirationError,
  ]);

  // Map our order type to SDK OrderType enum
  const sdkOrderType = useMemo(() => {
    switch (orderType) {
      case "GTC":
        return OrderType.GTC;
      case "GTD":
        return OrderType.GTD;
      case "FOK":
        return OrderType.FOK;
      case "FAK":
        return OrderType.FAK;
      default:
        return OrderType.GTC;
    }
  }, [orderType]);

  // Map our side to SDK Side enum
  const sdkSide = useMemo(() => {
    return side === "BUY" ? Side.BUY : Side.SELL;
  }, [side]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccessMsg(null);
    setIsPlacingOrder(true);

    try {
      if (!eoaAddress || !vaultAddress) {
        throw new Error("Wallet not connected or vault not deployed.");
      }

      // Ensure we're on Polygon network before proceeding
      // This is critical for Phantom Wallet which validates chainId in EIP-712 signatures
      await ensurePolygonChain(
        () => chainId,
        switchChainAsync
      );

      const tokenId = selectedOutcome?.tokenId;
      if (!tokenId) {
        throw new Error("Selected outcome does not have a tradable token.");
      }

      // Ensure we have API credentials before proceeding
      if (!credentials) {
        await getCredentials();
        // Wait for React state update and useClobClient hook to recreate client
        await new Promise((resolve) => setTimeout(resolve, 500));
      }

      // Use ref to get latest clobClient (handles state update timing)
      let currentClient = clobClientRef.current;
      if (!currentClient && credentials) {
        // If we have credentials but no client yet, wait a bit more
        await new Promise((resolve) => setTimeout(resolve, 300));
        currentClient = clobClientRef.current;
      }

      if (!currentClient) {
        throw new Error("Trading client not ready. Please ensure your wallet is connected and try again.");
      }

      // Calculate expiration
      // - GTD: user-provided timestamp
      // - GTC/FOK/FAK: 0 (SDK handles this correctly)
      let expiration = 0;
      if (orderType === "GTD") {
        if (!gtdExpirationSeconds || gtdExpirationSeconds <= 0) {
          throw new Error(
            gtdExpirationError ??
            "Invalid expiration provided for GTD order. Choose a timestamp at least 90 seconds from now."
          );
        }
        expiration = gtdExpirationSeconds;
      }

      // Check if neg risk market
      const isNegRisk = market?.neg_risk || market?.neg_risk_other;

      // Handle limit orders (GTC/GTD) vs market orders (FOK/FAK)
      const isLimitOrder = orderType === "GTC" || orderType === "GTD";
      let response;

      if (isLimitOrder) {
        // Create limit order using SDK (handles signing and submission)
        const limitOrder: UserOrder = {
          tokenID: tokenId,
          price: numericPrice,
          size: numericShares,
          side: sdkSide,
          feeRateBps: 0,
          expiration,
          taker: "0x0000000000000000000000000000000000000000",
        };

        response = await currentClient.createAndPostOrder(
          limitOrder,
          { negRisk: isNegRisk },
          sdkOrderType as OrderType.GTC | OrderType.GTD
        );
      } else {
        // Create market order using SDK (handles signing and submission)
        // For BUY orders: amount is in $$$, for SELL orders: amount is in shares
        const buyAmount =
          amountMode === "dollars"
            ? parseFloat(dollarAmount) || numericPrice * numericShares
            : numericPrice * numericShares;
        const marketOrder: UserMarketOrder = {
          tokenID: tokenId,
          price: numericPrice > 0 ? numericPrice : undefined, // Optional for market orders
          amount: side === "BUY" ? buyAmount : numericShares,
          side: sdkSide,
          feeRateBps: 0,
          taker: "0x0000000000000000000000000000000000000000",
        };

        response = await currentClient.createAndPostMarketOrder(
          marketOrder,
          { negRisk: isNegRisk },
          sdkOrderType as OrderType.FOK | OrderType.FAK
        );
      }

      const responseError =
        (response as any)?.error ||
        (response as any)?.data?.error ||
        (response as any)?.message;
      if (responseError) {
        throw new Error(responseError);
      }

      if (response.orderID) {
        setSuccessMsg(
          `Order placed for ${side} ${selectedOutcomeLabel}!`
        );
        setShares("");
        await queryClient.invalidateQueries({ queryKey: ["orders"] });
        // Async persist to backend for audit/history
        void syncOrderToBackend({
          orderId: response.orderID,
          marketId: market?.condition_id ?? "",
          outcome: selectedOutcomeLabel,
          outcomeTokenId: tokenId,
          side,
          price: numericPrice,
          size: numericShares,
          orderType,
          status: response.status ?? "OPEN",
          orderHashes: response.orderHashes ?? [],
          source: "BANKAI",
          makerAddress: vaultAddress,
        });
        setTimeout(() => refreshUser(), 1500);
      } else {
        throw new Error(
          responseError || "Order submission failed - no order ID returned"
        );
      }
    } catch (err: any) {
      console.error("Trade failed:", err);
      setError(
        err?.response?.data?.error ||
        err?.message ||
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
      const tokenId = selectedOutcome?.tokenId;
      if (!tokenId) {
        throw new Error("Selected outcome does not have a tradable token.");
      }

      let expiration = 0;
      if (orderType === "GTD") {
        if (!gtdExpirationSeconds || gtdExpirationSeconds <= 0) {
          throw new Error(
            gtdExpirationError ??
            "Invalid expiration provided for GTD order."
          );
        }
        expiration = gtdExpirationSeconds;
      }

      // Store order params for batch submission (will use SDK when submitting)
      const batchId =
        typeof crypto !== "undefined" && "randomUUID" in crypto
          ? crypto.randomUUID()
          : `${Date.now()}-${Math.random()}`;
      setBatchOrders((prev) => [
        ...prev,
        {
          id: batchId,
          orderParams: {
            tokenId,
            price: numericPrice,
            size: numericShares,
            side: sdkSide,
            expiration,
            orderType: sdkOrderType,
            isNegRisk: !!(market?.neg_risk || market?.neg_risk_other),
          },
          summary: {
            outcomeLabel: selectedOutcomeLabel,
            side,
            price: numericPrice,
            shares: numericShares,
          },
        },
      ]);
      setShares("");
      setSuccessMsg(
        `Added ${side} ${selectedOutcomeLabel} to batch queue.`
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
    
    // Ensure credentials are available
    if (!credentials) {
      try {
        await getCredentials();
        await new Promise((resolve) => setTimeout(resolve, 500));
      } catch (err: any) {
        setError("Failed to get trading credentials. Please try again.");
        return;
      }
    }

    const currentClient = clobClientRef.current;
    if (!currentClient) {
      setError("Trading client not ready. Please try again.");
      return;
    }
    setError(null);
    setSuccessMsg(null);
    setIsSubmittingBatch(true);
    try {
      // Submit each order in the batch using SDK
      const results = await Promise.allSettled(
        batchOrders.map((entry) => {
          const isLimitOrder = entry.orderParams.orderType === OrderType.GTC || entry.orderParams.orderType === OrderType.GTD;
          
          if (isLimitOrder) {
            const limitOrder: UserOrder = {
              tokenID: entry.orderParams.tokenId,
              price: entry.orderParams.price,
              size: entry.orderParams.size,
              side: entry.orderParams.side,
              feeRateBps: 0,
              expiration: entry.orderParams.expiration,
              taker: "0x0000000000000000000000000000000000000000",
            };
            return currentClient.createAndPostOrder(
              limitOrder,
              { negRisk: entry.orderParams.isNegRisk },
              entry.orderParams.orderType as OrderType.GTC | OrderType.GTD
            );
          } else {
            // For market orders: BUY uses $$$ amount, SELL uses shares
            const marketOrder: UserMarketOrder = {
              tokenID: entry.orderParams.tokenId,
              price: entry.orderParams.price > 0 ? entry.orderParams.price : undefined,
              amount: entry.orderParams.side === Side.BUY 
                ? entry.orderParams.price * entry.orderParams.size 
                : entry.orderParams.size,
              side: entry.orderParams.side,
              feeRateBps: 0,
              taker: "0x0000000000000000000000000000000000000000",
            };
            return currentClient.createAndPostMarketOrder(
              marketOrder,
              { negRisk: entry.orderParams.isNegRisk },
              entry.orderParams.orderType as OrderType.FOK | OrderType.FAK
            );
          }
        })
      );

      const successful = results.filter((r) => r.status === "fulfilled").length;
      const failed = results.filter((r) => r.status === "rejected").length;

      if (successful > 0) {
        setSuccessMsg(
          `Submitted ${successful} order${successful > 1 ? "s" : ""}${failed > 0 ? ` (${failed} failed)` : ""}.`
        );
        setBatchOrders([]);
        await queryClient.invalidateQueries({ queryKey: ["orders"] });
        setTimeout(() => refreshUser(), 1500);
      } else {
        throw new Error("All orders in batch failed");
      }
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
            {`${side} ${selectedOutcomeLabel} • ${executionLabel}`}
          </>
        )}
      </Button>
    );
  }, [
    canSubmit,
    eoaAddress,
    isAuthenticated,
    isPlacingOrder,
    executionLabel,
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
                    onClick={() => updateOutcomeIndex(idx)}
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
                  </button>
                );
              })}
            </div>
          </div>

          <div className="space-y-2">
            <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
              Order Type
            </label>
            <div className="flex rounded-md border border-border bg-background/40 p-1 text-[10px] font-mono uppercase tracking-wide">
              <button
                type="button"
                onClick={() => setExecutionType("LIMIT")}
                className={cn(
                  "flex-1 rounded-sm px-3 py-2 transition-colors",
                  executionType === "LIMIT"
                    ? "bg-primary/20 text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                Limit
              </button>
              <button
                type="button"
                onClick={() => setExecutionType("MARKET")}
                className={cn(
                  "flex-1 rounded-sm px-3 py-2 transition-colors",
                  executionType === "MARKET"
                    ? "bg-primary/20 text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                Market
              </button>
            </div>
            <p className="text-[10px] font-mono text-muted-foreground">
              {executionType === "LIMIT"
                ? "Posts to the book at your price."
                : "Executes against live liquidity."}
            </p>
          </div>

          <div className="space-y-3">
            <div className="space-y-2 rounded border border-border/50 bg-background/60 p-3">
              <div className="flex items-center justify-between text-[10px] font-mono uppercase tracking-wide">
                <span>
                  {isLimitOrderType ? "Limit Price" : "Market Price"} ({selectedOutcomeLabel})
                </span>
                {isLimitOrderType && (
                  <label className="flex items-center gap-2 text-muted-foreground">
                    <input
                      type="checkbox"
                      checked={limitExpires}
                      onChange={(e) => setLimitExpires(e.target.checked)}
                      className="h-3 w-3 accent-primary"
                    />
                    <span>Expires</span>
                  </label>
                )}
              </div>
              <Input
                type="number"
                step="0.01"
                min="0.01"
                max="0.99"
                placeholder="0.00"
                readOnly={isMarketOrderType}
                className={cn(
                  "font-mono text-right border-border bg-background/60",
                  isMarketOrderType && "text-muted-foreground"
                )}
                value={
                  isMarketOrderType
                    ? displayPrice > 0
                      ? displayPrice.toFixed(3)
                      : ""
                    : price
                }
                onChange={(e) => setPrice(e.target.value)}
              />
              <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
                <span>Min $0.01</span>
                <span>{isMarketOrderType ? "Live book" : "Max $0.99"}</span>
              </div>
              {isLimitOrderType && limitExpires && (
                <div className="space-y-1.5">
                  <Input
                    type="datetime-local"
                    min={toDateTimeLocalValue(
                      new Date(Date.now() + MIN_GTD_BUFFER_SECONDS * 1000)
                    )}
                    value={gtdExpiration}
                    onChange={(e) => setGtdExpiration(e.target.value)}
                    className="font-mono text-right border-border bg-background/60"
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

            {isMarketOrderType && (
              <div className="space-y-2 rounded border border-border/50 bg-background/60 p-3">
                <div className="flex items-center justify-between text-[10px] font-mono uppercase tracking-wide">
                  <span>Depth Snapshot</span>
                  {depthEnabled ? (
                    isDepthLoading ? (
                      <span className="text-muted-foreground">Updating</span>
                    ) : (
                      <span className="text-foreground">
                        {depthFillPercent.toFixed(0)}% cover
                      </span>
                    )
                  ) : (
                    <span className="text-muted-foreground">Enter size</span>
                  )}
                </div>
                {depthEstimate ? (
                  <>
                    <div className="flex justify-between text-xs font-mono">
                      <span className="text-muted-foreground">Est. fill</span>
                      <span className="font-semibold text-foreground">
                        ${depthEstimate.estimatedAveragePrice.toFixed(3)}
                      </span>
                    </div>
                    <div className="h-1.5 w-full rounded bg-muted/60">
                      <div
                        className={cn(
                          "h-1.5 rounded",
                          depthEstimate.insufficientLiquidity
                            ? "bg-amber-500"
                            : "bg-primary"
                        )}
                        style={{ width: `${depthFillPercent}%` }}
                      />
                    </div>
                    <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
                      <span>
                        {depthEstimate.fillableSize.toFixed(0)}/
                        {depthEstimate.requestedSize.toFixed(0)} shares
                      </span>
                      <span>
                        {side === "BUY" ? "Cost" : "Proceeds"} $
                        {depthEstimate.estimatedTotalValue.toFixed(2)}
                      </span>
                    </div>
                    {depthEstimate.insufficientLiquidity && (
                      <p className="text-[10px] text-amber-400 font-mono">
                        Partial fill likely at this size.
                      </p>
                    )}
                  </>
                ) : depthEnabled ? (
                  <p className="text-[10px] text-muted-foreground font-mono">
                    Pulling order book pricing...
                  </p>
                ) : (
                  <p className="text-[10px] text-muted-foreground font-mono">
                    Enter size to preview fill price.
                  </p>
                )}
              </div>
            )}

            <div className="space-y-2 rounded border border-border/50 bg-background/60 p-3">
              <div className="flex items-center justify-between text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                <span>Size</span>
                <div className="flex rounded-md border border-border bg-background/40 p-0.5 text-[10px]">
                  <button
                    type="button"
                    onClick={() => setAmountMode("shares")}
                    className={cn(
                      "rounded-sm px-2.5 py-1 transition-colors",
                      amountMode === "shares"
                        ? "bg-primary/20 text-foreground"
                        : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    Shares
                  </button>
                  <button
                    type="button"
                    onClick={() => setAmountMode("dollars")}
                    className={cn(
                      "rounded-sm px-2.5 py-1 transition-colors",
                      amountMode === "dollars"
                        ? "bg-primary/20 text-foreground"
                        : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    USD
                  </button>
                </div>
              </div>
              {amountMode === "shares" ? (
                <Input
                  type="number"
                  step="1"
                  min="1"
                  placeholder="0"
                  className="font-mono text-right border-border bg-background/60"
                  value={shares}
                  onChange={(e) => setShares(e.target.value)}
                />
              ) : (
                <Input
                  type="number"
                  step="0.01"
                  min="1"
                  placeholder="0"
                  className="font-mono text-right border-border bg-background/60"
                  value={dollarAmount}
                  onChange={(e) => setDollarAmount(e.target.value)}
                />
              )}
              <p className="text-[10px] text-muted-foreground font-mono">
                {conversionHint}
              </p>
            </div>

            <div className="p-3 rounded bg-muted/20 border border-border/50 space-y-2">
              <div className="flex justify-between text-xs font-mono">
                <span className="text-muted-foreground">Potential Win</span>
                <span className="text-foreground font-semibold">
                  {formatUsd(potentialWin)}
                </span>
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
                      {(entry.summary.price || 0).toFixed(2)} •{" "}
                      {entry.orderParams.orderType === OrderType.GTC ||
                      entry.orderParams.orderType === OrderType.GTD
                        ? "Limit"
                        : "Market"}
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
