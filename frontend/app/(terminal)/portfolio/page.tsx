"use client";

/**
 * @description
 * Portfolio page for connected wallets.
 * Displays open/closed positions, rewards, and CTF operations.
 */

import { useMemo } from "react";
import { AlertCircle, Loader2, RefreshCcw, Wallet } from "lucide-react";

import { Button } from "@/components/ui/button";
import { StatsCards } from "@/components/profile/StatsCards";
import { PositionsTable } from "@/components/portfolio/PositionsTable";
import { ClosedPositionsTable } from "@/components/portfolio/ClosedPositionsTable";
import { RewardsDashboard } from "@/components/portfolio/RewardsDashboard";
import { MergeSplitForm } from "@/components/portfolio/MergeSplitForm";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { useWallet } from "@/hooks/useWallet";
import {
  useTraderClosedPositions,
  useTraderPositions,
  useTraderStats,
} from "@/hooks/useTraderProfile";

const truncate = (value: string) =>
  `${value.slice(0, 6)}...${value.slice(-4)}`;

export default function PortfolioPage() {
  const {
    eoaAddress,
    vaultAddress,
    isLoading,
    isSessionRestoring,
    walletError,
  } = useWallet();

  const portfolioAddress = vaultAddress ?? eoaAddress ?? "";
  const statsQuery = useTraderStats(portfolioAddress || undefined);
  const positionsQuery = useTraderPositions(
    portfolioAddress || undefined,
    200,
    0,
    "CURRENT"
  );
  const closedPositionsQuery = useTraderClosedPositions(
    portfolioAddress || undefined,
    200,
    0
  );

  const conditionIds = useMemo(() => {
    const set = new Set<string>();
    positionsQuery.data?.positions?.forEach((position) => {
      if (position.conditionId) {
        set.add(position.conditionId);
      }
    });
    closedPositionsQuery.data?.positions?.forEach((position) => {
      if (position.conditionId) {
        set.add(position.conditionId);
      }
    });
    return Array.from(set);
  }, [positionsQuery.data?.positions, closedPositionsQuery.data?.positions]);

  const refreshAll = () => {
    void statsQuery.refetch();
    void positionsQuery.refetch();
    void closedPositionsQuery.refetch();
  };

  if (isLoading || isSessionRestoring) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="flex flex-col items-center gap-3 font-mono text-sm text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
          <p>Syncing wallet session...</p>
        </div>
      </div>
    );
  }

  if (!portfolioAddress) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center px-4">
        <div className="w-full max-w-md rounded-lg border border-border/60 bg-card/70 p-6 text-center">
          <Wallet className="mx-auto h-8 w-8 text-primary" />
          <h2 className="mt-4 text-lg font-semibold text-foreground">
            Connect a wallet to view your portfolio
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Portfolio data, rewards, and CTF operations require a connected wallet.
          </p>
          <div className="mt-4 flex justify-center">
            <WalletConnectButton />
          </div>
          {walletError && (
            <p className="mt-4 text-xs text-destructive">{walletError}</p>
          )}
        </div>
      </div>
    );
  }

  const positionsError =
    positionsQuery.error instanceof Error ? positionsQuery.error.message : null;
  const closedError =
    closedPositionsQuery.error instanceof Error
      ? closedPositionsQuery.error.message
      : null;

  return (
    <div className="relative">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top,_rgba(255,211,102,0.12),_transparent_55%)]" />
      <div className="pointer-events-none absolute inset-0 bg-[linear-gradient(to_right,rgba(255,255,255,0.03)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.03)_1px,transparent_1px)] bg-[size:48px_48px] opacity-20" />
      <div className="relative container max-w-6xl space-y-8 py-8">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Portfolio
            </p>
            <h1 className="text-2xl font-semibold text-foreground">
              Your Positions & Rewards
            </h1>
            <div className="mt-2 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
              <span className="font-mono">
                Using {vaultAddress ? "vault" : "wallet"}:{" "}
                {truncate(portfolioAddress)}
              </span>
              {vaultAddress && eoaAddress && (
                <span className="font-mono">EOA: {truncate(eoaAddress)}</span>
              )}
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-2 text-xs"
            onClick={refreshAll}
            disabled={
              statsQuery.isFetching ||
              positionsQuery.isFetching ||
              closedPositionsQuery.isFetching
            }
          >
            {statsQuery.isFetching ||
            positionsQuery.isFetching ||
            closedPositionsQuery.isFetching ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCcw className="h-3.5 w-3.5" />
            )}
            Refresh
          </Button>
        </div>

        {statsQuery.isError ? (
          <div className="flex items-center gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-4 text-sm text-destructive">
            <AlertCircle className="h-4 w-4" />
            {statsQuery.error instanceof Error
              ? statsQuery.error.message
              : "Failed to load portfolio stats."}
          </div>
        ) : (
          <StatsCards
            stats={statsQuery.data?.stats}
            isLoading={statsQuery.isLoading}
          />
        )}

        <section className="grid gap-6 lg:grid-cols-2">
          <PositionsTable
            positions={positionsQuery.data?.positions}
            isLoading={positionsQuery.isLoading}
            error={positionsError}
          />
          <ClosedPositionsTable
            positions={closedPositionsQuery.data?.positions}
            isLoading={closedPositionsQuery.isLoading}
            error={closedError}
          />
        </section>

        <section className="grid gap-6 lg:grid-cols-2">
          <RewardsDashboard />
          <MergeSplitForm conditionIds={conditionIds} />
        </section>
      </div>
    </div>
  );
}
