"use client";

/**
 * @description
 * Pro Trader Profile Page.
 * Displays trader identity, performance stats, activity heatmap, and positions.
 */

import { useParams } from "next/navigation";
import Link from "next/link";
import { ArrowLeft, Loader2, RefreshCcw, AlertCircle } from "lucide-react";

import { Button } from "@/components/ui/button";
import { ProfileHeader } from "@/components/profile/ProfileHeader";
import { StatsCards } from "@/components/profile/StatsCards";
import { ActivityHeatmap } from "@/components/profile/ActivityHeatmap";
import { PositionsSpy } from "@/components/profile/PositionsSpy";
import { RecentTrades } from "@/components/profile/RecentTrades";

import {
  useTraderProfile,
  useTraderPositions,
  useActivityHeatmap,
  useRecentTrades,
} from "@/hooks/useTraderProfile";

export default function TraderProfilePage() {
  const params = useParams<{ address: string }>();
  const address = params?.address;

  const {
    data: profileData,
    isLoading: isLoadingProfile,
    isError: isProfileError,
    refetch: refetchProfile,
    isFetching: isFetchingProfile,
  } = useTraderProfile(address);

  const {
    data: positionsData,
    isLoading: isLoadingPositions,
  } = useTraderPositions(address);

  const {
    data: activityData,
    isLoading: isLoadingActivity,
  } = useActivityHeatmap(address);

  const {
    data: tradesData,
    isLoading: isLoadingTrades,
  } = useRecentTrades(address, 15);

  const isLoading = isLoadingProfile || !address;

  if (isLoading) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="flex flex-col items-center gap-3 font-mono text-sm text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
          <p>Loading trader profile...</p>
        </div>
      </div>
    );
  }

  if (isProfileError || !profileData) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="flex flex-col items-center gap-4 text-center">
          <AlertCircle className="h-10 w-10 text-destructive" />
          <p className="font-mono text-sm text-destructive">
            Unable to load trader profile.
          </p>
          <div className="flex gap-2">
            <Button
              variant="outline"
              onClick={() => refetchProfile()}
              className="flex items-center gap-2"
            >
              <RefreshCcw className="h-4 w-4" />
              Retry
            </Button>
            <Button asChild variant="ghost">
              <Link href="/dashboard">Back to Dashboard</Link>
            </Button>
          </div>
        </div>
      </div>
    );
  }

  const { profile, follower_count } = profileData;

  return (
    <div className="relative">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top,_rgba(41,121,255,0.12),_transparent_55%)]" />
      <div className="pointer-events-none absolute inset-0 bg-[linear-gradient(to_right,rgba(255,255,255,0.03)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.03)_1px,transparent_1px)] bg-[size:48px_48px] opacity-20" />
      <div className="relative container max-w-6xl py-8 space-y-8">
        {/* Back Button */}
        <div className="flex items-center justify-between">
          <Button asChild variant="ghost" className="font-mono text-xs gap-2">
            <Link href="/dashboard">
              <ArrowLeft className="h-4 w-4" />
              Dashboard
            </Link>
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="font-mono text-xs"
            onClick={() => refetchProfile()}
            disabled={isFetchingProfile}
          >
            {isFetchingProfile ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCcw className="h-4 w-4" />
            )}
            <span className="ml-2">Refresh</span>
          </Button>
        </div>

        <section className="space-y-4 animate-in fade-in slide-in-from-bottom-2 duration-700">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Trader profile
            </p>
            <h2 className="text-xl font-semibold text-foreground">Profile Overview</h2>
          </div>
          <ProfileHeader profile={profile} followerCount={follower_count} />
        </section>

        <section className="space-y-4 animate-in fade-in slide-in-from-bottom-2 duration-700 delay-100">
          <div>
            <p className="text-[10px] uppercase tracking-[0.3em] text-muted-foreground">
              Performance
            </p>
            <h2 className="text-xl font-semibold text-foreground">Trading Snapshot</h2>
          </div>
          <StatsCards stats={profile.stats} isLoading={isLoadingProfile} />
        </section>

        <section className="grid gap-6 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
          <div className="space-y-6">
            <div className="animate-in fade-in slide-in-from-bottom-2 duration-700 delay-150">
              <PositionsSpy
                positions={positionsData?.positions}
                isLoading={isLoadingPositions}
              />
            </div>
            <div className="animate-in fade-in slide-in-from-bottom-2 duration-700 delay-200">
              <ActivityHeatmap
                activity={activityData?.activity}
                isLoading={isLoadingActivity}
              />
            </div>
          </div>
          <div className="animate-in fade-in slide-in-from-bottom-2 duration-700 delay-250">
            <RecentTrades trades={tradesData?.trades} isLoading={isLoadingTrades} />
          </div>
        </section>
      </div>
    </div>
  );
}
