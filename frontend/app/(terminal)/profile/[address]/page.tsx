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

import { useTraderProfile, useTraderPositions, useActivityHeatmap } from "@/hooks/useTraderProfile";

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
    <div className="container max-w-6xl py-6 space-y-6">
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

      {/* Profile Header */}
      <ProfileHeader profile={profile} followerCount={follower_count} />

      {/* Stats Cards */}
      <StatsCards stats={profile.stats} isLoading={false} />

      {/* Activity Heatmap */}
      <ActivityHeatmap
        activity={activityData?.activity}
        isLoading={isLoadingActivity}
      />

      {/* Positions Spy */}
      <PositionsSpy
        positions={positionsData?.positions}
        isLoading={isLoadingPositions}
      />
    </div>
  );
}
