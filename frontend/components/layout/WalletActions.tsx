/**
 * @description
 * Client-only wallet action cluster for the header.
 */

"use client";

import { useState } from "react";
import { Settings } from "lucide-react";

import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { DepositWithdrawModal } from "@/components/wallet/DepositWithdrawModal";
import { BalanceDisplay } from "@/components/wallet/BalanceDisplay";
import { NotificationBell } from "@/components/social/NotificationBell";
import { SettingsModal } from "@/components/settings/SettingsModal";
import { Button } from "@/components/ui/button";
import { useSettings } from "@/hooks/useSettings";
import { useWallet } from "@/hooks/useWallet";
import { cn } from "@/lib/utils";
import { useTerminalStore } from "@/lib/store";

export default function WalletActions() {
  const { isAuthenticated, vaultAddress, uiVaultAddress, isSessionRestoring } = useWallet();
  useSettings();
  const notificationChannel = useTerminalStore((state) => state.notificationChannel());
  const [depositModalOpen, setDepositModalOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const showWalletActions = isAuthenticated || Boolean(uiVaultAddress) || isSessionRestoring;
  const canManageFunds = Boolean(isAuthenticated && vaultAddress);

  return (
    <>
      <WalletConnectButton />

      {isAuthenticated && vaultAddress && (
        <BalanceDisplay className="hidden sm:flex" />
      )}

      {showWalletActions && uiVaultAddress && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn("font-mono", !canManageFunds && "opacity-60")}
          disabled={!canManageFunds}
          onClick={() => {
            if (!canManageFunds) {
              return;
            }
            setDepositModalOpen(true);
          }}
        >
          Funds
        </Button>
      )}

      {showWalletActions && (
        <NotificationBell
          enabled={isAuthenticated && notificationChannel === "IN_APP"}
        />
      )}

      {isAuthenticated && (
        <>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => setSettingsOpen(true)}
            aria-label="Open settings"
          >
            <Settings className="h-5 w-5" />
          </Button>
          <SettingsModal open={settingsOpen} onOpenChange={setSettingsOpen} />
        </>
      )}

      {isAuthenticated && vaultAddress && (
        <DepositWithdrawModal
          open={depositModalOpen}
          onOpenChange={setDepositModalOpen}
        />
      )}
    </>
  );
}
