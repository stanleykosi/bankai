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

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { polygon } from "viem/chains";
import { useAccount, useDisconnect, useSignMessage, useSwitchChain } from "wagmi";

import { api } from "@/lib/api";
import { ensurePolygonChain } from "@/lib/chain-utils";
import type { User } from "@/types";

export interface UseWalletReturn {
  isAuthenticated: boolean;
  isLoading: boolean;
  hasSession: boolean;
  isSessionRestoring: boolean;
  user: User | null;
  eoaAddress: string | null;
  vaultAddress: string | null;
  uiVaultAddress: string | null;
  walletError: string | null;
  disconnect: () => void;
  refreshUser: () => Promise<void>;
}

const walletCache = {
  user: null as User | null,
  eoaAddress: null as string | null,
  fetchedAt: 0,
  inFlight: null as Promise<User | null> | null,
};

const WALLET_CACHE_TTL_MS = 2 * 60 * 1000;
const AUTH_RETRY_WINDOW_MS = 30_000;
const AUTH_ATTEMPT_TTL_MS = 60_000;
const ENSURE_TTL_MS = 10 * 60 * 1000;
const ENSURE_KEY_PREFIX = "bankai:wallet:ensure:";
const LAST_PROXY_KEY = "bankai:wallet:last-proxy";
const LAST_EOA_KEY = "bankai:wallet:last-eoa";
const SESSION_RESTORE_WINDOW_MS = 4000;

const authState = {
  inFlight: null as Promise<void> | null,
  lastFailure: null as { address: string; at: number } | null,
  lastAttempt: null as { address: string; at: number } | null,
};

const useIsomorphicLayoutEffect =
  typeof window !== "undefined" ? useLayoutEffect : useEffect;

const sessionCache = {
  eoaAddress: null as string | null,
  proxyAddress: null as string | null,
  restoreUntil: 0,
  initialized: false,
};

const initSessionCache = () => {
  if (sessionCache.initialized || typeof window === "undefined") {
    return;
  }
  sessionCache.eoaAddress = window.sessionStorage.getItem(LAST_EOA_KEY);
  sessionCache.proxyAddress = window.sessionStorage.getItem(LAST_PROXY_KEY);
  if (sessionCache.eoaAddress || sessionCache.proxyAddress) {
    sessionCache.restoreUntil = Date.now() + SESSION_RESTORE_WINDOW_MS;
  }
  sessionCache.initialized = true;
};

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
  const { address, isConnected, isConnecting, isReconnecting, isDisconnected, chainId } = useAccount();
  const { disconnect: wagmiDisconnect } = useDisconnect();
  const { signMessageAsync } = useSignMessage();
  const { switchChainAsync } = useSwitchChain();

  initSessionCache();

  const [sessionState, setSessionState] = useState(() => ({
    eoaAddress: sessionCache.eoaAddress,
    proxyAddress: sessionCache.proxyAddress,
    restoreUntil: sessionCache.restoreUntil,
  }));
  const [backendUser, setBackendUser] = useState<User | null>(() => walletCache.user);
  const [isSyncing, setIsSyncing] = useState(false);
  const [walletError, setWalletError] = useState<string | null>(null);
  const chainIdRef = useRef<number | undefined>(chainId);

  const eoaAddress = address ?? null;
  const isSessionRestoring =
    !eoaAddress && sessionState.restoreUntil > Date.now();

  useEffect(() => {
    chainIdRef.current = chainId;
  }, [chainId]);

  useIsomorphicLayoutEffect(() => {
    if (sessionCache.initialized) {
      return;
    }
    initSessionCache();
    if (sessionCache.initialized) {
      setSessionState({
        eoaAddress: sessionCache.eoaAddress,
        proxyAddress: sessionCache.proxyAddress,
        restoreUntil: sessionCache.restoreUntil,
      });
    }
  }, []);

  useEffect(() => {
    if (!sessionState.restoreUntil || typeof window === "undefined") {
      return;
    }
    const now = Date.now();
    if (sessionState.restoreUntil <= now) {
      sessionCache.restoreUntil = 0;
      setSessionState((prev) => ({ ...prev, restoreUntil: 0 }));
      if (!authState.inFlight) {
        setIsSyncing(false);
      }
      return;
    }
    const timeout = window.setTimeout(() => {
      sessionCache.restoreUntil = 0;
      setSessionState((prev) => ({ ...prev, restoreUntil: 0 }));
      if (!authState.inFlight) {
        setIsSyncing(false);
      }
    }, sessionState.restoreUntil - now);
    return () => clearTimeout(timeout);
  }, [sessionState.restoreUntil]);

  useEffect(() => {
    if (!eoaAddress || !sessionState.restoreUntil) {
      return;
    }
    sessionCache.restoreUntil = 0;
    setSessionState((prev) => ({ ...prev, restoreUntil: 0 }));
  }, [eoaAddress, sessionState.restoreUntil]);

  useEffect(() => {
    if (!isDisconnected || typeof window === "undefined") {
      return;
    }
    const timeout = window.setTimeout(() => {
      const storedProxy = window.sessionStorage.getItem(LAST_PROXY_KEY);
      const storedEoa = window.sessionStorage.getItem(LAST_EOA_KEY);
      if (!storedProxy && !storedEoa) {
        sessionCache.eoaAddress = null;
        sessionCache.proxyAddress = null;
        sessionCache.restoreUntil = 0;
        sessionCache.initialized = true;
        setSessionState({
          eoaAddress: null,
          proxyAddress: null,
          restoreUntil: 0,
        });
        return;
      }
      sessionCache.eoaAddress = storedEoa;
      sessionCache.proxyAddress = storedProxy;
      sessionCache.restoreUntil = 0;
      sessionCache.initialized = true;
      setSessionState({
        eoaAddress: storedEoa,
        proxyAddress: storedProxy,
        restoreUntil: 0,
      });
    }, 2100);
    return () => clearTimeout(timeout);
  }, [isDisconnected]);

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
          walletCache.eoaAddress = data?.eoa_address ?? null;
          walletCache.fetchedAt = Date.now();
          return data;
        })
        .catch((error: any) => {
          if (error.response?.status === 404 || error.response?.status === 401) {
            walletCache.user = null;
            walletCache.eoaAddress = null;
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
      walletCache.eoaAddress = data?.eoa_address ?? null;
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
    if (!chainIdRef.current) {
      throw new Error("Wallet network not detected");
    }

    await ensurePolygonChain(() => chainIdRef.current, switchChainAsync);

    const { data } = await api.post<{ message: string }>("/auth/challenge", {
      address: eoaAddress,
      chain_id: polygon.id,
    });

    const signature = await signMessageAsync({ message: data.message });
    await api.post("/auth/verify", { message: data.message, signature });
  }, [eoaAddress, isConnected, signMessageAsync, switchChainAsync]);

  const ensureAuth = useCallback(
    async (force = false) => {
      if (!eoaAddress || !isConnected) {
        if (isSessionRestoring) {
          setIsSyncing(true);
          return;
        }
        if (isConnecting || isReconnecting) {
          setIsSyncing(true);
          return;
        }
        if (!isDisconnected && eoaAddress) {
          return;
        }
        walletCache.user = null;
        walletCache.eoaAddress = null;
        walletCache.fetchedAt = 0;
        setBackendUser(null);
        setIsSyncing(false);
        return;
      }

      if (authState.inFlight) {
        setIsSyncing(true);
        return authState.inFlight
          .then(() => {
            setBackendUser(walletCache.user ?? null);
          })
          .finally(() => {
            setIsSyncing(false);
          });
      }

      const run = (async () => {
        setIsSyncing(true);
        try {
          let user = await loadUser(force);
          if (user && user.eoa_address && user.eoa_address.toLowerCase() !== eoaAddress.toLowerCase()) {
            await api.post("/auth/logout").catch(() => undefined);
            walletCache.user = null;
            walletCache.eoaAddress = null;
            walletCache.fetchedAt = 0;
            setBackendUser(null);
            user = null;
          }

          if (!user) {
            const lastFailure = authState.lastFailure;
            if (!force && lastFailure && lastFailure.address === eoaAddress && Date.now() - lastFailure.at < AUTH_RETRY_WINDOW_MS) {
              return;
            }
            const lastAttempt = authState.lastAttempt;
            if (!force && lastAttempt && lastAttempt.address === eoaAddress && Date.now() - lastAttempt.at < AUTH_ATTEMPT_TTL_MS) {
              return;
            }
            authState.lastAttempt = { address: eoaAddress, at: Date.now() };
            await requestAuth();
            user = await loadUser(true);
          }

          setBackendUser(user);
          setWalletError(null);
          await maybeEnsureWallet(user);
          authState.lastFailure = null;
        } catch (error: any) {
          console.error("useWallet: Failed to authenticate", error);
          const message =
            error.response?.data?.error ||
            error.message ||
            "Failed to authenticate";
          setWalletError(message);
          if (eoaAddress) {
            authState.lastFailure = { address: eoaAddress, at: Date.now() };
          }
        } finally {
          setIsSyncing(false);
          authState.inFlight = null;
        }
      })();

      authState.inFlight = run;
      return run;
    },
    [eoaAddress, isConnected, isConnecting, isReconnecting, isDisconnected, isSessionRestoring, loadUser, maybeEnsureWallet, requestAuth]
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
    walletCache.eoaAddress = null;
    walletCache.fetchedAt = 0;
    setBackendUser(null);
    sessionCache.eoaAddress = null;
    sessionCache.proxyAddress = null;
    sessionCache.restoreUntil = 0;
    sessionCache.initialized = true;
    if (typeof window !== "undefined") {
      window.sessionStorage.removeItem(LAST_EOA_KEY);
      window.sessionStorage.removeItem(LAST_PROXY_KEY);
    }
    setSessionState({
      eoaAddress: null,
      proxyAddress: null,
      restoreUntil: 0,
    });
    authState.inFlight = null;
    authState.lastFailure = null;
    authState.lastAttempt = null;
    void api.post("/auth/logout").catch(() => undefined);
  }, [isConnected, wagmiDisconnect]);

  const refreshUser = useCallback(async () => {
    await ensureAuth(true);
  }, [ensureAuth]);

  const addressMatches =
    Boolean(eoaAddress) &&
    Boolean(backendUser?.eoa_address) &&
    backendUser?.eoa_address?.toLowerCase() === eoaAddress?.toLowerCase();
  const resolvedUser = eoaAddress ? (addressMatches ? backendUser : null) : backendUser;
  const vaultAddress = eoaAddress
    ? addressMatches
      ? backendUser?.vault_address ?? null
      : null
    : backendUser?.vault_address ?? null;

  useEffect(() => {
    if (!eoaAddress || !vaultAddress || typeof window === "undefined") {
      return;
    }
    if (
      sessionCache.eoaAddress === eoaAddress &&
      sessionCache.proxyAddress === vaultAddress
    ) {
      return;
    }
    sessionCache.eoaAddress = eoaAddress;
    sessionCache.proxyAddress = vaultAddress;
    sessionCache.restoreUntil = 0;
    sessionCache.initialized = true;
    setSessionState({
      eoaAddress,
      proxyAddress: vaultAddress,
      restoreUntil: 0,
    });
    window.sessionStorage.setItem(LAST_EOA_KEY, eoaAddress);
    window.sessionStorage.setItem(LAST_PROXY_KEY, vaultAddress);
  }, [eoaAddress, vaultAddress]);

  const uiVaultAddress = vaultAddress ?? sessionState.proxyAddress ?? null;
  const hasSession = Boolean(uiVaultAddress || eoaAddress || resolvedUser);
  const isLoading =
    isSyncing || isSessionRestoring || (Boolean(backendUser) && !eoaAddress);

  return {
    isAuthenticated: Boolean(resolvedUser) && Boolean(eoaAddress),
    isLoading,
    hasSession,
    isSessionRestoring,
    user: resolvedUser,
    eoaAddress,
    vaultAddress,
    uiVaultAddress,
    walletError,
    disconnect: handleDisconnect,
    refreshUser,
  };
}
