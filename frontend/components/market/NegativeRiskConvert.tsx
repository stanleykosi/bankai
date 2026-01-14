"use client";

import { useEffect, useMemo, useState } from "react";
import { AlertCircle, ExternalLink, Loader2, ShieldAlert } from "lucide-react";
import { useAccount } from "wagmi";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { useNegativeRisk } from "@/hooks/useNegativeRisk";
import { useWallet } from "@/hooks/useWallet";
import type { Market } from "@/types";
import { cn } from "@/lib/utils";
import { NEG_RISK_ADAPTER_ADDR } from "@/lib/polymarket";

interface NegativeRiskConvertProps {
  market: Market;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const toMarketId = (questionId?: string) => {
  if (!questionId || !questionId.startsWith("0x")) return null;
  try {
    const value = BigInt(questionId);
    const masked = value & ~0xffn;
    return `0x${masked.toString(16).padStart(64, "0")}` as `0x${string}`;
  } catch {
    return null;
  }
};

const toIndexSet = (questionId?: string) => {
  if (!questionId || !questionId.startsWith("0x")) return null;
  try {
    const value = BigInt(questionId);
    const index = Number(value & 0xffn);
    return 1n << BigInt(index);
  } catch {
    return null;
  }
};

export function NegativeRiskConvert({
  market,
  open,
  onOpenChange,
}: NegativeRiskConvertProps) {
  const { address } = useAccount();
  const { user } = useWallet();
  const {
    approveConditionalTokens,
    convertPositions,
    refreshApprovals,
    ctfApproved,
    status,
    error,
    txHash,
    isSubmitting,
    reset,
  } = useNegativeRisk();

  const [amount, setAmount] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  const marketId = useMemo(
    () => toMarketId(market?.question_id),
    [market?.question_id]
  );
  const indexSet = useMemo(
    () => toIndexSet(market?.question_id),
    [market?.question_id]
  );

  const numericAmount = Number(amount);
  const estimatedYes =
    Number.isFinite(numericAmount) && numericAmount > 0
      ? numericAmount
      : null;

  useEffect(() => {
    if (open) {
      void refreshApprovals();
    }
  }, [open, refreshApprovals]);

  useEffect(() => {
    if (!open) {
      setAmount("");
      setLocalError(null);
      reset();
    }
  }, [open, reset]);

  const canConvert =
    Boolean(address) &&
    Boolean(marketId) &&
    Boolean(indexSet) &&
    Boolean(amount) &&
    ctfApproved &&
    !isSubmitting;

  const handleConvert = async () => {
    setLocalError(null);
    if (!marketId || !indexSet) {
      setLocalError("Market metadata unavailable for conversion.");
      return;
    }
    if (!amount || Number.isNaN(Number(amount)) || Number(amount) <= 0) {
      setLocalError("Enter a valid amount of NO shares.");
      return;
    }
    await convertPositions(marketId, indexSet, amount);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Negative Risk Convert</DialogTitle>
          <DialogDescription>
            Convert NO shares in this market into YES shares across other outcomes.
          </DialogDescription>
        </DialogHeader>

        {user?.wallet_type && (
          <div className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs text-amber-200">
            <ShieldAlert className="mt-0.5 h-4 w-4" />
            <span>
              Conversions execute from your connected wallet. If you trade via a
              vault ({user.wallet_type.toLowerCase()}), move NO tokens to the
              connected wallet before converting.
            </span>
          </div>
        )}

        {!address ? (
          <div className="flex flex-col items-center gap-3 rounded-md border border-border/60 bg-muted/10 p-4 text-center">
            <p className="text-xs font-mono text-muted-foreground">
              Connect a wallet to convert positions.
            </p>
            <WalletConnectButton />
          </div>
        ) : (
          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                NO Shares to Convert
              </label>
              <Input
                type="number"
                min="0"
                step="0.01"
                placeholder="0.00"
                className="font-mono text-right border-border bg-background/60"
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
              />
              <p className="text-[10px] text-muted-foreground font-mono">
                {estimatedYes !== null
                  ? `Estimated YES per other outcome: ${estimatedYes.toFixed(2)}`
                  : "Enter an amount to preview conversion."}
              </p>
            </div>

            <div className="rounded-md border border-border/60 bg-muted/10 p-3 text-[10px] font-mono text-muted-foreground">
              <p className="uppercase tracking-wide">Conversion Path</p>
              <p className="mt-1">
                Uses NegRiskAdapter at{" "}
                <span className="text-foreground">
                  {NEG_RISK_ADAPTER_ADDR}
                </span>
              </p>
              <p className="mt-1">
                Market ID:{" "}
                <span className="text-foreground">
                  {marketId ?? "Unavailable"}
                </span>
              </p>
            </div>

            {(localError || error) && (
              <div className="flex items-center gap-2 rounded border border-destructive/30 bg-destructive/5 p-3 text-destructive">
                <AlertCircle className="h-4 w-4" />
                <p className="text-xs font-mono">
                  {localError || error}
                </p>
              </div>
            )}
            {status && (
              <p className="text-xs font-mono text-muted-foreground">
                {status}
              </p>
            )}
            {txHash && (
              <a
                href={`https://polygonscan.com/tx/${txHash}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 text-xs text-primary"
              >
                View on PolygonScan
                <ExternalLink className="h-3 w-3" />
              </a>
            )}

            <div className="flex flex-col gap-2 sm:flex-row">
              <Button
                type="button"
                variant="secondary"
                className={cn("flex-1 font-mono text-xs uppercase tracking-widest", ctfApproved && "opacity-60")}
                disabled={ctfApproved || isSubmitting}
                onClick={approveConditionalTokens}
              >
                {isSubmitting && !ctfApproved ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : ctfApproved ? (
                  "Approved"
                ) : (
                  "Approve"
                )}
              </Button>
              <Button
                type="button"
                className="flex-1 font-mono text-xs uppercase tracking-widest"
                disabled={!canConvert}
                onClick={handleConvert}
              >
                {isSubmitting && ctfApproved ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  "Convert"
                )}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
