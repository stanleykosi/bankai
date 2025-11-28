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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  walletError: string | null;
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
  const [clerkLoadTimeout, setClerkLoadTimeout] = useState(false);
  const [walletError, setWalletError] = useState<string | null>(null);
  const syncInFlightRef = useRef(false);

  // Fallback: if Clerk takes too long to load, assume it's loaded to prevent infinite loading
  useEffect(() => {
    if (!isClerkLoaded) {
      const timer = setTimeout(() => {
        setClerkLoadTimeout(true);
      }, 5000); // 5 second timeout
      return () => clearTimeout(timer);
    } else {
      setClerkLoadTimeout(false);
    }
  }, [isClerkLoaded]);

  const eoaAddress = useMemo(() => {
    if (isWagmiConnected && wagmiAddress) {
      return wagmiAddress;
    }
    if (clerkUser?.primaryWeb3Wallet?.web3Wallet) {
      return clerkUser.primaryWeb3Wallet.web3Wallet;
    }
    return null;
  }, [clerkUser, isWagmiConnected, wagmiAddress]);

  const ensureWallet = useCallback(
    async (token: string) => {
      try {
        const { data } = await api.get<User>("/wallet", {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        });
        setBackendUser(data);
        setWalletError(null);
        return data;
      } catch (error: any) {
        console.error("useWallet: Failed to ensure wallet", error);
        const message =
          error.response?.data?.error ||
          error.message ||
          "Failed to setup vault";
        setWalletError(message);
        return null;
      }
    },
    []
  );

  const syncUser = useCallback(async () => {
    // Sync user even if they don't have a wallet yet - they can connect it later
    if (!clerkUser || syncInFlightRef.current) {
      return;
    }

    try {
      syncInFlightRef.current = true;
      setIsSyncing(true);
      const token = await getToken();

      if (!token) {
        return;
      }

      // Add timeout to prevent hanging
      const syncPromise = api.post<User>(
        "/user/sync",
        {
          email: clerkUser.primaryEmailAddress?.emailAddress,
          eoa_address: eoaAddress || "", // Empty string if no wallet connected
        },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        }
      );

      const timeoutPromise = new Promise((_, reject) =>
        setTimeout(() => reject(new Error("Sync timeout")), 10000)
      );

      const { data } = await Promise.race([syncPromise, timeoutPromise]) as any;

      // Only call ensureWallet if we have an EOA address and no vault yet
      if (eoaAddress && !data?.vault_address) {
        try {
          const ensured = await ensureWallet(token);
          if (!ensured) {
            setBackendUser(data);
          }
        } catch (walletError) {
          // Don't fail the whole sync if ensureWallet fails
          console.error("useWallet: Failed to ensure wallet", walletError);
        }
      } else {
        setBackendUser(data);
      }
    } catch (error: any) {
      console.error("useWallet: Failed to sync user with backend", error);
      const message =
        error.response?.data?.error ||
        error.message ||
        "Failed to sync user";
      setWalletError(message);
      // Even on error, set backendUser to null so UI can render
      // This prevents infinite loading state
    } finally {
      syncInFlightRef.current = false;
      setIsSyncing(false);
    }
  }, [clerkUser, eoaAddress, ensureWallet, getToken]);

  useEffect(() => {
    // Sync user when they sign in, even without a wallet
    if (!isClerkLoaded || !clerkUser || isSyncing) {
      return;
    }

    // Check if we need to sync:
    // 1. No backend user yet - always sync
    // 2. EOA address changed - sync to update
    // 3. User has no EOA but we now have one - sync to add it
    const needsSync =
      !backendUser ||
      (eoaAddress && backendUser.eoa_address !== eoaAddress) ||
      (!backendUser.eoa_address && eoaAddress);

    if (needsSync) {
      syncUser();
    }
  }, [backendUser, clerkUser, eoaAddress, isClerkLoaded, isSyncing, syncUser]);

  const handleDisconnect = useCallback(() => {
    if (isWagmiConnected) {
      wagmiDisconnect();
    }
    setBackendUser(null);
    setWalletError(null);
  }, [isWagmiConnected, wagmiDisconnect]);

  useEffect(() => {
    if (isWagmiConnected) {
      return;
    }

    if (backendUser?.eoa_address) {
      setBackendUser(null);
    }
    setWalletError(null);
  }, [backendUser?.eoa_address, isWagmiConnected]);

  useEffect(() => {
    if (!backendUser) {
      return;
    }

    if (!eoaAddress) {
      return;
    }

    if (backendUser.eoa_address !== eoaAddress) {
      setBackendUser(null);
      setWalletError(null);
    }
  }, [backendUser, eoaAddress]);

  // Consider loaded if Clerk is loaded OR if timeout occurred (to prevent infinite loading)
  const isEffectivelyLoaded = isClerkLoaded || clerkLoadTimeout;

  return {
    isAuthenticated: !!clerkUser,
    isLoading: !isEffectivelyLoaded || isSyncing,
    user: backendUser,
    eoaAddress,
    vaultAddress: backendUser?.vault_address ?? null,
    walletError,
    disconnect: handleDisconnect,
    refreshUser: syncUser,
  };
}

