"use client";

/**
 * @description
 * Following Performance page.
 * Lists traders the user follows, ranked by total PnL (realized + unrealized).
 */

import Link from "next/link";
import Image from "next/image";
import { Loader2, ArrowUpRight } from "lucide-react";
import { useWallet } from "@/hooks/useWallet";

import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useFollowingPerformance } from "@/hooks/useFollow";
import type { FollowPerformance } from "@/types";

function formatCurrency(value: number): string {
  if (!Number.isFinite(value)) return "$0.00";
  if (Math.abs(value) >= 1_000_000) return `$${(value / 1_000_000).toFixed(2)}M`;
  if (Math.abs(value) >= 1_000) return `$${(value / 1_000).toFixed(1)}K`;
  return `$${value.toFixed(2)}`;
}

function formatPercent(value?: number): string {
  if (typeof value !== "number") return "--";
  return `${value.toFixed(1)}%`;
}

function TraderRow({ trader, index }: { trader: FollowPerformance; index: number }) {
  const isPositive = (trader.total_pnl || 0) >= 0;
  const pnlColor = isPositive ? "text-emerald-400" : "text-rose-400";

  return (
    <div className="group rounded-xl border border-border/60 bg-card/70 p-4 transition hover:border-primary/50 hover:bg-card/80">
      <div className="flex items-center gap-4">
        <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-border/60 bg-background/60 font-mono text-xs text-muted-foreground">
          #{index + 1}
        </div>
        <div className="flex flex-1 items-center gap-3 overflow-hidden">
          <div className="relative h-12 w-12 overflow-hidden rounded-xl border border-border/60 bg-background/50">
            {trader.profile_image ? (
              <Image src={trader.profile_image} alt={trader.profile_name || trader.target_address} fill className="object-cover" />
            ) : (
              <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-primary/10 to-primary/30 text-lg font-semibold text-primary">
                {(trader.profile_name || trader.target_address).charAt(0).toUpperCase()}
              </div>
            )}
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <p className="truncate text-base font-semibold text-foreground">
                {trader.profile_name || `${trader.target_address.slice(0, 6)}...${trader.target_address.slice(-4)}`}
              </p>
            </div>
            <p className="font-mono text-xs text-muted-foreground">
              {`${trader.target_address.slice(0, 8)}...${trader.target_address.slice(-6)}`}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-6">
          <div className="text-right">
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">Total PnL</p>
            <p className={`font-mono text-lg ${pnlColor}`}>{formatCurrency(trader.total_pnl)}</p>
          </div>
          <div className="text-right">
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">Win Rate</p>
            <p className="font-mono text-lg text-foreground">{formatPercent(trader.stats?.win_rate)}</p>
          </div>
          <div className="text-right hidden md:block">
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">Predictions</p>
            <p className="font-mono text-lg text-foreground">{trader.stats?.predictions ?? 0}</p>
          </div>
        </div>
        <Link
          href={`/profile/${trader.target_address}`}
          className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border/60 text-muted-foreground transition hover:border-primary/60 hover:text-primary"
        >
          <ArrowUpRight className="h-4 w-4" />
        </Link>
      </div>
    </div>
  );
}

export default function FollowingPerformancePage() {
  const { isAuthenticated, isLoading: isAuthLoading } = useWallet();
  const { data, isLoading, isError, refetch, isFetching } = useFollowingPerformance();

  const following = data?.following ?? [];

  return (
    <div className="relative">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top,_rgba(41,121,255,0.12),_transparent_55%)]" />
      <div className="pointer-events-none absolute inset-0 bg-[linear-gradient(to_right,rgba(255,255,255,0.03)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.03)_1px,transparent_1px)] bg-[size:48px_48px] opacity-20" />
      <div className="relative container max-w-6xl space-y-8 py-8">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">Dashboard</p>
            <h1 className="text-2xl font-semibold text-foreground">Following Performance</h1>
            <p className="text-sm text-muted-foreground">
              Ranked view of traders you follow. Subscriptions keep this list live with their latest results.
            </p>
          </div>
          <div className="flex items-center gap-3">
            <Button
              variant="outline"
              size="sm"
              onClick={() => refetch()}
              disabled={isFetching}
              className="font-mono text-xs"
            >
              {isFetching ? <Loader2 className="h-4 w-4 animate-spin" /> : "Refresh"}
            </Button>
            <Button asChild variant="ghost" size="sm" className="font-mono text-xs">
              <Link href="/dashboard">Back</Link>
            </Button>
          </div>
        </div>

        {!isAuthenticated ? (
          <Card className="border-border/60 bg-card/70">
            <CardContent className="py-8 text-center text-muted-foreground">
              {isAuthLoading ? "Loading session..." : "Connect a wallet to view the traders you follow."}
            </CardContent>
          </Card>
        ) : isLoading ? (
          <Card className="border-border/60 bg-card/70">
            <CardHeader className="pb-3">
              <div className="h-5 w-48 animate-pulse rounded bg-muted/40" />
            </CardHeader>
            <CardContent className="space-y-3">
              {[...Array(4)].map((_, idx) => (
                <div key={idx} className="h-16 animate-pulse rounded-lg bg-muted/30" />
              ))}
            </CardContent>
          </Card>
        ) : isError ? (
          <Card className="border-border/60 bg-card/70">
            <CardContent className="py-8 text-center">
              <p className="text-sm text-destructive">Failed to load following performance.</p>
              <Button variant="outline" size="sm" className="mt-3" onClick={() => refetch()}>
                Retry
              </Button>
            </CardContent>
          </Card>
        ) : following.length === 0 ? (
          <Card className="border-border/60 bg-card/70">
            <CardContent className="py-8 text-center text-muted-foreground">
              You are not following any traders yet.
            </CardContent>
          </Card>
        ) : (
          <div className="space-y-3">
            {following.map((trader, idx) => (
              <TraderRow key={trader.target_address} trader={trader} index={idx} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
