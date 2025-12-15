/**
 * @description
 * Adapter to convert wagmi/viem signer to ethers signer for Polymarket SDK.
 * The SDK requires ethers v5 signer, but we use wagmi/viem.
 */

import { providers } from "ethers";
import { type WalletClient } from "viem";

/**
 * Converts a viem WalletClient to an ethers JsonRpcSigner.
 * This allows the Polymarket SDK (which uses ethers) to work with wagmi/viem.
 * 
 * Based on wagmi-safe-builder-example pattern:
 * const provider = new providers.Web3Provider(wagmiWalletClient);
 * return provider.getSigner();
 */
export function walletClientToEthersSigner(
  walletClient: WalletClient
): providers.JsonRpcSigner {
  if (!walletClient) {
    throw new Error("Wallet client is required");
  }

  // Create ethers provider from the wallet client (ethers v5 Web3Provider)
  const provider = new providers.Web3Provider(walletClient as any);
  
  // Get the signer from the provider
  return provider.getSigner();
}

