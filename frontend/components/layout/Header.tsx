/**
 * @description
 * Application header that unifies navigation, user identity, and wallet state.
 * Shows wallet auth status, Wagmi wallet info, and leaves room for soon-to-ship
 * actions like deposits.
 */

"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, BarChart2, LayoutDashboard, Wallet } from "lucide-react";

import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { DepositWithdrawModal } from "@/components/wallet/DepositWithdrawModal";
import { NotificationBell } from "@/components/social/NotificationBell";
import { Button } from "@/components/ui/button";
import { useWallet } from "@/hooks/useWallet";
import { cn } from "@/lib/utils";

const navLinks = [
  { href: "/dashboard", label: "Radar", Icon: LayoutDashboard },
  { href: "/portfolio", label: "Portfolio", Icon: Wallet },
  { href: "/analysis", label: "Analysis", Icon: BarChart2 },
];

export function Header() {
  const pathname = usePathname();
  const { isAuthenticated, vaultAddress, uiVaultAddress, isSessionRestoring } = useWallet();
  const [depositModalOpen, setDepositModalOpen] = useState(false);
  const showWalletActions = isAuthenticated || isSessionRestoring;
  const canManageFunds = Boolean(isAuthenticated && vaultAddress);

  return (
    <header className="sticky top-0 z-50 w-full border-b border-border bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-14 w-full max-w-[1600px] items-center justify-between px-4">
        {/* Branding + Navigation */}
        <div className="flex items-center">
          <Link className="mr-8 flex items-center space-x-2" href="/dashboard">
            <div className="relative flex h-8 w-8 items-center justify-center rounded-sm bg-primary/10">
              <Activity className="h-5 w-5 text-primary" />
            </div>
            <span className="hidden font-mono text-lg font-bold tracking-tighter sm:inline-block">
              BANKAI<span className="text-primary">.TERMINAL</span>
            </span>
          </Link>

          <nav className="flex items-center space-x-6 text-sm font-medium">
            {navLinks.map(({ href, label, Icon }) => (
              <Link
                key={href}
                href={href}
                className={cn(
                  "flex items-center gap-2 transition-colors hover:text-primary",
                  pathname === href ? "text-foreground" : "text-foreground/60"
                )}
              >
                <Icon className="h-4 w-4" />
                {label}
              </Link>
            ))}
          </nav>
        </div>

        {/* User / Wallet Actions */}
        <div className="flex items-center space-x-3">
          <WalletConnectButton />

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
        </div>
      </div>
    </header>
  );
}
