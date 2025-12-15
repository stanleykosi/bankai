/**
 * @description
 * Hook to create an authenticated ClobClient with user API credentials and builder config.
 * The client handles order creation, signing, and submission using the official SDK.
 * 
 * Based on wagmi-safe-builder-example pattern.
 */

"use client";

import { useMemo } from "react";
import { useWalletClient, useAccount } from "wagmi";
import { ClobClient } from "@polymarket/clob-client";
import { BuilderConfig } from "@polymarket/builder-signing-sdk";
import { walletClientToEthersSigner } from "@/lib/ethers-adapter";
import { POLYGON_CHAIN_ID } from "@/lib/polymarket";
import type { UserApiCredentials } from "./useUserApiCredentials";

const CLOB_API_URL = "https://clob.polymarket.com";
const REMOTE_SIGNING_URL = "/api/polymarket/sign";

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

  const clobClient = useMemo(() => {
    if (
      !walletClient ||
      !eoaAddress ||
      !vaultAddress ||
      !credentials
    ) {
      return null;
    }

    try {
      // Convert wagmi signer to ethers signer for SDK
      const ethersSigner = walletClientToEthersSigner(walletClient);

      // Builder config with remote server signing for order attribution
      const builderConfig = new BuilderConfig({
        remoteBuilderConfig: {
          url: REMOTE_SIGNING_URL,
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
  }, [walletClient, eoaAddress, vaultAddress, credentials, walletType]);

  return { clobClient };
}

