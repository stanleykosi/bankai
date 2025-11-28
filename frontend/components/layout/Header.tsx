/**
 * @description
 * Application header that unifies navigation, user identity, and wallet state.
 * Shows Clerk auth status, Wagmi wallet info, and leaves room for soon-to-ship
 * actions like deposits.
 */

"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { SignInButton, UserButton } from "@clerk/nextjs";
import { Activity, BarChart2, LayoutDashboard, ShieldCheck, Wallet } from "lucide-react";

import { Button } from "@/components/ui/button";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { DepositWithdrawModal } from "@/components/wallet/DepositWithdrawModal";
import { BalanceDisplay } from "@/components/wallet/BalanceDisplay";
import { useWallet } from "@/hooks/useWallet";
import { cn } from "@/lib/utils";

const navLinks = [
  { href: "/dashboard", label: "Radar", Icon: LayoutDashboard },
  { href: "/portfolio", label: "Portfolio", Icon: Wallet },
  { href: "/analysis", label: "Analysis", Icon: BarChart2 },
];

const truncateAddress = (address: string) =>
  `${address.slice(0, 6)}...${address.slice(-4)}`;

export function Header() {
  const pathname = usePathname();
  const { isAuthenticated, isLoading, eoaAddress, vaultAddress, user } = useWallet();
  const [depositModalOpen, setDepositModalOpen] = useState(false);
  const walletTypeLabel = useMemo(
    () => (user?.wallet_type ? user.wallet_type : "VAULT"),
    [user?.wallet_type]
  );
  const hasVault = Boolean(vaultAddress);

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
          {isAuthenticated ? (
            <>
              <WalletConnectButton />

              {/* Wallet Status - Compact */}
              <div className="hidden items-center gap-2 rounded-md border border-border bg-card/50 px-3 py-2 md:flex">
                <div className="flex flex-col text-xs">
                  {isLoading ? (
                    <span className="font-mono text-muted-foreground animate-pulse text-[10px]">
                      Syncing...
                    </span>
                  ) : (
                    <>
                      <span className="text-[9px] uppercase text-muted-foreground">
                        Vault Address
                      </span>
                      <span className="font-mono text-xs text-foreground">
                        {hasVault && vaultAddress
                          ? truncateAddress(vaultAddress)
                          : "Pending deployment"}
                      </span>
                      <span
                        className={cn(
                          "mt-1 flex items-center gap-1 font-mono text-[9px] uppercase",
                          hasVault ? "text-constructive" : "text-muted-foreground"
                        )}
                      >
                        {hasVault ? (
                          <>
                            <ShieldCheck className="h-2.5 w-2.5" />
                            {walletTypeLabel} ACTIVE
                          </>
                        ) : (
                          "Connect wallet to deploy"
                        )}
                      </span>
                    </>
                  )}
                </div>
              </div>

              {/* Balance Display */}
              {!isLoading && <BalanceDisplay className="hidden md:flex" />}

              {/* Deposit/Withdraw Button */}
              {!isLoading && (
                <Button
                  variant={hasVault ? "outline" : "secondary"}
                  size="sm"
                  onClick={() => setDepositModalOpen(true)}
                  className={cn(
                    "border-primary/30 text-xs font-mono uppercase tracking-wider hover:bg-primary/10 hover:text-primary",
                    !hasVault && "border-dashed text-muted-foreground"
                  )}
                >
                  {hasVault ? "Funds" : "Vault Setup"}
                </Button>
              )}

              <UserButton
                afterSignOutUrl="/"
                appearance={{
                  elements: {
                    avatarBox: "h-8 w-8 rounded-sm border border-border",
                    userButtonPopoverCard: "border border-border bg-card shadow-xl",
                  },
                }}
              />

              <DepositWithdrawModal
                open={depositModalOpen}
                onOpenChange={setDepositModalOpen}
              />
            </>
          ) : isLoading ? (
            <div className="h-8 w-24 animate-pulse rounded bg-muted" />
          ) : (
            <SignInButton mode="modal">
              <Button size="sm" className="font-mono font-bold tracking-wide">
                Sign In
              </Button>
            </SignInButton>
          )}
        </div>
      </div>
    </header>
  );
}

