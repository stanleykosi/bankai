"use client";

/**
 * @description
 * Bookmark Button component - star icon toggle for market header.
 * Shows for all users; prompts wallet connection when not authenticated.
 */

import { Star } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useBookmarkToggle } from "@/hooks/useWatchlist";
import { useWallet } from "@/hooks/useWallet";
import { cn } from "@/lib/utils";

interface BookmarkButtonProps {
  marketId: string;
  size?: "sm" | "default" | "lg" | "icon";
  variant?: "ghost" | "outline" | "default";
  className?: string;
}

export function BookmarkButton({
  marketId,
  size = "icon",
  variant = "ghost",
  className,
}: BookmarkButtonProps) {
  const { isAuthenticated, isLoading, hasSession } = useWallet();
  const { isBookmarked, isLoading: isBookmarking, toggle } = useBookmarkToggle(marketId);

  if (!hasSession) {
    return (
      <Button
        variant={variant}
        size={size}
        disabled={isLoading}
        className={cn("transition-colors text-muted-foreground hover:text-yellow-500", className)}
        title="Connect wallet to add to watchlist"
      >
        <Star className="h-5 w-5" />
      </Button>
    );
  }

  return (
    <Button
      onClick={() => {
        if (!isAuthenticated) {
          return;
        }
        void toggle();
      }}
      disabled={isLoading || isBookmarking || !isAuthenticated}
      variant={variant}
      size={size}
      className={cn(
        "transition-colors",
        isBookmarked && "text-yellow-500 hover:text-yellow-400",
        className
      )}
      title={
        !isAuthenticated
          ? "Restoring wallet session..."
          : isBookmarked
            ? "Remove from watchlist"
            : "Add to watchlist"
      }
    >
      <Star
        className={cn(
          "h-5 w-5 transition-all",
          isBookmarked && "fill-current"
        )}
      />
    </Button>
  );
}
