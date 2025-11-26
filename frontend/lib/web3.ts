/**
 * @description
 * Wagmi configuration for Web3 wallet connections.
 * Configured for Polygon Mainnet using public RPCs (replace with dedicated
 * providers such as Alchemy/Infura in production).
 */

import { createConfig, http } from "wagmi";
import { polygon } from "wagmi/chains";
import { injected } from "wagmi/connectors";

export const config = createConfig({
  chains: [polygon],
  transports: {
    [polygon.id]: http(),
  },
  connectors: [
    injected({ shimDisconnect: true }),
  ],
  ssr: true,
});

