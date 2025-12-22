"use client";

/**
 * @description
 * Follow Button component with loading and toggle states.
 * Shows for all users; prompts sign-in when clicked if not authenticated.
 */

import { Button } from "@/components/ui/button";
import { useFollowToggle } from "@/hooks/useFollow";
import { useAuth, SignInButton } from "@clerk/nextjs";
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
  const { isSignedIn } = useAuth();
  const { isFollowing, isLoading, toggle } = useFollowToggle(targetAddress);

  // If not signed in, show the button but trigger sign-in modal on click
  if (!isSignedIn) {
    return (
      <SignInButton mode="modal">
        <Button size={size} className={className}>
          {showIcon && <UserPlus className="h-4 w-4" />}
          <span className={showIcon ? "ml-2" : ""}>Follow</span>
        </Button>
      </SignInButton>
    );
  }

  return (
    <Button
      onClick={toggle}
      disabled={isLoading}
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
