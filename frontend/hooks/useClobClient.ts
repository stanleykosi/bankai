/**
 * @description
 * Hook to create an authenticated ClobClient with user API credentials and builder config.
 * The client handles order creation, signing, and submission using the official SDK.
 * 
 * Based on wagmi-safe-builder-example pattern.
 */

"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useWalletClient, useAccount } from "wagmi";
import { ClobClient } from "@polymarket/clob-client";
import { BuilderConfig } from "@polymarket/builder-signing-sdk";
import { walletClientToEthersSigner } from "@/lib/ethers-adapter";
import { api } from "@/lib/api";
import { POLYGON_CHAIN_ID } from "@/lib/polymarket";
import type { UserApiCredentials } from "./useUserApiCredentials";

const CLOB_API_URL = "https://clob.polymarket.com";
const MIN_SIGNER_REFRESH_MS = 10_000;
const RETRY_BACKOFF_CAP_MS = 60_000;
const BACKOFF_STEPS = 3;
const BACKEND_ASSERTION_SKEW_SECONDS = 5;

type SharedBackendAssertion = {
  token: string;
  expiresAt: number;
};

let sharedBackendAssertion: SharedBackendAssertion | null = null;
let sharedBackendAssertionInFlight: Promise<SharedBackendAssertion> | null = null;

// Builder signing SDK requires an absolute remote URL (must start with http/https)
// Derive it from window location when available, or fall back to deployment hints during SSR.
const getRemoteSigningUrl = () => {
  if (typeof window !== "undefined" && window.location?.origin) {
    return `${window.location.origin}/api/polymarket/sign`;
  }

  const envUrl =
    process.env.NEXT_PUBLIC_VERCEL_URL ||
    process.env.VERCEL_URL ||
    process.env.RAILWAY_PUBLIC_DOMAIN ||
    process.env.RAILWAY_STATIC_URL;

  if (envUrl) {
    const trimmed = envUrl.replace(/\/+$/, "");
    const base = trimmed.startsWith("http")
      ? trimmed
      : `https://${trimmed.replace(/^\/+/, "")}`;
    return `${base}/api/polymarket/sign`;
  }

  // Sensible local fallback for SSR (useful in dev or static export)
  return "http://localhost:3000/api/polymarket/sign";
};

export interface UseClobClientParams {
  credentials: UserApiCredentials | null;
  vaultAddress: string | null;
  walletType?: "SAFE" | "PROXY" | null;
}

export function useClobClient({
  credentials,
  vaultAddress,
  walletType,
}: UseClobClientParams) {
  const { data: walletClient } = useWalletClient();
  const { address: eoaAddress } = useAccount();
  const [signerToken, setSignerToken] = useState<string | null>(null);
  const signerTokenRef = useRef<string | null>(null);
  const signerTokenExpiryRef = useRef<number>(0);
  const backendAssertionRef = useRef<string | null>(null);
  const backendAssertionExpiryRef = useRef<number>(0);

  useEffect(() => {
    signerTokenRef.current = signerToken;
  }, [signerToken]);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let controller: AbortController | null = null;
    let failureStreak = 0;
    const nowSeconds = () => Math.floor(Date.now() / 1000);
    const clearToken = () => {
      signerTokenRef.current = null;
      signerTokenExpiryRef.current = 0;
      setSignerToken(null);
    };
    const setFreshToken = (token: string, expiresAt: number) => {
      signerTokenRef.current = token;
      signerTokenExpiryRef.current = expiresAt;
      setSignerToken(token);
    };
    const clearBackendAssertion = () => {
      backendAssertionRef.current = null;
      backendAssertionExpiryRef.current = 0;
      sharedBackendAssertion = null;
    };
    const hydrateBackendAssertionFromShared = () => {
      if (!sharedBackendAssertion) return;
      if (sharedBackendAssertion.expiresAt <= nowSeconds() + BACKEND_ASSERTION_SKEW_SECONDS) {
        sharedBackendAssertion = null;
        return;
      }
      if (
        backendAssertionRef.current !== sharedBackendAssertion.token ||
        backendAssertionExpiryRef.current !== sharedBackendAssertion.expiresAt
      ) {
        backendAssertionRef.current = sharedBackendAssertion.token;
        backendAssertionExpiryRef.current = sharedBackendAssertion.expiresAt;
      }
    };
    const hasUsableToken = () =>
      Boolean(
        signerTokenRef.current &&
          signerTokenExpiryRef.current > nowSeconds() + 5
      );
    const hasUsableBackendAssertion = () =>
      Boolean(
        backendAssertionRef.current &&
          backendAssertionExpiryRef.current >
            nowSeconds() + BACKEND_ASSERTION_SKEW_SECONDS
      );
    const scheduleRetry = (baseDelayMs = MIN_SIGNER_REFRESH_MS) => {
      const step = Math.min(failureStreak, BACKOFF_STEPS);
      const delay = Math.min(RETRY_BACKOFF_CAP_MS, baseDelayMs * 2 ** step);
      failureStreak += 1;
      scheduleRefresh(delay);
    };

    const fetchBackendAssertion = async () => {
      hydrateBackendAssertionFromShared();
      if (hasUsableBackendAssertion()) {
        return backendAssertionRef.current as string;
      }
      if (sharedBackendAssertionInFlight) {
        const shared = await sharedBackendAssertionInFlight;
        backendAssertionRef.current = shared.token;
        backendAssertionExpiryRef.current = shared.expiresAt;
        return shared.token;
      }
      const request = (async (): Promise<SharedBackendAssertion> => {
        const response = await api.get<{
          token?: string;
          expires_at?: number | string;
        }>("/auth/signer-assertion");
        const token =
          typeof response?.data?.token === "string"
            ? response.data.token.trim()
            : "";
        const expiresAtRaw =
          typeof response?.data?.expires_at === "number"
            ? response.data.expires_at
            : Number(response?.data?.expires_at);
        const expiresAt = Number.isFinite(expiresAtRaw)
          ? Math.floor(expiresAtRaw)
          : nowSeconds() + 60;
        if (!token || expiresAt <= nowSeconds()) {
          throw new Error("invalid signer assertion");
        }
        return { token, expiresAt };
      })();
      sharedBackendAssertionInFlight = request;
      try {
        const shared = await request;
        sharedBackendAssertion = shared;
        backendAssertionRef.current = shared.token;
        backendAssertionExpiryRef.current = shared.expiresAt;
        return shared.token;
      } finally {
        if (sharedBackendAssertionInFlight === request) {
          sharedBackendAssertionInFlight = null;
        }
      }
    };

    const scheduleRefresh = (delayMs: number) => {
      if (cancelled) return;
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        void fetchToken();
      }, delayMs);
    };

    const fetchToken = async () => {
      if (cancelled) return;
      const nextController = new AbortController();
      controller = nextController;

      try {
        const assertionToken = hasUsableBackendAssertion()
          ? backendAssertionRef.current
          : await fetchBackendAssertion();
        if (!assertionToken) {
          clearToken();
          scheduleRetry();
          return;
        }

        const res = await fetch("/api/polymarket/sign", {
          method: "GET",
          credentials: "include",
          headers: {
            Authorization: `Bearer ${assertionToken}`,
          },
          signal: nextController.signal,
        });
        if (cancelled) return;

        if (!res.ok) {
          // Auth failures should immediately drop the token; transient failures should not.
          if (res.status === 401 || res.status === 403) {
            clearToken();
            clearBackendAssertion();
          } else if (!hasUsableToken()) {
            clearToken();
          }
          scheduleRetry(res.status === 429 ? 15_000 : MIN_SIGNER_REFRESH_MS);
          return;
        }

        const payload = await res.json();
        if (cancelled) return;
        const nextToken =
          typeof payload?.token === "string" ? payload.token.trim() : "";
        const expiresAtRaw =
          typeof payload?.expires_at === "number"
            ? payload.expires_at
            : Number(payload?.expires_at);
        const expiresAt = Number.isFinite(expiresAtRaw)
          ? Math.floor(expiresAtRaw)
          : nowSeconds() + 90;

        if (!nextToken || expiresAt <= nowSeconds()) {
          if (!hasUsableToken()) {
            clearToken();
          }
          scheduleRetry();
          return;
        }

        failureStreak = 0;
        setFreshToken(nextToken, expiresAt);
        const refreshMs = Math.max(
          MIN_SIGNER_REFRESH_MS,
          (expiresAt - nowSeconds() - 15) * 1000
        );
        scheduleRefresh(refreshMs);
      } catch {
        if (cancelled) return;
        if (!hasUsableToken()) {
          clearToken();
        }
        scheduleRetry();
      } finally {
        if (controller === nextController) {
          controller = null;
        }
      }
    };

    if (credentials && eoaAddress && vaultAddress) {
      void fetchToken();
    } else {
      clearToken();
      clearBackendAssertion();
    }

    return () => {
      cancelled = true;
      if (controller) controller.abort();
      if (timer) clearTimeout(timer);
    };
  }, [credentials, eoaAddress, vaultAddress]);

  const clobClient = useMemo(() => {
    if (
      !walletClient ||
      !eoaAddress ||
      !vaultAddress ||
      !credentials ||
      !signerToken
    ) {
      return null;
    }

    try {
      // Convert wagmi signer to ethers signer for SDK
      const ethersSigner = walletClientToEthersSigner(walletClient);

      const remoteSigningUrl = getRemoteSigningUrl();
      if (!remoteSigningUrl) {
        console.error("Remote builder signing URL unavailable");
        return null;
      }

      // Builder config with remote server signing for order attribution
      const builderConfig = new BuilderConfig({
        remoteBuilderConfig: {
          url: remoteSigningUrl,
          token: signerToken,
        },
      });

      // Determine signature type based on wallet type
      // 0 = raw EOA signature (works for most wallets)
      // 1 = Magic/Proxy
      // 2 = Browser wallets (Metamask/Coinbase) / Safe
      let signatureType = 0;
      if (walletType === "SAFE") {
        signatureType = 2;
      } else if (walletType === "PROXY") {
        signatureType = 1;
      }

      // Create authenticated ClobClient with:
      // - User API credentials (for L2 authentication)
      // - Builder config (for order attribution)
      // - Signature type (based on wallet type)
      // - Maker address (vault address)
      const client = new ClobClient(
        CLOB_API_URL,
        POLYGON_CHAIN_ID,
        ethersSigner,
        credentials,
        signatureType,
        vaultAddress as `0x${string}`,
        undefined, // mandatory placeholder
        false,
        builderConfig // Builder order attribution
      );

      return client;
    } catch (error) {
      console.error("Failed to create ClobClient:", error);
      return null;
    }
  }, [walletClient, eoaAddress, vaultAddress, credentials, walletType, signerToken]);

  return { clobClient };
}
