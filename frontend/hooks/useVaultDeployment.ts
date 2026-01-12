/**
 * @description
 * Hook that orchestrates the Polymarket SAFE-CREATE deployment flow:
 * 1. Fetch typed data from the backend
 * 2. Ask the connected wallet to sign it (EIP-712)
 * 3. POST the signature back to the backend so it can call the relayer
 *
 * @dependencies
 * - wagmi (for signTypedData)
 * - axios api helper
 */

"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { polygon } from "viem/chains";
import { useAccount, useSignTypedData, useSwitchChain } from "wagmi";

import { api } from "@/lib/api";
import { ensurePolygonChain } from "@/lib/chain-utils";
import type { SafeCreateTypedData, VaultDeploymentResult } from "@/types/vault";

interface UseVaultDeploymentArgs {
  eoaAddress: string | null;
  hasVault: boolean;
  isReady: boolean;
  refreshUser: () => Promise<void>;
}

type DeploymentStep =
  | "idle"
  | "preparing"
  | "fetchingPayload"
  | "checkingNetwork"
  | "switchingNetwork"
  | "awaitingSignature"
  | "submitting"
  | "pollingRelayer";

interface UseVaultDeploymentResult {
  canDeploy: boolean;
  isDeploying: boolean;
  deployError: string | null;
  deploymentStatus: VaultDeploymentResult | null;
  deploymentStep: DeploymentStep;
  deployVault: () => Promise<void>;
}

export function useVaultDeployment({
  eoaAddress,
  hasVault,
  isReady,
  refreshUser,
}: UseVaultDeploymentArgs): UseVaultDeploymentResult {
  const { signTypedDataAsync } = useSignTypedData();
  const { chainId: walletChainId } = useAccount();
  const { switchChainAsync } = useSwitchChain();

  const [typedData, setTypedData] = useState<SafeCreateTypedData | null>(null);
  const [typedDataOwner, setTypedDataOwner] = useState<string | null>(null);
  const [isDeploying, setIsDeploying] = useState(false);
  const [deployError, setDeployError] = useState<string | null>(null);
  const [deploymentStatus, setDeploymentStatus] =
    useState<VaultDeploymentResult | null>(null);
  const [deploymentStep, setDeploymentStep] =
    useState<DeploymentStep>("idle");
  const chainIdRef = useRef<number | undefined>(walletChainId);

  useEffect(() => {
    chainIdRef.current = walletChainId;
  }, [walletChainId]);

  const fetchTypedData = useCallback(async () => {
    const { data } = await api.get<{
      owner: string;
      typed_data: SafeCreateTypedData;
    }>("/wallet/deploy/typed-data", {
      withCredentials: true,
    });
    setTypedData(data.typed_data);
    setTypedDataOwner(data.owner.toLowerCase());
    return data.typed_data;
  }, []);

  const deployVault = useCallback(async () => {
    if (!eoaAddress || hasVault || !isReady) {
      return;
    }

    try {
      setIsDeploying(true);
      setDeployError(null);
      setDeploymentStatus(null);
      setDeploymentStep("preparing");

      let payload = typedData;
      const ownerMismatch =
        typedDataOwner &&
        eoaAddress &&
        typedDataOwner !== eoaAddress.toLowerCase();

      if (!payload || ownerMismatch) {
        setDeploymentStep("fetchingPayload");
        payload = await fetchTypedData();
      }

      if (!payload) {
        throw new Error("Failed to load deployment payload");
      }

      setDeploymentStep("checkingNetwork");
      if (chainIdRef.current && chainIdRef.current !== polygon.id) {
        setDeploymentStep("switchingNetwork");
      }
      await ensurePolygonChain(
        () => chainIdRef.current,
        switchChainAsync
      );

      setDeploymentStep("awaitingSignature");
      const signature = await signTypedDataAsync({
        domain: payload.domain,
        types: payload.types as any,
        primaryType: payload.primaryType as any,
        message: payload.message,
      });

      setDeploymentStep("submitting");
      const { data } = await api.post<VaultDeploymentResult>(
        "/wallet/deploy",
        {
          signature,
          metadata: "bankai:vault-deploy",
        }
      );

      setDeploymentStatus(data);
      setDeploymentStep("pollingRelayer");
      await refreshUser();
    } catch (error: any) {
      const message =
        error?.response?.data?.error || error?.message || "Deployment failed";
      setDeployError(message);
      setDeploymentStep("idle");
    } finally {
      setIsDeploying(false);
    }
  }, [
    eoaAddress,
    fetchTypedData,
    hasVault,
    isReady,
    refreshUser,
    signTypedDataAsync,
    switchChainAsync,
    typedData,
    typedDataOwner,
  ]);

  const canDeploy = useMemo(
    () => Boolean(eoaAddress) && !hasVault && isReady,
    [eoaAddress, hasVault, isReady]
  );

  useEffect(() => {
    setTypedData(null);
    setTypedDataOwner(null);
    setDeploymentStatus(null);
    setDeployError(null);
    setDeploymentStep("idle");
  }, [eoaAddress]);

  useEffect(() => {
    if (hasVault) {
      setDeploymentStep("idle");
      setDeployError(null);
    }
  }, [hasVault]);

  return {
    canDeploy,
    isDeploying,
    deployError,
    deploymentStatus,
    deploymentStep,
    deployVault,
  };
}
