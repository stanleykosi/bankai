/**
 * @description
 * Wagmi configuration for Web3 wallet connections (Metamask, etc.)
 * This is a placeholder - will be configured with proper chain settings in later steps.
 */

import { createConfig, http } from "wagmi";
import { polygon } from "wagmi/chains";

// Placeholder configuration - will be expanded with proper RPC endpoints
export const config = createConfig({
  chains: [polygon],
  transports: {
    [polygon.id]: http(),
  },
});

