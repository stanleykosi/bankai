"use client";

/**
 * @description
 * Liquidity rewards dashboard powered by the Polymarket CLOB rewards endpoints.
 */

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Loader2, RefreshCcw } from "lucide-react";

import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useUserApiCredentials } from "@/hooks/useUserApiCredentials";
import { useClobClient } from "@/hooks/useClobClient";
import { useWallet } from "@/hooks/useWallet";
import type { UserRewardsEarning, UserRewardTotal } from "@/types";

interface RewardsSnapshot {
  totals: UserRewardTotal[];
  markets: UserRewardsEarning[];
  date: string;
}

const formatCurrency = (value: number): string => {
  if (!Number.isFinite(value)) return "$0.00";
  if (Math.abs(value) >= 1_000) {
    return `$${value.toFixed(2)}`;
  }
  return `$${value.toFixed(4)}`;
};

const getUtcDateString = (date: Date) => date.toISOString().slice(0, 10);

export function RewardsDashboard() {
  const { user, eoaAddress } = useWallet();
  const { credentials, getCredentials, isLoading: credsLoading } = useUserApiCredentials();
  const { clobClient } = useClobClient({
    credentials,
    vaultAddress: user?.vault_address ?? eoaAddress ?? null,
    walletType: user?.wallet_type ?? null,
  });

  const clobClientRef = useRef(clobClient);
  useEffect(() => {
    clobClientRef.current = clobClient;
  }, [clobClient]);

  const date = useMemo(() => getUtcDateString(new Date()), []);

  const ensureClient = async () => {
    if (clobClientRef.current) return clobClientRef.current;
    if (!credentials) {
      await getCredentials();
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
    if (!clobClientRef.current) {
      throw new Error("Rewards client not ready. Connect a wallet to continue.");
    }
    return clobClientRef.current;
  };

  const rewardsQuery = useQuery({
    queryKey: ["rewards", date, user?.vault_address ?? eoaAddress],
    queryFn: async (): Promise<RewardsSnapshot> => {
      const client = await ensureClient();
      const [totals, markets] = await Promise.all([
        client.getTotalEarningsForUserForDay(date),
        client.getUserEarningsAndMarketsConfig(date, "earnings", "DESC", true),
      ]);
      return { totals, markets, date };
    },
    enabled: Boolean(eoaAddress) && !credsLoading,
    staleTime: 60_000,
    retry: 1,
  });

  const totalEarnings =
    rewardsQuery.data?.totals?.reduce((sum, entry) => sum + entry.earnings, 0) ?? 0;

  const topMarkets =
    rewardsQuery.data?.markets
      ?.map((market) => ({
        ...market,
        total: market.earnings.reduce((sum, earning) => sum + earning.earnings, 0),
      }))
      .sort((a, b) => b.total - a.total)
      .slice(0, 6) ?? [];

  return (
    <Card className="border-border/60 bg-card/70">
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Rewards
            </p>
            <h3 className="text-lg font-semibold text-foreground">Liquidity Rewards</h3>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-2 text-xs"
            onClick={() => rewardsQuery.refetch()}
            disabled={rewardsQuery.isFetching}
          >
            {rewardsQuery.isFetching ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCcw className="h-3.5 w-3.5" />
            )}
            Refresh
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-0">
        {!eoaAddress ? (
          <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
            Connect a wallet to view your liquidity rewards.
          </div>
        ) : rewardsQuery.isLoading ? (
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin text-primary" />
            Loading reward snapshot...
          </div>
        ) : rewardsQuery.isError ? (
          <div className="flex items-center gap-2 text-sm text-destructive">
            <AlertCircle className="h-4 w-4" />
            {rewardsQuery.error instanceof Error
              ? rewardsQuery.error.message
              : "Failed to load rewards."}
          </div>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-4 rounded-md border border-border/60 bg-background/40 p-4">
              <div>
                <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
                  Daily Earnings (UTC)
                </p>
                <p className="text-2xl font-semibold text-foreground">
                  {formatCurrency(totalEarnings)}
                </p>
              </div>
              <div className="text-xs text-muted-foreground">
                Snapshot date: {rewardsQuery.data?.date || date}
                <br />
                Rewards are sampled hourly and paid daily around midnight UTC.
              </div>
            </div>

            {topMarkets.length === 0 ? (
              <div className="rounded-md border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
                No rewards earned for this snapshot yet.
              </div>
            ) : (
              <div className="space-y-3">
                {topMarkets.map((market) => (
                  <div
                    key={market.condition_id}
                    className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border/60 bg-background/30 p-3 text-sm"
                  >
                    <div className="min-w-[200px]">
                      <p className="truncate font-medium text-foreground">
                        {market.question || market.market_slug || "Market rewards"}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        Spread max {market.rewards_max_spread} • Min size {market.rewards_min_size}
                      </p>
                    </div>
                    <div className="text-right font-mono text-sm text-foreground">
                      {formatCurrency(market.total)}
                      <div className="text-xs text-muted-foreground">
                        {Number.isFinite(market.earning_percentage)
                          ? market.earning_percentage.toFixed(2)
                          : "0.00"}
                        % share
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
