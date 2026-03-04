/**
 * @description
 * Modal component for depositing and withdrawing USDC to/from the vault address.
 * Provides a clean interface for users to manage their funds.
 */

"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Copy, Check, ExternalLink, Wallet, ArrowDown, ArrowUp } from "lucide-react";
import { useAccount, useSignMessage, useSwitchChain } from "wagmi";
import { polygon } from "viem/chains";
import { encodeFunctionData, encodePacked, hashTypedData, hexToBigInt, Hex } from "viem";
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
import { ensurePolygonChain } from "@/lib/chain-utils";
import { cn } from "@/lib/utils";

interface DepositWithdrawModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type TabType = "deposit" | "withdraw";

const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";
const FALLBACK_USDC = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174";

const splitAndPackSig = (sig: `0x${string}`): `0x${string}` => {
  let v = parseInt(sig.slice(-2), 16);
  switch (v) {
    case 0:
    case 1:
      v += 31;
      break;
    case 27:
    case 28:
      v += 4;
      break;
    default:
      throw new Error("Invalid signature v");
  }

  const normalizedSig = `${sig.slice(0, -2)}${v.toString(16).padStart(2, "0")}` as `0x${string}`;
  const r = hexToBigInt(`0x${normalizedSig.slice(2, 66)}` as Hex);
  const s = hexToBigInt(`0x${normalizedSig.slice(66, 130)}` as Hex);
  const packedV = Number(hexToBigInt(`0x${normalizedSig.slice(130, 132)}` as Hex));
  return encodePacked(["uint256", "uint256", "uint8"], [r, s, packedV]);
};

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

  const { vaultAddress } = useWallet();
  const { data: balanceData, refetch } = useBalance();
  const { chainId } = useAccount();
  const { switchChainAsync } = useSwitchChain();
  const { signMessageAsync } = useSignMessage();
  const chainIdRef = useRef<number | undefined>(chainId);

  const [depositData, setDepositData] = useState<{
    vault_address: string;
    network: string;
    token: string;
    token_address: string;
  } | null>(null);

  const fetchDepositInfo = useCallback(async () => {
    try {
      const { data } = await api.get("/wallet/deposit");
      setDepositData(data);
    } catch (error: any) {
      console.error("Failed to fetch deposit info:", error);
      setError("Failed to load deposit information");
    }
  }, []);

  useEffect(() => {
    chainIdRef.current = chainId;
  }, [chainId]);

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
    if (!vaultAddress || !/^0x[a-fA-F0-9]{40}$/.test(vaultAddress)) {
      setError("Vault address unavailable. Reconnect wallet and try again.");
      return;
    }

    try {
      setIsSubmitting(true);
      setError(null);

      // Convert amount to USDC units (6 decimals)
      const amountInUnits = Math.floor(amount * 1000000).toString();
      const tokenAddress =
        depositData?.token_address ||
        balanceData?.token_address ||
        FALLBACK_USDC;

      if (!/^0x[a-fA-F0-9]{40}$/.test(tokenAddress)) {
        throw new Error("Invalid collateral token address");
      }

      const transferData = encodeFunctionData({
        abi: [
          {
            type: "function",
            name: "transfer",
            stateMutability: "nonpayable",
            inputs: [
              { name: "to", type: "address" },
              { name: "value", type: "uint256" },
            ],
            outputs: [{ name: "", type: "bool" }],
          },
        ],
        functionName: "transfer",
        args: [withdrawAddress as `0x${string}`, BigInt(amountInUnits)],
      });

      const nonceResp = await api.get("/wallet/withdraw/nonce");
      const nonce = String(nonceResp?.data?.nonce || "").trim();
      if (!nonce) {
        throw new Error("Unable to fetch relayer nonce");
      }

      await ensurePolygonChain(() => chainIdRef.current, switchChainAsync);

      const safeTxPayload = {
        domain: {
          chainId: BigInt(polygon.id),
          verifyingContract: vaultAddress as `0x${string}`,
        },
        types: {
          SafeTx: [
            { name: "to", type: "address" },
            { name: "value", type: "uint256" },
            { name: "data", type: "bytes" },
            { name: "operation", type: "uint8" },
            { name: "safeTxGas", type: "uint256" },
            { name: "baseGas", type: "uint256" },
            { name: "gasPrice", type: "uint256" },
            { name: "gasToken", type: "address" },
            { name: "refundReceiver", type: "address" },
            { name: "nonce", type: "uint256" },
          ],
        } as const,
        primaryType: "SafeTx" as const,
        message: {
          to: tokenAddress as `0x${string}`,
          value: BigInt(0),
          data: transferData,
          operation: 0,
          safeTxGas: BigInt(0),
          baseGas: BigInt(0),
          gasPrice: BigInt(0),
          gasToken: ZERO_ADDRESS as `0x${string}`,
          refundReceiver: ZERO_ADDRESS as `0x${string}`,
          nonce: BigInt(nonce),
        },
      };

      const safeTxHash = hashTypedData({
        domain: safeTxPayload.domain,
        types: safeTxPayload.types,
        primaryType: safeTxPayload.primaryType,
        message: safeTxPayload.message,
      });

      const signature = await signMessageAsync({
        message: { raw: safeTxHash },
      });

      const packedSignature = splitAndPackSig(signature);

      await api.post(
        "/wallet/withdraw",
        {
          to_address: withdrawAddress,
          amount: amountInUnits,
          safe_tx_to: tokenAddress,
          safe_tx_data: transferData,
          nonce,
          signature: packedSignature,
          operation: "0",
          safe_txn_gas: "0",
          base_gas: "0",
          gas_price: "0",
          gas_token: ZERO_ADDRESS,
          refund_receiver: ZERO_ADDRESS,
          metadata: "bankai:withdraw",
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
                    Withdrawal is signed as a SafeTx and submitted to the
                    Polymarket relayer.
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
