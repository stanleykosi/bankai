/**
 * @description
 * Custom hook to bridge Clerk authentication (email users) with Wagmi wallet
 * state (EOA / MetaMask). Responsible for syncing authenticated users with the
 * backend so we always have an internal record + vault address.
 *
 * @dependencies
 * - @clerk/nextjs
 * - wagmi
 * - @tanstack/react-query (indirectly through api hook usage)
 */

"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useAuth, useUser } from "@clerk/nextjs";
import { useAccount, useDisconnect } from "wagmi";

import { api } from "@/lib/api";
import type { User } from "@/types";

export interface UseWalletReturn {
  isAuthenticated: boolean;
  isLoading: boolean;
  user: User | null;
  eoaAddress: string | null;
  vaultAddress: string | null;
  disconnect: () => void;
  refreshUser: () => Promise<void>;
}

export function useWallet(): UseWalletReturn {
  const { user: clerkUser, isLoaded: isClerkLoaded } = useUser();
  const { getToken } = useAuth();
  const { address: wagmiAddress, isConnected: isWagmiConnected } = useAccount();
  const { disconnect: wagmiDisconnect } = useDisconnect();

  const [backendUser, setBackendUser] = useState<User | null>(null);
  const [isSyncing, setIsSyncing] = useState(false);

  const eoaAddress = useMemo(() => {
    if (isWagmiConnected && wagmiAddress) {
      return wagmiAddress;
    }
    if (clerkUser?.primaryWeb3Wallet?.web3Wallet) {
      return clerkUser.primaryWeb3Wallet.web3Wallet;
    }
    return null;
  }, [clerkUser, isWagmiConnected, wagmiAddress]);

  const syncUser = useCallback(async () => {
    if (!clerkUser || !eoaAddress) return;

    try {
      setIsSyncing(true);
      const token = await getToken();

      if (!token) {
        console.warn("No auth token available for sync");
        return;
      }

      const { data } = await api.post<User>(
        "/user/sync",
        {
          email: clerkUser.primaryEmailAddress?.emailAddress,
          eoa_address: eoaAddress,
        },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        }
      );

      setBackendUser(data);
    } catch (error) {
      console.error("Failed to sync user with backend:", error);
    } finally {
      setIsSyncing(false);
    }
  }, [clerkUser, eoaAddress, getToken]);

  useEffect(() => {
    if (!isClerkLoaded || !clerkUser || !eoaAddress) {
      return;
    }

    const alreadySynced =
      backendUser?.eoa_address === eoaAddress && !!backendUser.vault_address;

    if (!alreadySynced) {
      syncUser();
    }
  }, [backendUser?.eoa_address, backendUser?.vault_address, clerkUser, eoaAddress, isClerkLoaded, syncUser]);

  const handleDisconnect = useCallback(() => {
    if (isWagmiConnected) {
      wagmiDisconnect();
    }
    setBackendUser(null);
  }, [isWagmiConnected, wagmiDisconnect]);

  useEffect(() => {
    if (!backendUser) {
      return;
    }

    if (!eoaAddress || backendUser.eoa_address !== eoaAddress) {
      setBackendUser(null);
    }
  }, [backendUser, eoaAddress]);

  return {
    isAuthenticated: !!clerkUser,
    isLoading: !isClerkLoaded || isSyncing,
    user: backendUser,
    eoaAddress,
    vaultAddress: backendUser?.vault_address ?? null,
    disconnect: handleDisconnect,
    refreshUser: syncUser,
  };
}

