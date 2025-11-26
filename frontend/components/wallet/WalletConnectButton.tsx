/**
 * @description
 * Wallet connect control that surfaces Wagmi connectors (MetaMask/WalletConnect)
 * so users can link, switch, or disconnect EOAs without leaving the terminal UI.
 */

"use client";

import { useEffect, useMemo, useState } from "react";
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

const CONNECTOR_LABELS: Record<string, string> = {
  injected: "MetaMask / Browser Wallet",
  walletConnect: "WalletConnect (QR / Mobile)",
};

const truncate = (addr: string) => `${addr.slice(0, 6)}...${addr.slice(-4)}`;

export function WalletConnectButton() {
  const { address, isConnecting, isReconnecting, status } = useAccount();
  const { connect, connectors, error, status: connectStatus } = useConnect();
  const { disconnect } = useDisconnect();
  const [open, setOpen] = useState(false);

  const availableConnectors = useMemo(
    () =>
      connectors.filter((connector) => {
        if (connector.id === "injected") {
          return connector.ready;
        }
        return true;
      }),
    [connectors]
  );

  useEffect(() => {
    if (address) {
      setOpen(false);
    }
  }, [address]);

  const handleConnect = async (connectorId: string) => {
    const connector = connectors.find(({ id }) => id === connectorId);
    if (!connector) return;
    connect({ connector });
  };

  const handleDisconnect = () => {
    disconnect();
    setOpen(false);
  };

  const busy =
    connectStatus === "pending" ||
    status === "connecting" ||
    status === "reconnecting" ||
    isConnecting ||
    isReconnecting;

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          variant={address ? "secondary" : "default"}
          size="sm"
          className={cn(
            "font-mono tracking-wide",
            address ? "text-foreground" : "font-bold"
          )}
          disabled={busy}
        >
          <PlugZap className="mr-2 h-4 w-4" />
          {address ? truncate(address) : "Connect Wallet"}
        </Button>
      </DialogTrigger>

      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {address ? "Switch wallet" : "Connect a wallet"}
          </DialogTitle>
          <DialogDescription>
            Choose a wallet provider to authorize trades on Polygon.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          {availableConnectors.map((connector) => (
            <Button
              key={connector.id}
              variant="outline"
              className="flex w-full items-center justify-between font-mono"
              disabled={!connector.ready || busy}
              onClick={() => handleConnect(connector.id)}
            >
              <span className="flex items-center gap-2">
                {CONNECTOR_LABELS[connector.id] ?? connector.name}
                {busy && <Loader2 className="h-3 w-3 animate-spin" />}
              </span>
            </Button>
          ))}

          {availableConnectors.length === 0 && (
            <p className="text-sm text-muted-foreground">
              No wallet connectors are configured. Add one in `lib/web3.ts`.
            </p>
          )}

          {address && (
            <div className="rounded-md border border-border bg-muted/20 p-3">
              <div className="text-xs font-mono text-muted-foreground">
                Connected as {truncate(address)}
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="mt-2 w-full text-destructive hover:text-destructive"
                onClick={handleDisconnect}
              >
                <LogOut className="mr-2 h-4 w-4" />
                Disconnect
              </Button>
            </div>
          )}

          {error && (
            <p className="text-sm text-destructive">
              {error.message || "Failed to connect wallet."}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

