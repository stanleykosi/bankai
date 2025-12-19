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
  walletError: string | null;
  disconnect: () => void;
  refreshUser: () => Promise<void>;
}

type SyncPayload = {
  email?: string;
  eoaAddress?: string | null;
  clearWallet?: boolean;
};

const walletCache = {
  user: null as User | null,
  fetchedAt: 0,
  inFlight: null as Promise<User | null> | null,
  syncInFlight: null as Promise<User | null> | null,
};

const WALLET_CACHE_TTL_MS = 2 * 60 * 1000;
const ENSURE_TTL_MS = 10 * 60 * 1000;
const ENSURE_KEY_PREFIX = "bankai:wallet:ensure:";

const shouldEnsureWallet = (eoaAddress: string) => {
  if (typeof window === "undefined") {
    return false;
  }

  const key = `${ENSURE_KEY_PREFIX}${eoaAddress.toLowerCase()}`;
  const last = Number(window.sessionStorage.getItem(key) || "0");
  const now = Date.now();
  if (now - last < ENSURE_TTL_MS) {
    return false;
  }
  window.sessionStorage.setItem(key, String(now));
  return true;
};

export function useWallet(): UseWalletReturn {
  const { user: clerkUser, isLoaded: isClerkLoaded } = useUser();
  const { getToken } = useAuth();
  const { address: wagmiAddress, isConnected: isWagmiConnected } = useAccount();
  const { disconnect: wagmiDisconnect } = useDisconnect();

  const [backendUser, setBackendUser] = useState<User | null>(null);
  const [isSyncing, setIsSyncing] = useState(false);
  const [clerkLoadTimeout, setClerkLoadTimeout] = useState(false);
  const [walletError, setWalletError] = useState<string | null>(null);

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

  const loadUser = useCallback(
    async (token: string, force = false) => {
      const now = Date.now();
      if (!force && walletCache.user && now - walletCache.fetchedAt < WALLET_CACHE_TTL_MS) {
        return walletCache.user;
      }

      if (walletCache.inFlight) {
        return walletCache.inFlight;
      }

      walletCache.inFlight = api
        .get<User>("/user/me", {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        })
        .then(({ data }) => {
          walletCache.user = data;
          walletCache.fetchedAt = Date.now();
          return data;
        })
        .catch((error: any) => {
          if (error.response?.status === 404) {
            walletCache.user = null;
            walletCache.fetchedAt = Date.now();
            return null;
          }
          throw error;
        })
        .finally(() => {
          walletCache.inFlight = null;
        });

      return walletCache.inFlight;
    },
    []
  );

  const syncUser = useCallback(
    async (token: string, payload: SyncPayload) => {
      if (walletCache.syncInFlight) {
        return walletCache.syncInFlight;
      }

      const syncPromise = api
        .post<User>(
          "/user/sync",
          {
            email: payload.email ?? clerkUser?.primaryEmailAddress?.emailAddress,
            eoa_address: payload.eoaAddress ?? "",
            clear_wallet: payload.clearWallet ?? false,
          },
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          }
        )
        .then(({ data }) => {
          walletCache.user = data;
          walletCache.fetchedAt = Date.now();
          return data;
        })
        .finally(() => {
          walletCache.syncInFlight = null;
        });

      walletCache.syncInFlight = syncPromise;
      return syncPromise;
    },
    [clerkUser?.primaryEmailAddress?.emailAddress]
  );

  const ensureWallet = useCallback(async (token: string) => {
    try {
      const { data } = await api.get<User>("/wallet", {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });
      walletCache.user = data;
      walletCache.fetchedAt = Date.now();
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
  }, []);

  const maybeEnsureWallet = useCallback(
    async (token: string, user: User | null) => {
      if (!user?.eoa_address || user.vault_address) {
        return;
      }
      if (!shouldEnsureWallet(user.eoa_address)) {
        return;
      }
      const ensured = await ensureWallet(token);
      if (ensured) {
        setBackendUser(ensured);
      }
    },
    [ensureWallet]
  );

  const bootstrapUser = useCallback(async () => {
    if (!isClerkLoaded || !clerkUser) {
      setBackendUser(null);
      return;
    }

    setIsSyncing(true);
    try {
      const token = await getToken();
      if (!token) {
        return;
      }

      let user = await loadUser(token);
      if (!user) {
        user = await syncUser(token, {
          email: clerkUser.primaryEmailAddress?.emailAddress,
          eoaAddress: eoaAddress,
        });
      }
      setBackendUser(user);
      await maybeEnsureWallet(token, user);
    } catch (error: any) {
      console.error("useWallet: Failed to load user", error);
      const message =
        error.response?.data?.error ||
        error.message ||
        "Failed to load user";
      setWalletError(message);
    } finally {
      setIsSyncing(false);
    }
  }, [
    clerkUser,
    eoaAddress,
    getToken,
    isClerkLoaded,
    loadUser,
    maybeEnsureWallet,
    syncUser,
  ]);

  useEffect(() => {
    void bootstrapUser();
  }, [bootstrapUser]);

  useEffect(() => {
    if (!isClerkLoaded || !clerkUser || !backendUser) {
      return;
    }
    if (!eoaAddress || backendUser.eoa_address === eoaAddress) {
      return;
    }

    const run = async () => {
      setIsSyncing(true);
      try {
        const token = await getToken();
        if (!token) {
          return;
        }
        const updated = await syncUser(token, {
          email: clerkUser.primaryEmailAddress?.emailAddress,
          eoaAddress,
        });
        setBackendUser(updated);
        await maybeEnsureWallet(token, updated);
      } catch (error: any) {
        console.error("useWallet: Failed to sync updated wallet", error);
        const message =
          error.response?.data?.error ||
          error.message ||
          "Failed to sync user";
        setWalletError(message);
      } finally {
        setIsSyncing(false);
      }
    };

    void run();
  }, [
    backendUser,
    clerkUser,
    eoaAddress,
    getToken,
    isClerkLoaded,
    maybeEnsureWallet,
    syncUser,
  ]);

  const handleDisconnect = useCallback(() => {
    if (isWagmiConnected) {
      wagmiDisconnect();
    }
    setWalletError(null);

    const run = async () => {
      if (!clerkUser) {
        setBackendUser(null);
        return;
      }
      const token = await getToken();
      if (!token) {
        setBackendUser(null);
        return;
      }
      setIsSyncing(true);
      try {
        const updated = await syncUser(token, {
          email: clerkUser.primaryEmailAddress?.emailAddress,
          eoaAddress: "",
          clearWallet: true,
        });
        setBackendUser(updated);
      } catch (error: any) {
        console.error("useWallet: Failed to clear wallet", error);
        const message =
          error.response?.data?.error ||
          error.message ||
          "Failed to disconnect wallet";
        setWalletError(message);
        setBackendUser(null);
      } finally {
        setIsSyncing(false);
      }
    };

    void run();
  }, [clerkUser, getToken, isWagmiConnected, syncUser, wagmiDisconnect]);

  const refreshUser = useCallback(async () => {
    const token = await getToken();
    if (!token) {
      return;
    }
    setIsSyncing(true);
    try {
      const user = await loadUser(token, true);
      setBackendUser(user);
    } catch (error: any) {
      console.error("useWallet: Failed to refresh user", error);
      const message =
        error.response?.data?.error ||
        error.message ||
        "Failed to refresh user";
      setWalletError(message);
    } finally {
      setIsSyncing(false);
    }
  }, [getToken, loadUser]);

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
    refreshUser,
  };
}
