"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useAccount, usePublicClient, useSwitchChain, useWriteContract } from "wagmi";
import { parseUnits } from "viem";

import { ensurePolygonChain } from "@/lib/chain-utils";
import {
  COLLATERAL_TOKEN_ADDR,
  CTF_CONTRACT_ADDR,
  MAX_ALLOWANCE,
  NEG_RISK_ADAPTER_ADDR,
  USDC_DECIMALS,
} from "@/lib/polymarket";

const ZERO_BYTES32 = `0x${"0".repeat(64)}`;

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
] as const;

const ERC1155_ABI = [
  {
    inputs: [
      { internalType: "address", name: "account", type: "address" },
      { internalType: "address", name: "operator", type: "address" },
    ],
    name: "isApprovedForAll",
    outputs: [{ internalType: "bool", name: "", type: "bool" }],
    stateMutability: "view",
    type: "function",
  },
  {
    inputs: [
      { internalType: "address", name: "operator", type: "address" },
      { internalType: "bool", name: "approved", type: "bool" },
    ],
    name: "setApprovalForAll",
    outputs: [],
    stateMutability: "nonpayable",
    type: "function",
  },
] as const;

const NEG_RISK_ADAPTER_ABI = [
  {
    inputs: [
      { internalType: "address", name: "collateralToken", type: "address" },
      { internalType: "bytes32", name: "parentCollectionId", type: "bytes32" },
      { internalType: "bytes32", name: "conditionId", type: "bytes32" },
      { internalType: "uint256[]", name: "partition", type: "uint256[]" },
      { internalType: "uint256", name: "amount", type: "uint256" },
    ],
    name: "splitPosition",
    outputs: [],
    stateMutability: "nonpayable",
    type: "function",
  },
  {
    inputs: [
      { internalType: "address", name: "collateralToken", type: "address" },
      { internalType: "bytes32", name: "parentCollectionId", type: "bytes32" },
      { internalType: "bytes32", name: "conditionId", type: "bytes32" },
      { internalType: "uint256[]", name: "partition", type: "uint256[]" },
      { internalType: "uint256", name: "amount", type: "uint256" },
    ],
    name: "mergePositions",
    outputs: [],
    stateMutability: "nonpayable",
    type: "function",
  },
  {
    inputs: [
      { internalType: "bytes32", name: "marketId", type: "bytes32" },
      { internalType: "uint256", name: "indexSet", type: "uint256" },
      { internalType: "uint256", name: "amount", type: "uint256" },
    ],
    name: "convertPositions",
    outputs: [],
    stateMutability: "nonpayable",
    type: "function",
  },
] as const;

const formatError = (error: unknown) => {
  if (!error || typeof error !== "object") {
    return "Transaction failed. Please try again.";
  }
  const err = error as { shortMessage?: string; message?: string; cause?: { message?: string } };
  return err.shortMessage || err.cause?.message || err.message || "Transaction failed. Please try again.";
};

export function useNegativeRisk() {
  const { address, chainId } = useAccount();
  const { switchChainAsync } = useSwitchChain();
  const publicClient = usePublicClient();
  const { writeContractAsync } = useWriteContract();

  const [collateralAllowance, setCollateralAllowance] = useState<bigint>(0n);
  const [ctfApproved, setCtfApproved] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<`0x${string}` | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const refreshApprovals = useCallback(async () => {
    if (!publicClient || !address) return;
    const [allowance, approved] = await Promise.all([
      publicClient.readContract({
        address: COLLATERAL_TOKEN_ADDR,
        abi: ERC20_ABI,
        functionName: "allowance",
        args: [address, NEG_RISK_ADAPTER_ADDR],
      }),
      publicClient.readContract({
        address: CTF_CONTRACT_ADDR,
        abi: ERC1155_ABI,
        functionName: "isApprovedForAll",
        args: [address, NEG_RISK_ADAPTER_ADDR],
      }),
    ]);
    setCollateralAllowance(allowance as bigint);
    setCtfApproved(Boolean(approved));
  }, [address, publicClient]);

  useEffect(() => {
    void refreshApprovals();
  }, [refreshApprovals]);

  const requireWallet = useCallback(async () => {
    if (!address) {
      throw new Error("Connect a wallet to continue.");
    }
    await ensurePolygonChain(() => chainId, switchChainAsync);
    if (!publicClient) {
      throw new Error("Wallet client not ready. Please reconnect and try again.");
    }
  }, [address, chainId, publicClient, switchChainAsync]);

  const approveCollateral = useCallback(async () => {
    setError(null);
    setStatus("Approving USDC...");
    setIsSubmitting(true);
    setTxHash(null);
    try {
      await requireWallet();
      const hash = await writeContractAsync({
        address: COLLATERAL_TOKEN_ADDR,
        abi: ERC20_ABI,
        functionName: "approve",
        args: [NEG_RISK_ADAPTER_ADDR, BigInt(MAX_ALLOWANCE)],
      });
      setTxHash(hash);
      await publicClient!.waitForTransactionReceipt({ hash });
      setStatus("USDC approved.");
      await refreshApprovals();
    } catch (err) {
      setError(formatError(err));
    } finally {
      setIsSubmitting(false);
    }
  }, [publicClient, refreshApprovals, requireWallet, writeContractAsync]);

  const approveConditionalTokens = useCallback(async () => {
    setError(null);
    setStatus("Approving conditional tokens...");
    setIsSubmitting(true);
    setTxHash(null);
    try {
      await requireWallet();
      const hash = await writeContractAsync({
        address: CTF_CONTRACT_ADDR,
        abi: ERC1155_ABI,
        functionName: "setApprovalForAll",
        args: [NEG_RISK_ADAPTER_ADDR, true],
      });
      setTxHash(hash);
      await publicClient!.waitForTransactionReceipt({ hash });
      setStatus("Conditional tokens approved.");
      await refreshApprovals();
    } catch (err) {
      setError(formatError(err));
    } finally {
      setIsSubmitting(false);
    }
  }, [publicClient, refreshApprovals, requireWallet, writeContractAsync]);

  const splitPosition = useCallback(
    async (conditionId: string, amount: string) => {
      setError(null);
      setStatus("Splitting collateral...");
      setIsSubmitting(true);
      setTxHash(null);
      try {
        await requireWallet();
        const amountUnits = parseUnits(amount, USDC_DECIMALS);
        if (amountUnits <= 0n) {
          throw new Error("Amount must be greater than zero.");
        }
        if (collateralAllowance < amountUnits) {
          throw new Error("Approve USDC before splitting.");
        }
        const hash = await writeContractAsync({
          address: NEG_RISK_ADAPTER_ADDR,
          abi: NEG_RISK_ADAPTER_ABI,
          functionName: "splitPosition",
          args: [
            COLLATERAL_TOKEN_ADDR,
            ZERO_BYTES32,
            conditionId as `0x${string}`,
            [1n, 2n],
            amountUnits,
          ],
        });
        setTxHash(hash);
        await publicClient!.waitForTransactionReceipt({ hash });
        setStatus("Split complete.");
      } catch (err) {
        setError(formatError(err));
      } finally {
        setIsSubmitting(false);
      }
    },
    [
      collateralAllowance,
      publicClient,
      requireWallet,
      writeContractAsync,
    ]
  );

  const mergePositions = useCallback(
    async (conditionId: string, amount: string) => {
      setError(null);
      setStatus("Merging positions...");
      setIsSubmitting(true);
      setTxHash(null);
      try {
        await requireWallet();
        const amountUnits = parseUnits(amount, USDC_DECIMALS);
        if (amountUnits <= 0n) {
          throw new Error("Amount must be greater than zero.");
        }
        if (!ctfApproved) {
          throw new Error("Approve conditional tokens before merging.");
        }
        const hash = await writeContractAsync({
          address: NEG_RISK_ADAPTER_ADDR,
          abi: NEG_RISK_ADAPTER_ABI,
          functionName: "mergePositions",
          args: [
            COLLATERAL_TOKEN_ADDR,
            ZERO_BYTES32,
            conditionId as `0x${string}`,
            [1n, 2n],
            amountUnits,
          ],
        });
        setTxHash(hash);
        await publicClient!.waitForTransactionReceipt({ hash });
        setStatus("Merge complete.");
      } catch (err) {
        setError(formatError(err));
      } finally {
        setIsSubmitting(false);
      }
    },
    [ctfApproved, publicClient, requireWallet, writeContractAsync]
  );

  const convertPositions = useCallback(
    async (marketId: `0x${string}`, indexSet: bigint, amount: string) => {
      setError(null);
      setStatus("Converting positions...");
      setIsSubmitting(true);
      setTxHash(null);
      try {
        await requireWallet();
        const amountUnits = parseUnits(amount, USDC_DECIMALS);
        if (amountUnits <= 0n) {
          throw new Error("Amount must be greater than zero.");
        }
        if (!ctfApproved) {
          throw new Error("Approve conditional tokens before converting.");
        }
        const hash = await writeContractAsync({
          address: NEG_RISK_ADAPTER_ADDR,
          abi: NEG_RISK_ADAPTER_ABI,
          functionName: "convertPositions",
          args: [marketId, indexSet, amountUnits],
        });
        setTxHash(hash);
        await publicClient!.waitForTransactionReceipt({ hash });
        setStatus("Conversion complete.");
      } catch (err) {
        setError(formatError(err));
      } finally {
        setIsSubmitting(false);
      }
    },
    [ctfApproved, publicClient, requireWallet, writeContractAsync]
  );

  const reset = useCallback(() => {
    setStatus(null);
    setError(null);
    setTxHash(null);
    setIsSubmitting(false);
  }, []);

  const needsCollateralApproval = useMemo(
    () => collateralAllowance <= 0n,
    [collateralAllowance]
  );

  return {
    approveCollateral,
    approveConditionalTokens,
    splitPosition,
    mergePositions,
    convertPositions,
    refreshApprovals,
    collateralAllowance,
    ctfApproved,
    needsCollateralApproval,
    status,
    error,
    txHash,
    isSubmitting,
    reset,
  };
}
