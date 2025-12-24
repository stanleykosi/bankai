"use client";

/**
 * @description
 * Watchlist Sidebar component showing starred markets with live prices.
 */

import Link from "next/link";
import { Star, TrendingUp, TrendingDown, Loader2 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useWatchlist } from "@/hooks/useWatchlist";
import { useAuth } from "@clerk/nextjs";
import type { WatchlistItem } from "@/types";

function formatPrice(price: number): string {
  return `${(price * 100).toFixed(0)}¢`;
}

function formatChange(change: number): string {
  const sign = change >= 0 ? "+" : "";
  return `${sign}${(change * 100).toFixed(1)}%`;
}

function WatchlistMarketItem({ item }: { item: WatchlistItem }) {
  const isPriceUp = item.one_day_change >= 0;
  const marketHref = `/market/${item.slug || item.market_id}`;

  return (
    <Link
      href={marketHref}
      className="flex items-start gap-3 p-3 rounded-lg hover:bg-muted/50 transition-colors group"
    >
      {/* Image */}
      <div className="h-10 w-10 shrink-0 overflow-hidden rounded-md bg-muted">
        {item.image_url ? (
          <img
            src={item.image_url}
            alt={item.title}
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-primary/10">
            <Star className="h-4 w-4 text-primary" />
          </div>
        )}
      </div>

      {/* Info */}
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-foreground truncate group-hover:text-primary transition-colors">
          {item.title}
        </p>
        <div className="flex items-center gap-2 mt-1 text-xs text-muted-foreground">
          <span className="text-green-500 font-mono">
            YES {formatPrice(item.yes_price)}
          </span>
          <span className="text-muted-foreground/50">|</span>
          <span className="text-red-500 font-mono">
            NO {formatPrice(item.no_price)}
          </span>
        </div>
      </div>

      {/* Change */}
      <div
        className={`flex items-center gap-1 text-xs font-mono ${isPriceUp ? "text-green-500" : "text-red-500"
          }`}
      >
        {isPriceUp ? (
          <TrendingUp className="h-3 w-3" />
        ) : (
          <TrendingDown className="h-3 w-3" />
        )}
        {formatChange(item.one_day_change)}
      </div>
    </Link>
  );
}

export function WatchlistSidebar() {
  const { isSignedIn } = useAuth();
  const { data, isLoading } = useWatchlist();

  if (!isSignedIn) {
    return null;
  }

  return (
    <Card className="border-border/50 bg-card/60 backdrop-blur">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-base font-semibold">
          <Star className="h-4 w-4 text-yellow-500 fill-yellow-500" />
          Watchlist
          {data && data.count > 0 && (
            <span className="text-xs font-normal text-muted-foreground">
              ({data.count})
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-2">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : !data || data.watchlist.length === 0 ? (
          <div className="py-8 text-center">
            <Star className="mx-auto h-8 w-8 text-muted-foreground/30" />
            <p className="mt-2 text-sm text-muted-foreground">
              No starred markets yet
            </p>
            <p className="mt-1 text-xs text-muted-foreground/70">
              Click the star icon on any market to add it here
            </p>
          </div>
        ) : (
          <div className="space-y-1 max-h-[400px] overflow-y-auto">
            {data.watchlist.map((item) => (
              <WatchlistMarketItem key={item.id} item={item} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
