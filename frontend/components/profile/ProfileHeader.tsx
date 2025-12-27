"use client";

/**
 * @description
 * Profile Header component displaying trader identity info.
 * Shows avatar, name, verification badge, and follow button.
 */

import { useState } from "react";
import Image from "next/image";
import { Calendar, Check, CheckCircle2, Copy, Users } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useFollowToggle } from "@/hooks/useFollow";
import { useAuth } from "@clerk/nextjs";
import type { TraderProfile } from "@/types";

interface ProfileHeaderProps {
  profile: TraderProfile;
  followerCount: number;
}

export function ProfileHeader({ profile, followerCount }: ProfileHeaderProps) {
  const { isSignedIn } = useAuth();
  const { isFollowing, isLoading, toggle } = useFollowToggle(profile.address);
  const [copied, setCopied] = useState(false);

  const displayName =
    (profile.profile_name && profile.profile_name.trim()) ||
    profile.ens_name ||
    `${profile.address.slice(0, 6)}...${profile.address.slice(-4)}`;

  const joinedAtLabel = (() => {
    if (!profile.joined_at) return null;
    const date = new Date(profile.joined_at);
    if (Number.isNaN(date.getTime())) return null;
    return date.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  })();

  const copyAddress = async () => {
    await navigator.clipboard.writeText(profile.address);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Card className="relative overflow-hidden border-border/60 bg-card/70">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top,_rgba(41,121,255,0.12),_transparent_55%)]" />
      <CardContent className="relative p-6">
        <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex items-start gap-4">
            {/* Avatar */}
            <div className="relative h-20 w-20 shrink-0 overflow-hidden rounded-2xl border border-border/60 bg-background/40">
              {profile.profile_image ? (
                <Image
                  src={profile.profile_image}
                  alt={displayName}
                  fill
                  className="object-cover"
                />
              ) : (
                <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-primary/10 to-primary/30 text-2xl font-semibold text-primary">
                  {displayName.charAt(0).toUpperCase()}
                </div>
              )}
            </div>

            {/* Identity Info */}
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-semibold text-foreground">{displayName}</h1>
                {profile.is_verified && (
                  <CheckCircle2 className="h-5 w-5 text-primary" />
                )}
              </div>

              {/* Address */}
              <button
                onClick={copyAddress}
                className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                <span>{`${profile.address.slice(0, 10)}...${profile.address.slice(-8)}`}</span>
                {copied ? (
                  <Check className="h-3 w-3 text-emerald-400" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
              </button>

              {/* ENS / Lens Handle */}
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                {profile.ens_name && (
                  <span className="rounded-full border border-border/60 bg-background/40 px-2 py-0.5">
                    {profile.ens_name}
                  </span>
                )}
                {profile.lens_handle && (
                  <span className="rounded-full border border-border/60 bg-background/40 px-2 py-0.5">
                    @{profile.lens_handle}
                  </span>
                )}
              </div>

              {/* Bio */}
              {profile.bio && (
                <p className="max-w-xl text-sm text-muted-foreground">
                  {profile.bio}
                </p>
              )}

              {/* Follower count */}
              <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1">
                  <Users className="h-4 w-4" />
                  {followerCount.toLocaleString()} followers
                </span>
                {joinedAtLabel && (
                  <span className="inline-flex items-center gap-1">
                    <Calendar className="h-4 w-4" />
                    Joined {joinedAtLabel}
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* Follow Button */}
          {isSignedIn && (
            <Button
              onClick={toggle}
              disabled={isLoading}
              variant={isFollowing ? "outline" : "default"}
              className="min-w-[120px] text-xs font-mono uppercase tracking-[0.3em]"
            >
              {isLoading ? "..." : isFollowing ? "Following" : "Follow"}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
