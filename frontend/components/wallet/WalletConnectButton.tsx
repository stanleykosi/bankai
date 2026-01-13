/**
 * @description
 * Wallet connect control that surfaces Wagmi connectors (MetaMask/WalletConnect)
 * so users can link, switch, or disconnect EOAs without leaving the terminal UI.
 */

"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useAccount, useConnect, useDisconnect } from "wagmi";
import { Loader2, LogOut, PlugZap } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { useVaultDeployment } from "@/hooks/useVaultDeployment";
import { useWallet } from "@/hooks/useWallet";

const CONNECTOR_LABELS: Record<string, string> = {
  injected: "Browser Wallet",
  walletConnect: "WalletConnect",
  metaMaskSDK: "MetaMask",
  brave: "Brave Wallet",
  phantom: "Phantom",
};

// Helper to get a friendly name for the connector
const getConnectorName = (connector: any): string => {
  // Check connector name first
  if (connector.name) {
    const name = connector.name.toLowerCase();
    if (name.includes("metamask")) return "MetaMask";
    if (name.includes("brave")) return "Brave Wallet";
    if (name.includes("phantom")) return "Phantom";
    if (name.includes("coinbase")) return "Coinbase Wallet";
  }
  
  // Fall back to our labels or connector ID
  return CONNECTOR_LABELS[connector.id] ?? connector.name ?? connector.id;
};

const truncate = (addr: string) => `${addr.slice(0, 6)}...${addr.slice(-4)}`;
const LAST_PROXY_KEY = "bankai:wallet:last-proxy";
const LAST_EOA_KEY = "bankai:wallet:last-eoa";

export function WalletConnectButton() {
  const { address, isConnecting, isReconnecting, isDisconnected } = useAccount();
  const { connect, connectors, error, isPending } = useConnect();
  const { disconnect } = useDisconnect();
  const {
    eoaAddress,
    vaultAddress,
    isAuthenticated,
    isLoading: isWalletLoading,
    refreshUser,
  } = useWallet();
  const [open, setOpen] = useState(false);
  const [connectingId, setConnectingId] = useState<string | null>(null);
  const [cachedProxyAddress, setCachedProxyAddress] = useState<string | null>(null);
  const [cachedEoaAddress, setCachedEoaAddress] = useState<string | null>(null);
  const disconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hasTradingWallet = Boolean(vaultAddress);
  const {
    canDeploy,
    isDeploying,
    deployError,
    deploymentStep,
    deployVault,
  } = useVaultDeployment({
    eoaAddress,
    hasVault: hasTradingWallet,
    isReady: isAuthenticated && !isWalletLoading,
    refreshUser,
  });

  // Filter and sort connectors - show all available connectors
  const availableConnectors = useMemo(() => {
    // Show all connectors - don't filter by ready state
    // Wagmi will handle the connection attempt
    const all = connectors.filter((connector) => {
      // Always show WalletConnect if configured
      if (connector.id === "walletConnect") return true;
      // Show injected connector (it will handle browser wallet detection)
      if (connector.id === "injected") return true;
      // Show any other connectors
      return true;
    });

    // Sort: WalletConnect first, then others by name
    return all.sort((a, b) => {
      if (a.id === "walletConnect") return -1;
      if (b.id === "walletConnect") return 1;
      if (a.id === "injected") return 1; // Put injected last
      if (b.id === "injected") return -1;
      return getConnectorName(a).localeCompare(getConnectorName(b));
    });
  }, [connectors]);

  useEffect(() => {
    if (address) {
      setOpen(false);
      setConnectingId(null);
    }
  }, [address]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }
    const storedProxy = window.sessionStorage.getItem(LAST_PROXY_KEY);
    const storedEoa = window.sessionStorage.getItem(LAST_EOA_KEY);
    if (storedProxy) {
      setCachedProxyAddress(storedProxy);
    }
    if (storedEoa) {
      setCachedEoaAddress(storedEoa);
    }
  }, []);

  useEffect(() => {
    return () => {
      if (disconnectTimerRef.current) {
        clearTimeout(disconnectTimerRef.current);
        disconnectTimerRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    if (vaultAddress && address) {
      setCachedProxyAddress(vaultAddress);
      setCachedEoaAddress(address);
      if (typeof window !== "undefined") {
        window.sessionStorage.setItem(LAST_PROXY_KEY, vaultAddress);
        window.sessionStorage.setItem(LAST_EOA_KEY, address);
      }
    }
  }, [address, vaultAddress]);

  useEffect(() => {
    if (!isDisconnected) {
      if (disconnectTimerRef.current) {
        clearTimeout(disconnectTimerRef.current);
        disconnectTimerRef.current = null;
      }
      return;
    }

    if (disconnectTimerRef.current) {
      return;
    }

    disconnectTimerRef.current = setTimeout(() => {
      setCachedProxyAddress(null);
      setCachedEoaAddress(null);
      if (typeof window !== "undefined") {
        window.sessionStorage.removeItem(LAST_PROXY_KEY);
        window.sessionStorage.removeItem(LAST_EOA_KEY);
      }
      disconnectTimerRef.current = null;
    }, 2000);
  }, [isDisconnected]);

  // Clear connecting state on error and allow modal to be closed
  useEffect(() => {
    if (error) {
      // Clear connecting state after a short delay to show the error
      const timer = setTimeout(() => {
        setConnectingId(null);
      }, 3000);
      return () => clearTimeout(timer);
    }
  }, [error]);

  // Close modal when user clicks outside or presses escape (even during connection)
  const handleOpenChange = (newOpen: boolean) => {
    if (!newOpen) {
      // If closing, clear any pending connection state
      setConnectingId(null);
    }
    setOpen(newOpen);
  };

  const handleConnect = async (connectorId: string) => {
    const connector = connectors.find(({ id }) => id === connectorId);
    
    if (!connector) {
      return;
    }

    setConnectingId(connectorId);
    
    // Set a timeout for WalletConnect connections (they can hang)
    const timeoutId = connectorId === "walletConnect" 
      ? setTimeout(() => {
          setConnectingId(null);
        }, 15000) // 15 second timeout for WalletConnect
      : null;
    
    try {
      await connect({ connector });
      if (timeoutId) clearTimeout(timeoutId);
    } catch (err) {
      console.error("Connection error:", err);
      setConnectingId(null);
      if (timeoutId) clearTimeout(timeoutId);
    }
  };

  const handleDisconnect = () => {
    disconnect();
    setOpen(false);
    setConnectingId(null);
    setCachedProxyAddress(null);
    setCachedEoaAddress(null);
    if (typeof window !== "undefined") {
      window.sessionStorage.removeItem(LAST_PROXY_KEY);
      window.sessionStorage.removeItem(LAST_EOA_KEY);
    }
  };

  // Only disable primary trigger if we actively kicked off a connection
  const isBusy =
    Boolean(connectingId) && (isPending || isConnecting || isReconnecting);
  const matchedCachedProxy =
    address && cachedEoaAddress && cachedProxyAddress
      ? cachedEoaAddress.toLowerCase() === address.toLowerCase()
        ? cachedProxyAddress
        : null
      : null;
  const reconnectProxy =
    !address && (isConnecting || isReconnecting) ? cachedProxyAddress : null;
  const displayAddress = vaultAddress || matchedCachedProxy || reconnectProxy || null;
  const deploymentLabel = useMemo(() => {
    if (deployError) return deployError;
    switch (deploymentStep) {
      case "preparing":
        return "Preparing proxy wallet...";
      case "fetchingPayload":
        return "Fetching signing payload...";
      case "checkingNetwork":
        return "Checking wallet network...";
      case "switchingNetwork":
        return "Switching to Polygon...";
      case "awaitingSignature":
        return "Awaiting signature...";
      case "submitting":
        return "Submitting deployment...";
      case "pollingRelayer":
        return "Confirming deployment...";
      default:
        return null;
    }
  }, [deployError, deploymentStep]);

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button
          variant={address || displayAddress ? "secondary" : "default"}
          size="sm"
          className={cn(
            "font-mono tracking-wide",
            address || displayAddress ? "text-foreground" : "font-bold"
          )}
          disabled={isBusy}
        >
          <PlugZap className="mr-2 h-4 w-4" />
          {displayAddress
            ? truncate(displayAddress)
            : address
              ? "Proxy wallet pending"
              : "Connect Wallet"}
        </Button>
      </DialogTrigger>

      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {address ? "Switch wallet" : "Connect a wallet"}
          </DialogTitle>
          <DialogDescription>
            Choose a wallet provider to authorize trades on Polygon.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          {availableConnectors.length > 0 ? (
            availableConnectors.map((connector) => {
              const isConnectingThis = connectingId === connector.id;
              // Only disable if we're busy connecting a DIFFERENT connector
              const shouldDisable = isBusy && !isConnectingThis;
              
              return (
                <Button
                  key={connector.id}
                  type="button"
                  variant="outline"
                  className={cn(
                    "flex w-full items-center justify-between font-mono transition-all",
                    "hover:bg-accent hover:text-accent-foreground",
                    shouldDisable && "opacity-50 cursor-not-allowed"
                  )}
                  disabled={shouldDisable}
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    if (!shouldDisable) {
                      handleConnect(connector.id);
                    }
                  }}
                >
                  <span className="flex items-center gap-2">
                    {getConnectorName(connector)}
                  </span>
                  {isConnectingThis && (
                    <Loader2 className="h-4 w-4 animate-spin text-primary" />
                  )}
                </Button>
              );
            })
          ) : (
            <p className="text-sm text-muted-foreground text-center py-4">
              No wallet connectors available. Please install a wallet extension.
            </p>
          )}

          {address && (
            <div className="mt-4 rounded-md border border-border bg-muted/20 p-3">
              <div className="text-xs font-mono text-muted-foreground mb-2">
                Proxy wallet
              </div>
              <div className="text-sm font-mono mb-3">
                {displayAddress ? truncate(displayAddress) : "Pending"}
              </div>
              {!hasTradingWallet && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="w-full"
                  disabled={!canDeploy || isDeploying}
                  onClick={() => {
                    void deployVault();
                  }}
                >
                  {isDeploying ? "Setting up..." : "Create proxy wallet"}
                </Button>
              )}
              {deploymentLabel && (
                <p className={cn(
                  "mt-2 text-xs font-mono",
                  deployError ? "text-destructive" : "text-muted-foreground"
                )}>
                  {deploymentLabel}
                </p>
              )}
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="w-full text-destructive hover:text-destructive hover:bg-destructive/10"
                onClick={handleDisconnect}
              >
                <LogOut className="mr-2 h-4 w-4" />
                Disconnect
              </Button>
            </div>
          )}

          {error && (
            <div className="mt-2 rounded-md border border-destructive/50 bg-destructive/10 p-3">
              <p className="text-sm text-destructive font-mono">
                {error.message?.includes("WebSocket") 
                  ? "Connection failed. Please try MetaMask or another browser wallet."
                  : error.message || "Failed to connect wallet. Please try again."}
              </p>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="mt-2 w-full text-xs"
                onClick={() => {
                  setConnectingId(null);
                  setOpen(false);
                }}
              >
                Close
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
