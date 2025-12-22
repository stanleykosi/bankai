"use client";

/**
 * @description
 * Bookmark Button component - star icon toggle for market header.
 */

import { Star } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useBookmarkToggle } from "@/hooks/useWatchlist";
import { useAuth } from "@clerk/nextjs";
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
  const { isSignedIn } = useAuth();
  const { isBookmarked, isLoading, toggle } = useBookmarkToggle(marketId);

  if (!isSignedIn) {
    return null;
  }

  return (
    <Button
      onClick={toggle}
      disabled={isLoading}
      variant={variant}
      size={size}
      className={cn(
        "transition-colors",
        isBookmarked && "text-yellow-500 hover:text-yellow-400",
        className
      )}
      title={isBookmarked ? "Remove from watchlist" : "Add to watchlist"}
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
