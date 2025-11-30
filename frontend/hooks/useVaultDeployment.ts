/**
 * @description
 * Hook that orchestrates the Polymarket SAFE-CREATE deployment flow:
 * 1. Fetch typed data from the backend
 * 2. Ask the connected wallet to sign it (EIP-712)
 * 3. POST the signature back to the backend so it can call the relayer
 *
 * @dependencies
 * - @clerk/nextjs (for auth token)
 * - wagmi (for signTypedData)
 * - axios api helper
 */

"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "@clerk/nextjs";
import { polygon } from "viem/chains";
import { useAccount, useSignTypedData, useSwitchChain } from "wagmi";

import { api } from "@/lib/api";
import type { SafeCreateTypedData, VaultDeploymentResult } from "@/types/vault";

interface UseVaultDeploymentArgs {
  eoaAddress: string | null;
  hasVault: boolean;
  isReady: boolean;
  refreshUser: () => Promise<void>;
}

interface UseVaultDeploymentResult {
  canDeploy: boolean;
  isDeploying: boolean;
  deployError: string | null;
  deploymentStatus: VaultDeploymentResult | null;
  deployVault: () => Promise<void>;
}

export function useVaultDeployment({
  eoaAddress,
  hasVault,
  isReady,
  refreshUser,
}: UseVaultDeploymentArgs): UseVaultDeploymentResult {
  const { getToken } = useAuth();
  const { signTypedDataAsync } = useSignTypedData();
  const { chainId: walletChainId } = useAccount();
  const { switchChainAsync } = useSwitchChain();

  const [typedData, setTypedData] = useState<SafeCreateTypedData | null>(null);
  const [typedDataOwner, setTypedDataOwner] = useState<string | null>(null);
  const [isDeploying, setIsDeploying] = useState(false);
  const [deployError, setDeployError] = useState<string | null>(null);
  const [deploymentStatus, setDeploymentStatus] =
    useState<VaultDeploymentResult | null>(null);
  const autoTriggeredRef = useRef(false);

  const fetchTypedData = useCallback(async () => {
    const token = await getToken();
    if (!token) {
      throw new Error("Unable to fetch auth token");
    }
    const { data } = await api.get<{
      owner: string;
      typed_data: SafeCreateTypedData;
    }>("/wallet/deploy/typed-data", {
      headers: { Authorization: `Bearer ${token}` },
    });
    setTypedData(data.typed_data);
    setTypedDataOwner(data.owner.toLowerCase());
    return data.typed_data;
  }, [getToken]);

  const deployVault = useCallback(async () => {
    if (!eoaAddress || hasVault || !isReady) {
      return;
    }

    try {
      setIsDeploying(true);
      setDeployError(null);

      const token = await getToken();
      if (!token) {
        throw new Error("Unable to fetch auth token");
      }

      let payload = typedData;
      const ownerMismatch =
        typedDataOwner &&
        eoaAddress &&
        typedDataOwner !== eoaAddress.toLowerCase();

      if (!payload || ownerMismatch) {
        payload = await fetchTypedData();
      }

      if (!payload) {
        throw new Error("Failed to load deployment payload");
      }

      if (walletChainId !== polygon.id) {
        if (switchChainAsync) {
          await switchChainAsync({ chainId: polygon.id });
          return; // wait for chain change before continuing flow
        }
        throw new Error("Switch wallet to Polygon before deploying");
      }

      const signature = await signTypedDataAsync({
        domain: payload.domain,
        types: payload.types as any,
        primaryType: payload.primaryType as any,
        message: payload.message,
      });

      const { data } = await api.post<VaultDeploymentResult>(
        "/wallet/deploy",
        {
          signature,
          metadata: "bankai:vault-deploy",
        },
        {
          headers: { Authorization: `Bearer ${token}` },
        }
      );

      setDeploymentStatus(data);
      await refreshUser();
    } catch (error: any) {
      const message =
        error?.response?.data?.error || error?.message || "Deployment failed";
      setDeployError(message);
    } finally {
      setIsDeploying(false);
    }
  }, [
    eoaAddress,
    fetchTypedData,
    getToken,
    hasVault,
    isReady,
    refreshUser,
    signTypedDataAsync,
    switchChainAsync,
    typedData,
  ]);

  const canDeploy = useMemo(
    () => Boolean(eoaAddress) && !hasVault && isReady,
    [eoaAddress, hasVault, isReady]
  );

  useEffect(() => {
    autoTriggeredRef.current = false;
    setTypedData(null);
    setTypedDataOwner(null);
    setDeploymentStatus(null);
    setDeployError(null);
  }, [eoaAddress]);

  useEffect(() => {
    if (!canDeploy) {
      autoTriggeredRef.current = false;
      return;
    }
    if (!isDeploying && !autoTriggeredRef.current) {
      autoTriggeredRef.current = true;
      deployVault();
    }
  }, [canDeploy, deployVault, isDeploying]);

  return {
    canDeploy,
    isDeploying,
    deployError,
    deploymentStatus,
    deployVault,
  };
}

