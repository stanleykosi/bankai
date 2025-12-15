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

  // Auto-fetch credentials when wallet connects (optimization for network performance)
  useEffect(() => {
    if (walletClient && address && !credentials && !isLoading && !fetchInProgressRef.current) {
      // Silently fetch in background - don't show errors to user unless they try to trade
      getCredentials().catch(() => {
        // Silently fail - credentials will be fetched on-demand when needed
      });
    }
  }, [walletClient, address, credentials, isLoading, getCredentials]);

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

