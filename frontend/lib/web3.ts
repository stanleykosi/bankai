/**
 * @description
 * Wagmi configuration for Web3 wallet connections.
 * Configured for Polygon Mainnet using public RPCs (replace with dedicated
 * providers such as Alchemy/Infura in production).
 * 
 * Phantom Wallet compatibility: Uses reliable public RPC endpoints that
 * Phantom Wallet can connect to for chain switching.
 */

import { createConfig, http } from "wagmi";
import { polygon } from "wagmi/chains";
import { injected, walletConnect } from "wagmi/connectors";

const walletConnectProjectId =
  process.env.NEXT_PUBLIC_WALLETCONNECT_PROJECT_ID || "";

// Use a reliable public RPC endpoint for Polygon
// This ensures Phantom Wallet and other wallets can properly switch chains
const polygonRpcUrl = process.env.NEXT_PUBLIC_POLYGON_RPC_URL || 
  "https://polygon-rpc.com";

export const config = createConfig({
  chains: [polygon],
  transports: {
    [polygon.id]: http(polygonRpcUrl),
  },
  connectors: [
    injected({ shimDisconnect: true }),
    ...(walletConnectProjectId
      ? [
          walletConnect({
            projectId: walletConnectProjectId,
            showQrModal: true,
          }),
        ]
      : []),
  ],
  ssr: true,
});

