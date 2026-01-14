"use client";

/**
 * @description
 * UI for CTF split/merge/redeem operations on Conditional Tokens.
 */

import { useMemo, useState } from "react";
import { ethers } from "ethers";
import { AlertCircle, ExternalLink, Loader2, ShieldAlert } from "lucide-react";
import { useAccount, useSwitchChain, useWalletClient } from "wagmi";

import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ensurePolygonChain } from "@/lib/chain-utils";
import {
  COLLATERAL_TOKEN_ADDR,
  CTF_CONTRACT_ADDR,
  MAX_ALLOWANCE,
  USDC_DECIMALS,
} from "@/lib/polymarket";
import { walletClientToEthersSigner } from "@/lib/ethers-adapter";
import { useWallet } from "@/hooks/useWallet";
import ConditionalTokensAbi from "@/lib/abi/ConditionalTokens.json";

type Mode = "split" | "merge" | "redeem";

interface MergeSplitFormProps {
  conditionIds: string[];
}

const ERC20_ABI = [
  {
    constant: true,
    inputs: [
      { name: "owner", type: "address" },
      { name: "spender", type: "address" },
    ],
    name: "allowance",
    outputs: [{ name: "", type: "uint256" }],
    stateMutability: "view",
    type: "function",
  },
  {
    constant: false,
    inputs: [
      { name: "spender", type: "address" },
      { name: "amount", type: "uint256" },
    ],
    name: "approve",
    outputs: [{ name: "", type: "bool" }],
    stateMutability: "nonpayable",
    type: "function",
  },
];

const isValidConditionId = (value: string) => /^0x[a-fA-F0-9]{64}$/.test(value);

const parseEthersError = (error: unknown) => {
  if (!error || typeof error !== "object") {
    return "Transaction failed. Please try again.";
  }
  const err = error as { reason?: string; message?: string; data?: { message?: string } };
  return err.reason || err.data?.message || err.message || "Transaction failed.";
};

export function MergeSplitForm({ conditionIds }: MergeSplitFormProps) {
  const { data: walletClient } = useWalletClient();
  const { address, chainId } = useAccount();
  const { switchChainAsync } = useSwitchChain();
  const { user } = useWallet();

  const [mode, setMode] = useState<Mode>("split");
  const [conditionId, setConditionId] = useState("");
  const [amount, setAmount] = useState("");
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const conditionOptions = useMemo(
    () => Array.from(new Set(conditionIds)).filter(Boolean),
    [conditionIds]
  );

  const helperText = useMemo(() => {
    if (mode === "split") {
      return "Split converts USDC collateral into YES/NO outcome tokens.";
    }
    if (mode === "merge") {
      return "Merge burns complete YES/NO sets back into USDC.";
    }
    return "Redeem burns resolved outcome tokens for their USDC payout.";
  }, [mode]);

  const handleSubmit = async () => {
    setError(null);
    setStatus(null);
    setTxHash(null);

    if (!walletClient || !address) {
      setError("Connect a wallet to execute CTF operations.");
      return;
    }

    if (!isValidConditionId(conditionId)) {
      setError("Enter a valid condition ID (32-byte hex string).");
      return;
    }

    if (mode !== "redeem") {
      const parsed = Number(amount);
      if (!Number.isFinite(parsed) || parsed <= 0) {
        setError("Enter a valid amount in USDC.");
        return;
      }
    }

    setIsSubmitting(true);
    try {
      await ensurePolygonChain(() => chainId, switchChainAsync);

      const signer = walletClientToEthersSigner(walletClient);
      const ctf = new ethers.Contract(
        CTF_CONTRACT_ADDR,
        ConditionalTokensAbi,
        signer
      );
      const collateral = new ethers.Contract(
        COLLATERAL_TOKEN_ADDR,
        ERC20_ABI,
        signer
      );

      const partition = [1, 2];
      const parentCollectionId = ethers.constants.HashZero;

      if (mode === "split") {
        const amountUnits = ethers.utils.parseUnits(amount, USDC_DECIMALS);
        if (amountUnits.lte(0)) {
          setError("Amount must be greater than zero.");
          return;
        }
        const allowance = await collateral.allowance(address, CTF_CONTRACT_ADDR);
        if (allowance.lt(amountUnits)) {
          setStatus("Approving USDC collateral...");
          const approvalTx = await collateral.approve(
            CTF_CONTRACT_ADDR,
            MAX_ALLOWANCE
          );
          await approvalTx.wait(1);
        }

        setStatus("Splitting collateral into outcome tokens...");
        const tx = await ctf.splitPosition(
          COLLATERAL_TOKEN_ADDR,
          parentCollectionId,
          conditionId,
          partition,
          amountUnits
        );
        setTxHash(tx.hash);
        await tx.wait(1);
        setStatus("Split complete.");
      }

      if (mode === "merge") {
        const amountUnits = ethers.utils.parseUnits(amount, USDC_DECIMALS);
        if (amountUnits.lte(0)) {
          setError("Amount must be greater than zero.");
          return;
        }
        setStatus("Merging outcome tokens into USDC...");
        const tx = await ctf.mergePositions(
          COLLATERAL_TOKEN_ADDR,
          parentCollectionId,
          conditionId,
          partition,
          amountUnits
        );
        setTxHash(tx.hash);
        await tx.wait(1);
        setStatus("Merge complete.");
      }

      if (mode === "redeem") {
        setStatus("Redeeming resolved outcome tokens...");
        const tx = await ctf.redeemPositions(
          COLLATERAL_TOKEN_ADDR,
          parentCollectionId,
          conditionId,
          partition
        );
        setTxHash(tx.hash);
        await tx.wait(1);
        setStatus("Redeem complete.");
      }
    } catch (err) {
      setError(parseEthersError(err));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Card className="border-border/60 bg-card/70">
      <CardHeader className="pb-3">
        <div>
          <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
            CTF Operations
          </p>
          <h3 className="text-lg font-semibold text-foreground">Merge / Split / Redeem</h3>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-0">
        {user?.wallet_type && (
          <div className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs text-amber-200">
            <ShieldAlert className="mt-0.5 h-4 w-4" />
            <span>
              CTF operations execute from your connected wallet. If you trade via a
              vault ({user.wallet_type.toLowerCase()}), move collateral to the
              connected wallet before using these actions.
            </span>
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          {(["split", "merge", "redeem"] as Mode[]).map((option) => (
            <Button
              key={option}
              variant={mode === option ? "default" : "outline"}
              size="sm"
              className="capitalize"
              onClick={() => {
                setMode(option);
                setStatus(null);
                setError(null);
              }}
            >
              {option}
            </Button>
          ))}
        </div>

        <div className="space-y-2">
          <label className="text-xs font-medium uppercase tracking-[0.2em] text-muted-foreground">
            Condition ID
          </label>
          <Input
            value={conditionId}
            onChange={(event) => setConditionId(event.target.value.trim())}
            placeholder="0x..."
            list="condition-id-options"
            className="font-mono text-xs"
          />
          <datalist id="condition-id-options">
            {conditionOptions.map((id) => (
              <option key={id} value={id} />
            ))}
          </datalist>
          <p className="text-xs text-muted-foreground">{helperText}</p>
        </div>

        {mode !== "redeem" && (
          <div className="space-y-2">
            <label className="text-xs font-medium uppercase tracking-[0.2em] text-muted-foreground">
              Amount (USDC)
            </label>
            <Input
              value={amount}
              onChange={(event) => setAmount(event.target.value)}
              placeholder="0.00"
              inputMode="decimal"
            />
          </div>
        )}

        {error && (
          <div className="flex items-center gap-2 text-sm text-destructive">
            <AlertCircle className="h-4 w-4" />
            {error}
          </div>
        )}
        {status && <div className="text-sm text-muted-foreground">{status}</div>}
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

        <Button
          onClick={handleSubmit}
          disabled={isSubmitting}
          className="w-full"
        >
          {isSubmitting ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" />
              Processing
            </>
          ) : (
            "Execute"
          )}
        </Button>
      </CardContent>
    </Card>
  );
}
