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
import { useWallet } from "@/hooks/useWallet";
import { useVaultDeployment } from "@/hooks/useVaultDeployment";
import { useBalance } from "@/hooks/useBalance";
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
  const {
    isAuthenticated,
    isLoading,
    eoaAddress,
    vaultAddress,
    user,
    walletError,
    refreshUser,
  } = useWallet();
  const {
    data: balanceData,
    isLoading: isBalanceLoading,
  } = useBalance();
  const [depositModalOpen, setDepositModalOpen] = useState(false);
  const walletTypeLabel = useMemo(
    () => (user?.wallet_type ? user.wallet_type : "VAULT"),
    [user?.wallet_type]
  );
  const hasVault = Boolean(vaultAddress);
  const {
    canDeploy,
    isDeploying: isVaultDeploying,
    deployError,
    deploymentStatus,
    deployVault,
  } = useVaultDeployment({
    eoaAddress,
    hasVault,
    isReady: !isLoading,
    refreshUser,
  });
  const showVaultCard = isAuthenticated && Boolean(eoaAddress);
  const deploymentMessage = useMemo(() => {
    if (deploymentStatus?.proxy_address) {
      return `Vault pending at ${truncateAddress(
        deploymentStatus.proxy_address
      )}`;
    }
    if (deploymentStatus?.state) {
      return `Relayer: ${deploymentStatus.state}`;
    }
    if (deploymentStatus?.task_id) {
      return `Relayer task ${deploymentStatus.task_id}`;
    }
    return null;
  }, [deploymentStatus]);

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

              {showVaultCard && (
                <button
                  type="button"
                  onClick={() =>
                    hasVault ? setDepositModalOpen(true) : deployVault()
                  }
                  disabled={
                    isVaultDeploying ||
                    isLoading ||
                    (hasVault ? false : !canDeploy)
                  }
                  className={cn(
                    "hidden md:flex h-10 items-center gap-2.5 rounded-md border border-border bg-card/70 px-2.5 py-1.5 text-left transition hover:border-primary/60 hover:bg-card",
                    !hasVault && "border-dashed opacity-75"
                  )}
                >
                  {walletError ? (
                    <span className="font-mono text-[10px] text-destructive whitespace-nowrap max-w-[200px] truncate">
                      {walletError}
                    </span>
                  ) : (
                    <div className="flex items-center gap-2.5 min-w-0 w-full">
                      <div className="flex flex-col gap-0.5 min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-2">
                          <span className="font-mono text-[9px] uppercase tracking-wider text-muted-foreground whitespace-nowrap">
                            {walletTypeLabel}
                          </span>
                          <span className="font-mono text-[9px] text-muted-foreground whitespace-nowrap">
                            {hasVault
                              ? isBalanceLoading
                                ? "Loading..."
                                : `${balanceData?.balance_formatted ?? "0.00"} USDC`
                              : ""}
                          </span>
                        </div>
                        <div className="flex items-center gap-1.5 min-w-0">
                          <span className="font-mono text-[11px] text-foreground truncate">
                            {hasVault && vaultAddress
                              ? truncateAddress(vaultAddress)
                              : isVaultDeploying
                              ? "Authorizing vault deployment..."
                              : deploymentMessage ||
                                (deployError || isLoading
                                  ? "Syncing wallet..."
                                  : "Preparing deployment...")}
                          </span>
                          {hasVault && (
                            <span className="flex items-center gap-0.5 text-[8px] uppercase text-constructive whitespace-nowrap shrink-0">
                              <ShieldCheck className="h-2.5 w-2.5" />
                              Active
                            </span>
                          )}
                        </div>
                        {!hasVault && deployError && (
                          <span className="text-[9px] text-destructive">
                            {deployError}
                          </span>
                        )}
                      </div>
                    </div>
                  )}
                </button>
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

