/**
 * @description
 * Utility functions for ensuring the wallet is on the correct chain.
 * Handles chain switching and verification, especially important for
 * Phantom Wallet which validates chainId in EIP-712 signatures.
 */

import { polygon } from "viem/chains";
import { type SwitchChainReturnType } from "wagmi/actions";

/**
 * Ensures the wallet is connected to Polygon network.
 * Switches chain if needed and waits for the switch to complete.
 * 
 * This function is critical for Phantom Wallet compatibility, as Phantom
 * validates the chainId in EIP-712 domain before allowing signature.
 * 
 * @param getChainId - Function that returns current chain ID (from useAccount().chainId)
 * @param switchChainAsync - Function from useSwitchChain hook
 * @returns Promise that resolves when chain is confirmed to be Polygon
 */
export async function ensurePolygonChain(
  getChainId: () => number | undefined,
  switchChainAsync: ((args: { chainId: number }) => Promise<SwitchChainReturnType>) | undefined
): Promise<void> {
  // Check current chain
  let currentChainId = getChainId();
  
  // Already on Polygon
  if (currentChainId === polygon.id) {
    return;
  }

  // No switch function available
  if (!switchChainAsync) {
    throw new Error(
      "Please switch your wallet to Polygon network (Chain ID: 137) before proceeding."
    );
  }

  // Attempt to switch chain
  try {
    await switchChainAsync({ chainId: polygon.id });
    
    // Wait for chain switch to complete
    // Phantom Wallet needs time to process the switch and update its internal state
    // We'll poll the chain ID to verify the switch completed
    let attempts = 0;
    const maxAttempts = 15; // 15 attempts * 300ms = 4.5 seconds max wait
    
    while (attempts < maxAttempts) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      
      currentChainId = getChainId();
      
      if (currentChainId === polygon.id) {
        // Chain switch confirmed, wait a bit more for wallet to fully update
        // This is especially important for Phantom Wallet
        await new Promise((resolve) => setTimeout(resolve, 500));
        return;
      }
      
      attempts++;
    }
    
    // If we get here, the chain switch may not have completed
    // Check one more time and throw if still wrong
    currentChainId = getChainId();
    if (currentChainId !== polygon.id) {
      throw new Error(
        "Chain switch did not complete. Please ensure Polygon network is added to your wallet and try again."
      );
    }
  } catch (error: any) {
    // Handle user rejection
    if (error?.code === 4001 || 
        error?.message?.includes("rejected") || 
        error?.message?.includes("denied") ||
        error?.message?.includes("User rejected")) {
      throw new Error("Chain switch was rejected. Please switch to Polygon network manually.");
    }
    
    // Re-throw if it's already our custom error
    if (error?.message?.includes("Chain switch did not complete")) {
      throw error;
    }
    
    // Handle other errors
    const errorMessage = error?.message || "Failed to switch to Polygon network";
    throw new Error(`${errorMessage}. Please ensure Polygon is added to your wallet and try again.`);
  }
}

