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

const walletConnectProjectId =
  process.env.NEXT_PUBLIC_WALLETCONNECT_PROJECT_ID || "";

// Use a reliable public RPC endpoint for Polygon
// This ensures Phantom Wallet and other wallets can properly switch chains
const polygonRpcUrl = process.env.NEXT_PUBLIC_POLYGON_RPC_URL ||
  "https://polygon-rpc.com";

const getConnectors = () => {
  if (typeof window === "undefined") {
    return [];
  }

  const { injected, walletConnect } =
    require("wagmi/connectors") as typeof import("wagmi/connectors");
  type ConnectorInstance =
    | ReturnType<typeof injected>
    | ReturnType<typeof walletConnect>;
  const connectors: ConnectorInstance[] = [
    injected({ shimDisconnect: true }),
  ];

  if (walletConnectProjectId) {
    connectors.push(
      walletConnect({
        projectId: walletConnectProjectId,
        showQrModal: true,
      })
    );
  }

  return connectors;
};

export const config = createConfig({
  chains: [polygon],
  transports: {
    [polygon.id]: http(polygonRpcUrl),
  },
  connectors: getConnectors(),
  ssr: true,
});
