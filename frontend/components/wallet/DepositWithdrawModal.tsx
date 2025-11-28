/**
 * @description
 * Modal component for depositing and withdrawing USDC to/from the vault address.
 * Provides a clean interface for users to manage their funds.
 */

"use client";

import { useCallback, useEffect, useState } from "react";
import { useAuth } from "@clerk/nextjs";
import { Copy, Check, ExternalLink, Wallet, ArrowDown, ArrowUp } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useWallet } from "@/hooks/useWallet";
import { useBalance } from "@/hooks/useBalance";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";

interface DepositWithdrawModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type TabType = "deposit" | "withdraw";

export function DepositWithdrawModal({
  open,
  onOpenChange,
}: DepositWithdrawModalProps) {
  const [activeTab, setActiveTab] = useState<TabType>("deposit");
  const [copied, setCopied] = useState(false);
  const [withdrawAddress, setWithdrawAddress] = useState("");
  const [withdrawAmount, setWithdrawAmount] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { getToken } = useAuth();
  const { vaultAddress } = useWallet();
  const { data: balanceData, refetch } = useBalance();

  const [depositData, setDepositData] = useState<{
    vault_address: string;
    network: string;
    token: string;
    token_address: string;
  } | null>(null);

  const fetchDepositInfo = useCallback(async () => {
    try {
      const token = await getToken();
      if (!token) return;

      const { data } = await api.get("/wallet/deposit", {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });
      setDepositData(data);
    } catch (error: any) {
      console.error("Failed to fetch deposit info:", error);
      setError("Failed to load deposit information");
    }
  }, [getToken]);

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error("Failed to copy:", err);
    }
  };

  const handleWithdraw = async () => {
    if (!withdrawAddress || !withdrawAmount) {
      setError("Please fill in all fields");
      return;
    }

    // Basic validation
    if (!/^0x[a-fA-F0-9]{40}$/.test(withdrawAddress)) {
      setError("Invalid Ethereum address");
      return;
    }

    const amount = parseFloat(withdrawAmount);
    if (isNaN(amount) || amount <= 0) {
      setError("Invalid amount");
      return;
    }

    try {
      setIsSubmitting(true);
      setError(null);

      const token = await getToken();
      if (!token) {
        setError("Authentication required");
        return;
      }

      // Convert amount to USDC units (6 decimals)
      const amountInUnits = Math.floor(amount * 1000000).toString();

      await api.post(
        "/wallet/withdraw",
        {
          to_address: withdrawAddress,
          amount: amountInUnits,
        },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        }
      );

      // Reset form
      setWithdrawAddress("");
      setWithdrawAmount("");
      await refetch();
      onOpenChange(false);
    } catch (error: any) {
      const errorMessage =
        error.response?.data?.error || "Failed to initiate withdrawal";
      setError(errorMessage);
    } finally {
      setIsSubmitting(false);
    }
  };

  useEffect(() => {
    if (open && activeTab === "deposit" && !depositData) {
      fetchDepositInfo();
    }
  }, [activeTab, depositData, fetchDepositInfo, open]);

  useEffect(() => {
    setDepositData(null);
  }, [vaultAddress]);

  const truncateAddress = (address: string) =>
    `${address.slice(0, 6)}...${address.slice(-4)}`;

  const polygonScanUrl = (address: string) =>
    `https://polygonscan.com/address/${address}`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Wallet className="h-5 w-5" />
            Manage Funds
          </DialogTitle>
          <DialogDescription>
            Deposit or withdraw USDC from your vault
          </DialogDescription>
        </DialogHeader>

        {/* Tabs */}
        <div className="flex gap-2 border-b border-border">
          <button
            onClick={() => {
              setActiveTab("deposit");
              setError(null);
            }}
            className={cn(
              "flex-1 py-2 text-sm font-medium transition-colors border-b-2",
              activeTab === "deposit"
                ? "border-primary text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            <div className="flex items-center justify-center gap-2">
              <ArrowDown className="h-4 w-4" />
              Deposit
            </div>
          </button>
          <button
            onClick={() => {
              setActiveTab("withdraw");
              setError(null);
            }}
            className={cn(
              "flex-1 py-2 text-sm font-medium transition-colors border-b-2",
              activeTab === "withdraw"
                ? "border-primary text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            <div className="flex items-center justify-center gap-2">
              <ArrowUp className="h-4 w-4" />
              Withdraw
            </div>
          </button>
        </div>

        {/* Content */}
        <div className="space-y-4">
          {activeTab === "deposit" ? (
            <div className="space-y-4">
              {!vaultAddress ? (
                <div className="rounded-md border border-border bg-muted/50 p-4 text-center text-sm text-muted-foreground">
                  Please connect a wallet to get your deposit address
                </div>
              ) : depositData ? (
                <>
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Vault Address</label>
                    <div className="flex items-center gap-2 rounded-md border border-border bg-background p-3">
                      <code className="flex-1 font-mono text-xs">
                        {depositData.vault_address}
                      </code>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleCopy(depositData.vault_address)}
                        className="h-8 w-8 p-0"
                      >
                        {copied ? (
                          <Check className="h-4 w-4 text-green-500" />
                        ) : (
                          <Copy className="h-4 w-4" />
                        )}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() =>
                          window.open(
                            polygonScanUrl(depositData.vault_address),
                            "_blank"
                          )
                        }
                        className="h-8 w-8 p-0"
                      >
                        <ExternalLink className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>

                  <div className="rounded-md border border-border bg-muted/50 p-4 space-y-2 text-sm">
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Network:</span>
                      <span className="font-mono uppercase">
                        {depositData.network}
                      </span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Token:</span>
                      <span className="font-mono">{depositData.token}</span>
                    </div>
                    <div className="pt-2 border-t border-border">
                      <p className="text-xs text-muted-foreground">
                        Send USDC to this address on Polygon. Your funds will be
                        available in your vault for trading.
                      </p>
                    </div>
                  </div>
                </>
              ) : (
                <div className="rounded-md border border-border bg-muted/50 p-4 text-center text-sm text-muted-foreground">
                  Loading deposit information...
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-4">
              {!vaultAddress ? (
                <div className="rounded-md border border-border bg-muted/50 p-4 text-center text-sm text-muted-foreground">
                  Please connect a wallet to withdraw funds
                </div>
              ) : (
                <>
                  <div className="rounded-md border border-border bg-muted/50 p-4 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">
                        Available Balance:
                      </span>
                      <span className="font-mono font-semibold">
                        {balanceData?.balance_formatted || "0.00"} USDC
                      </span>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <label className="text-sm font-medium">
                      Destination Address
                    </label>
                    <input
                      type="text"
                      value={withdrawAddress}
                      onChange={(e) => {
                        setWithdrawAddress(e.target.value);
                        setError(null);
                      }}
                      placeholder="0x..."
                      className="w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                    />
                  </div>

                  <div className="space-y-2">
                    <label className="text-sm font-medium">Amount (USDC)</label>
                    <input
                      type="number"
                      value={withdrawAmount}
                      onChange={(e) => {
                        setWithdrawAmount(e.target.value);
                        setError(null);
                      }}
                      placeholder="0.00"
                      step="0.01"
                      min="0"
                      className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                    />
                  </div>

                  {error && (
                    <div className="rounded-md border border-red-500/50 bg-red-500/10 p-3 text-sm text-red-500">
                      {error}
                    </div>
                  )}

                  <Button
                    onClick={handleWithdraw}
                    disabled={isSubmitting || !withdrawAddress || !withdrawAmount}
                    className="w-full"
                  >
                    {isSubmitting ? "Processing..." : "Withdraw"}
                  </Button>

                  <p className="text-xs text-muted-foreground text-center">
                    Note: Withdrawal requires signing a Safe transaction. This
                    feature is currently in development.
                  </p>
                </>
              )}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

