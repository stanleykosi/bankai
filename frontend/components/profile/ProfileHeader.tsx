"use client";

/**
 * @description
 * Profile Header component displaying trader identity info.
 * Shows avatar, name, verification badge, and follow button.
 */

import { useState } from "react";
import Image from "next/image";
import { CheckCircle2, Copy, Check, Users } from "lucide-react";
import { Button } from "@/components/ui/button";
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
    profile.profile_name ||
    profile.ens_name ||
    `${profile.address.slice(0, 6)}...${profile.address.slice(-4)}`;

  const copyAddress = async () => {
    await navigator.clipboard.writeText(profile.address);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="flex flex-col gap-6 md:flex-row md:items-start md:justify-between">
      <div className="flex items-start gap-4">
        {/* Avatar */}
        <div className="relative h-20 w-20 shrink-0 overflow-hidden rounded-full border-2 border-primary/30 bg-card">
          {profile.profile_image ? (
            <Image
              src={profile.profile_image}
              alt={displayName}
              fill
              className="object-cover"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-primary/20 to-primary/40 text-2xl font-bold text-primary">
              {displayName.charAt(0).toUpperCase()}
            </div>
          )}
        </div>

        {/* Identity Info */}
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-bold text-foreground">{displayName}</h1>
            {profile.is_verified && (
              <CheckCircle2 className="h-5 w-5 text-blue-500" />
            )}
          </div>

          {/* Address */}
          <button
            onClick={copyAddress}
            className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            <span>{`${profile.address.slice(0, 10)}...${profile.address.slice(-8)}`}</span>
            {copied ? (
              <Check className="h-3 w-3 text-green-500" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
          </button>

          {/* ENS / Lens Handle */}
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {profile.ens_name && (
              <span className="rounded-full bg-primary/10 px-2 py-0.5">
                {profile.ens_name}
              </span>
            )}
            {profile.lens_handle && (
              <span className="rounded-full bg-green-500/10 px-2 py-0.5 text-green-500">
                @{profile.lens_handle}
              </span>
            )}
          </div>

          {/* Bio */}
          {profile.bio && (
            <p className="mt-2 max-w-md text-sm text-muted-foreground">
              {profile.bio}
            </p>
          )}

          {/* Follower count */}
          <div className="mt-2 flex items-center gap-1 text-sm text-muted-foreground">
            <Users className="h-4 w-4" />
            <span>{followerCount.toLocaleString()} followers</span>
          </div>
        </div>
      </div>

      {/* Follow Button */}
      {isSignedIn && (
        <Button
          onClick={toggle}
          disabled={isLoading}
          variant={isFollowing ? "outline" : "default"}
          className="min-w-[100px]"
        >
          {isLoading ? "..." : isFollowing ? "Following" : "Follow"}
        </Button>
      )}
    </div>
  );
}
