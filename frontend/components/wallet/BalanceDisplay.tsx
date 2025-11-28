/**
 * @description
 * Compact balance display component showing USDC balance in the vault.
 * Designed to be minimal and professional.
 */

"use client";

import { Wallet } from "lucide-react";
import { useBalance } from "@/hooks/useBalance";
import { cn } from "@/lib/utils";

interface BalanceDisplayProps {
  className?: string;
  showIcon?: boolean;
}

export function BalanceDisplay({ className, showIcon = true }: BalanceDisplayProps) {
  const { data: balanceData, isLoading } = useBalance();

  if (isLoading) {
    return (
      <div
        className={cn(
          "flex items-center gap-2 rounded-md border border-border bg-card/50 px-3 py-1.5",
          className
        )}
      >
        {showIcon && <Wallet className="h-3.5 w-3.5 animate-pulse text-muted-foreground" />}
        <span className="font-mono text-xs text-muted-foreground animate-pulse">
          Loading...
        </span>
      </div>
    );
  }

  if (!balanceData || !balanceData.vault_address) {
    return (
      <div
        className={cn(
          "flex items-center gap-2 rounded-md border border-border bg-card/50 px-3 py-1.5",
          className
        )}
      >
        {showIcon && (
          <Wallet className="h-3.5 w-3.5 text-muted-foreground" />
        )}
        <div className="flex flex-col">
          <span className="font-mono text-xs text-muted-foreground">
            Vault pending
          </span>
          <span className="text-[10px] uppercase text-muted-foreground">
            Balance unavailable
          </span>
        </div>
      </div>
    );
  }

  const balanceFormatted =
    balanceData.balance_formatted ??
    (Number(balanceData.balance ?? "0") / 1_000_000).toFixed(2);

  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-md border border-border bg-card/50 px-3 py-1.5",
        className
      )}
      title={`Balance: ${balanceFormatted} USDC`}
    >
      {showIcon && (
        <Wallet className="h-3.5 w-3.5 text-muted-foreground" />
      )}
      <div className="flex items-baseline gap-1">
        <span className="font-mono text-xs font-semibold text-foreground">
          {balanceFormatted}
        </span>
        <span className="text-[10px] uppercase text-muted-foreground">
          USDC
        </span>
      </div>
    </div>
  );
}

