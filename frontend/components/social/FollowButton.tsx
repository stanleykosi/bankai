"use client";

/**
 * @description
 * Follow Button component with loading and toggle states.
 * Shows for all users; prompts wallet connection when not authenticated.
 */

import { Button } from "@/components/ui/button";
import { useFollowToggle } from "@/hooks/useFollow";
import { useWallet } from "@/hooks/useWallet";
import { UserPlus, UserCheck, Loader2 } from "lucide-react";

interface FollowButtonProps {
  targetAddress: string;
  size?: "sm" | "default" | "lg";
  showIcon?: boolean;
  className?: string;
}

export function FollowButton({
  targetAddress,
  size = "default",
  showIcon = true,
  className,
}: FollowButtonProps) {
  const { isAuthenticated, isLoading: isAuthLoading } = useWallet();
  const { isFollowing, isLoading, toggle } = useFollowToggle(targetAddress);

  if (!isAuthenticated) {
    return (
      <Button size={size} className={className} disabled={isAuthLoading}>
        {showIcon && <UserPlus className="h-4 w-4" />}
        <span className={showIcon ? "ml-2" : ""}>Connect wallet</span>
      </Button>
    );
  }

  return (
    <Button
      onClick={toggle}
      disabled={isLoading || isAuthLoading}
      variant={isFollowing ? "outline" : "default"}
      size={size}
      className={className}
    >
      {isLoading ? (
        <Loader2 className="h-4 w-4 animate-spin" />
      ) : showIcon ? (
        isFollowing ? (
          <UserCheck className="h-4 w-4" />
        ) : (
          <UserPlus className="h-4 w-4" />
        )
      ) : null}
      <span className={showIcon ? "ml-2" : ""}>
        {isFollowing ? "Following" : "Follow"}
      </span>
    </Button>
  );
}
