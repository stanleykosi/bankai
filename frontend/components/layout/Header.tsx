/**
 * @description
 * Application header that unifies navigation, user identity, and wallet state.
 * Shows Clerk auth status, Wagmi wallet info, and leaves room for soon-to-ship
 * actions like deposits.
 */

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { SignInButton, UserButton } from "@clerk/nextjs";
import { Activity, BarChart2, LayoutDashboard, ShieldCheck, Wallet } from "lucide-react";

import { Button } from "@/components/ui/button";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
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

              {!isLoading && (
                <>
                  <div className="hidden items-center gap-3 rounded-md border border-border bg-card/50 px-3 py-1.5 md:flex">
                    <div className="flex flex-col text-xs">
                      <span className="font-mono text-muted-foreground">
                        {eoaAddress ? truncateAddress(eoaAddress) : "No Wallet"}
                      </span>
                      {vaultAddress ? (
                        <span className="flex items-center gap-1 font-mono text-[10px] uppercase text-constructive">
                          <ShieldCheck className="h-3 w-3" />
                          {(user?.wallet_type ?? "VAULT")} ACTIVE
                        </span>
                      ) : (
                        <span className="font-mono text-[10px] uppercase text-muted-foreground">
                          Wallet not synced
                        </span>
                      )}
                    </div>
                  </div>

                  <Button
                    variant="outline"
                    size="sm"
                    className="hidden border-primary/30 text-xs font-mono uppercase tracking-wider hover:bg-primary/10 hover:text-primary md:flex"
                  >
                    Deposit
                  </Button>
                </>
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

