/**
 * @description
 * Wallet-only auth hook that signs SIWE messages and uses httpOnly JWT cookies.
 * Keeps backend user state in sync with the connected wallet.
 *
 * @dependencies
 * - wagmi
 * - @tanstack/react-query (indirectly through api hook usage)
 */

"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useAccount, useChainId, useDisconnect, useSignMessage } from "wagmi";

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

const walletCache = {
  user: null as User | null,
  fetchedAt: 0,
  inFlight: null as Promise<User | null> | null,
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
  const { address, isConnected } = useAccount();
  const chainId = useChainId();
  const { disconnect: wagmiDisconnect } = useDisconnect();
  const { signMessageAsync } = useSignMessage();

  const [backendUser, setBackendUser] = useState<User | null>(null);
  const [isSyncing, setIsSyncing] = useState(false);
  const [walletError, setWalletError] = useState<string | null>(null);
  const authInFlight = useRef<Promise<void> | null>(null);
  const lastAuthFailure = useRef<{ address: string; at: number } | null>(null);

  const eoaAddress = address ?? null;

  const loadUser = useCallback(
    async (force = false) => {
      const now = Date.now();
      if (!force && walletCache.user && now - walletCache.fetchedAt < WALLET_CACHE_TTL_MS) {
        return walletCache.user;
      }

      if (walletCache.inFlight) {
        return walletCache.inFlight;
      }

      walletCache.inFlight = api
        .get<User>("/user/me")
        .then(({ data }) => {
          walletCache.user = data;
          walletCache.fetchedAt = Date.now();
          return data;
        })
        .catch((error: any) => {
          if (error.response?.status === 404 || error.response?.status === 401) {
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

  const ensureWallet = useCallback(async () => {
    try {
      const { data } = await api.get<User>("/wallet");
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
    async (user: User | null) => {
      if (!user?.eoa_address || user.vault_address) {
        return;
      }
      if (!shouldEnsureWallet(user.eoa_address)) {
        return;
      }
      const ensured = await ensureWallet();
      if (ensured) {
        setBackendUser(ensured);
      }
    },
    [ensureWallet]
  );

  const requestAuth = useCallback(async () => {
    if (!eoaAddress || !isConnected) {
      return;
    }
    if (!chainId) {
      throw new Error("Wallet network not detected");
    }

    const { data } = await api.post<{ message: string }>("/auth/challenge", {
      address: eoaAddress,
      chain_id: chainId,
    });

    const signature = await signMessageAsync({ message: data.message });
    await api.post("/auth/verify", { message: data.message, signature });
  }, [chainId, eoaAddress, isConnected, signMessageAsync]);

  const ensureAuth = useCallback(
    async (force = false) => {
      if (!eoaAddress || !isConnected) {
        walletCache.user = null;
        walletCache.fetchedAt = 0;
        setBackendUser(null);
        return;
      }

      if (authInFlight.current) {
        return authInFlight.current;
      }

      const run = (async () => {
        setIsSyncing(true);
        try {
          let user = await loadUser(force);
          if (user && user.eoa_address && user.eoa_address.toLowerCase() !== eoaAddress.toLowerCase()) {
            await api.post("/auth/logout").catch(() => undefined);
            walletCache.user = null;
            walletCache.fetchedAt = 0;
            user = null;
          }

          if (!user) {
            const lastFailure = lastAuthFailure.current;
            if (!force && lastFailure && lastFailure.address === eoaAddress && Date.now() - lastFailure.at < 30_000) {
              return;
            }
            await requestAuth();
            user = await loadUser(true);
          }

          setBackendUser(user);
          setWalletError(null);
          await maybeEnsureWallet(user);
          lastAuthFailure.current = null;
        } catch (error: any) {
          console.error("useWallet: Failed to authenticate", error);
          const message =
            error.response?.data?.error ||
            error.message ||
            "Failed to authenticate";
          setWalletError(message);
          if (eoaAddress) {
            lastAuthFailure.current = { address: eoaAddress, at: Date.now() };
          }
        } finally {
          setIsSyncing(false);
          authInFlight.current = null;
        }
      })();

      authInFlight.current = run;
      return run;
    },
    [eoaAddress, isConnected, loadUser, maybeEnsureWallet, requestAuth]
  );

  useEffect(() => {
    void ensureAuth();
  }, [ensureAuth]);

  const handleDisconnect = useCallback(() => {
    if (isConnected) {
      wagmiDisconnect();
    }
    setWalletError(null);
    walletCache.user = null;
    walletCache.fetchedAt = 0;
    setBackendUser(null);
    void api.post("/auth/logout").catch(() => undefined);
  }, [isConnected, wagmiDisconnect]);

  const refreshUser = useCallback(async () => {
    await ensureAuth(true);
  }, [ensureAuth]);

  return {
    isAuthenticated: Boolean(backendUser) && Boolean(eoaAddress),
    isLoading: isSyncing,
    user: backendUser,
    eoaAddress,
    vaultAddress: backendUser?.vault_address ?? null,
    walletError,
    disconnect: handleDisconnect,
    refreshUser,
  };
}
