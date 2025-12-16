/**
 * @description
 * Hook to obtain user API credentials from Polymarket CLOB.
 * Uses the official SDK's deriveApiKey/createApiKey methods.
 * 
 * Based on wagmi-safe-builder-example pattern.
 */

"use client";

import { useState, useCallback, useEffect, useRef } from "react";
import { useWalletClient, useAccount } from "wagmi";
import { ClobClient } from "@polymarket/clob-client";
import { walletClientToEthersSigner } from "@/lib/ethers-adapter";
import { POLYGON_CHAIN_ID } from "@/lib/polymarket";

const CLOB_API_URL = "https://clob.polymarket.com";
const STORAGE_PREFIX = "clob-creds:";

const loadStoredCreds = (address: string): UserApiCredentials | null => {
  try {
    const raw = localStorage.getItem(`${STORAGE_PREFIX}${address.toLowerCase()}`);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (parsed?.key && parsed?.secret && parsed?.passphrase) {
      return parsed as UserApiCredentials;
    }
  } catch {
    // ignore parse errors
  }
  return null;
};

const persistCreds = (address: string, creds: UserApiCredentials) => {
  try {
    localStorage.setItem(`${STORAGE_PREFIX}${address.toLowerCase()}`, JSON.stringify(creds));
  } catch {
    // ignore storage errors (e.g., private mode)
  }
};

const clearStoredCreds = (address?: string) => {
  if (!address) return;
  try {
    localStorage.removeItem(`${STORAGE_PREFIX}${address.toLowerCase()}`);
  } catch {
    // ignore
  }
};

export interface UserApiCredentials {
  key: string;
  secret: string;
  passphrase: string;
}

export function useUserApiCredentials() {
  const { data: walletClient } = useWalletClient();
  const { address } = useAccount();
  const [credentials, setCredentials] = useState<UserApiCredentials | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const fetchInProgressRef = useRef(false);

  const getCredentials = useCallback(async (): Promise<UserApiCredentials> => {
    if (!walletClient || !address) {
      throw new Error("Wallet not connected");
    }

    // Return cached credentials if available
    if (credentials) {
      return credentials;
    }

    // Try localStorage cache
    const cached = loadStoredCreds(address);
    if (cached) {
      setCredentials(cached);
      return cached;
    }

    // Prevent concurrent fetches
    if (fetchInProgressRef.current) {
      // Wait for the in-progress fetch
      while (fetchInProgressRef.current) {
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      // Return cached credentials after fetch completes
      if (credentials) {
        return credentials;
      }
    }

    fetchInProgressRef.current = true;
    setIsLoading(true);
    setError(null);

    try {
      // Convert wagmi signer to ethers signer for SDK
      const ethersSigner = walletClientToEthersSigner(walletClient);

      // Create temporary CLOB client (no credentials yet)
      const tempClient = new ClobClient(
        CLOB_API_URL,
        POLYGON_CHAIN_ID,
        ethersSigner
      );

      // Try to derive existing credentials first (for returning users)
      const derivedCreds = await tempClient.deriveApiKey().catch(() => null);

      if (
        derivedCreds?.key &&
        derivedCreds?.secret &&
        derivedCreds?.passphrase
      ) {
        const creds: UserApiCredentials = {
          key: derivedCreds.key,
          secret: derivedCreds.secret,
          passphrase: derivedCreds.passphrase,
        };
        setCredentials(creds);
        persistCreds(address, creds);
        return creds;
      }

      // Derive failed or returned invalid data - create new credentials
      const newCreds = await tempClient.createApiKey();
      const creds: UserApiCredentials = {
        key: newCreds.key,
        secret: newCreds.secret,
        passphrase: newCreds.passphrase,
      };
      setCredentials(creds);
      persistCreds(address, creds);
      return creds;
    } catch (err: any) {
      const error = err instanceof Error ? err : new Error("Failed to get API credentials");
      setError(error);
      throw error;
    } finally {
      setIsLoading(false);
      fetchInProgressRef.current = false;
    }
  }, [walletClient, address, credentials]);

  // Warm state from localStorage when wallet connects
  useEffect(() => {
    if (address && !credentials) {
      const cached = loadStoredCreds(address);
      if (cached) {
        setCredentials(cached);
      }
    }
  }, [address, credentials]);

  // Clear credentials when wallet disconnects
  useEffect(() => {
    if (!walletClient || !address) {
      setCredentials(null);
      setError(null);
    }
  }, [walletClient, address]);

  return {
    credentials,
    getCredentials,
    isLoading,
    error,
  };
}
