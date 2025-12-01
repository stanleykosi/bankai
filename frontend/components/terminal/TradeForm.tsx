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
import { useAuth } from "@clerk/nextjs";
import { useSignTypedData, useAccount, useSwitchChain } from "wagmi";
import { polygon } from "viem/chains";
import { Loader2, AlertCircle, Wallet } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useWallet } from "@/hooks/useWallet";
import { useBalance } from "@/hooks/useBalance";
import { buildOrderTypedData } from "@/lib/signing";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import type { Market } from "@/types";

interface TradeFormProps {
  market: Market;
}

type OrderSide = "BUY" | "SELL";

const OUTCOME_FALLBACKS = ["Yes", "No"];

type OutcomeOption = {
  label: string;
  tokenId: string | null;
  lastPrice?: number;
  bestBid?: number;
  bestAsk?: number;
  accent: "constructive" | "destructive";
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

  const [side, setSide] = useState<OrderSide>("BUY");
  const [price, setPrice] = useState<string>("");
  const [shares, setShares] = useState<string>("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);
  const [selectedOutcomeIndex, setSelectedOutcomeIndex] = useState(0);

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
  const numericShares = parseFloat(shares) || 0;
  const totalCost = numericPrice * numericShares;

  const currentBalance = parseFloat(balanceData?.balance ?? "0") / 1_000_000;
  const vaultAddress = user?.vault_address;

  // Validation
  const canSubmit = useMemo(() => {
    if (!isAuthenticated || !eoaAddress || !vaultAddress) return false;
    if (!selectedOutcome?.tokenId) return false;
    if (numericPrice <= 0 || numericPrice >= 1) return false;
    if (numericShares <= 0) return false;
    if (isSubmitting) return false;

    // Simple balance check for buys
    if (side === "BUY" && totalCost > currentBalance) return false;

    return true;
  }, [
    isAuthenticated,
    eoaAddress,
    vaultAddress,
    numericPrice,
    numericShares,
    isSubmitting,
    side,
    totalCost,
    currentBalance,
    selectedOutcome?.tokenId,
  ]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccessMsg(null);
    setIsSubmitting(true);

    try {
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

      // 1. Build EIP-712 Typed Data
      const tokenId = selectedOutcome?.tokenId;
      if (!tokenId) {
        throw new Error("Selected outcome does not have a tradable token.");
      }

      const makerAddress = vaultAddress as `0x${string}`;
      const signerAddress = eoaAddress as `0x${string}`;

      const typedData = buildOrderTypedData({
        maker: makerAddress, // The order is placed by the Proxy/Safe
        signer: signerAddress,  // But signed by the EOA
        tokenId,
        price: numericPrice,
        size: numericShares,
        side,
        expiration: 0, // GTC
      });

      // 2. Sign
      // Note: We need to handle JSON bigints serialization for wagmi
      // wagmi expects standard types, our buildOrderTypedData returns BigInts in 'message'
      // which signTypedDataAsync handles fine.
      const signature = await signTypedDataAsync({
        domain: typedData.domain,
        types: typedData.types,
        primaryType: typedData.primaryType,
        message: typedData.message,
      });

      // 3. Serialize BigInts for JSON payload to Backend
      // The backend expects string representations for numbers
      // The backend Order struct expects:
      // - All numeric fields as strings
      // - Side as "BUY" or "SELL" (OrderSide type)
      // - SignatureType as int (0=EOA, 1=PolyProxy, 2=GnosisSafe)
      const orderPayload = {
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
        side: side, // Backend expects "BUY" or "SELL" string
        signatureType: user?.wallet_type === "SAFE" ? 2 : 1, // 1=Proxy, 2=Safe (0=EOA not used here)
        signature,
      };

      const token = await getToken();
      const response = await api.post("/trade", {
        order: orderPayload,
        orderType: "GTC"
      }, {
        headers: { Authorization: `Bearer ${token}` }
      });

      setSuccessMsg(`Order placed successfully for ${selectedOutcomeLabel}!`);
      setShares(""); // Reset form slightly
      // Refresh balance after short delay
      setTimeout(() => refreshUser(), 2000);

    } catch (err: any) {
      console.error("Trade failed:", err);
      setError(err.response?.data?.error || err.message || "Failed to place order");
    } finally {
      setIsSubmitting(false);
    }
  };

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

          <div className="space-y-1.5">
            <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">Shares</label>
            <Input
              type="number"
              step="1"
              min="1"
              placeholder="0"
              className="font-mono text-right border-border bg-background/50 focus:bg-background transition-colors"
              value={shares}
              onChange={(e) => setShares(e.target.value)}
            />
          </div>

          {/* Totals */}
          <div className="p-3 rounded bg-muted/20 border border-border/50 space-y-2">
            <div className="flex justify-between text-xs font-mono">
              <span className="text-muted-foreground">Est. Total</span>
              <span className="text-foreground font-semibold">${totalCost.toFixed(2)}</span>
            </div>
            {side === "BUY" && (
              <div className="flex justify-between text-xs font-mono">
                <span className="text-muted-foreground">Potential ROI</span>
                <span className="text-constructive">
                  {numericPrice > 0 ? ((1 - numericPrice) / numericPrice * 100).toFixed(0) : 0}%
                </span>
              </div>
            )}
          </div>

          {/* Status Messages */}
          {error && (
            <div className="p-2 rounded bg-destructive/10 border border-destructive/20 flex items-start gap-2">
              <AlertCircle className="h-4 w-4 text-destructive shrink-0 mt-0.5" />
              <p className="text-[10px] text-destructive font-mono leading-tight">{error}</p>
            </div>
          )}

          {successMsg && (
            <div className="p-2 rounded bg-constructive/10 border border-constructive/20">
              <p className="text-[10px] text-constructive font-mono text-center">{successMsg}</p>
            </div>
          )}

          {/* Submit Button */}
          <Button
            type="submit"
            className={cn(
              "w-full font-mono font-bold tracking-wider",
              side === "BUY"
                ? "bg-constructive text-black hover:bg-constructive/90"
                : "bg-destructive text-white hover:bg-destructive/90"
            )}
            disabled={!canSubmit}
          >
            {isSubmitting ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : !isAuthenticated ? (
              "Connect Wallet"
            ) : !vaultAddress ? (
              "Deploy Vault"
            ) : !selectedOutcome?.tokenId ? (
              "Outcome unavailable"
            ) : (
              `${side} ${selectedOutcomeLabel}`
            )}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

