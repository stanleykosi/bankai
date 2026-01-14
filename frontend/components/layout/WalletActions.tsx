/**
 * @description
 * Client-only wallet action cluster for the header.
 */

"use client";

import { useState } from "react";

import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { DepositWithdrawModal } from "@/components/wallet/DepositWithdrawModal";
import { BalanceDisplay } from "@/components/wallet/BalanceDisplay";
import { NotificationBell } from "@/components/social/NotificationBell";
import { Button } from "@/components/ui/button";
import { useWallet } from "@/hooks/useWallet";
import { cn } from "@/lib/utils";

export default function WalletActions() {
  const { isAuthenticated, vaultAddress, uiVaultAddress, isSessionRestoring } = useWallet();
  const [depositModalOpen, setDepositModalOpen] = useState(false);
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

      {showWalletActions && <NotificationBell enabled={isAuthenticated} />}

      {isAuthenticated && vaultAddress && (
        <DepositWithdrawModal
          open={depositModalOpen}
          onOpenChange={setDepositModalOpen}
        />
      )}
    </>
  );
}
