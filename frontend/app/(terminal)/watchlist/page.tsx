"use client";

import * as React from "react";
import Link from "next/link";
import { SignInButton, useAuth } from "@clerk/nextjs";
import {
  ArrowUpRight,
  Loader2,
  Search,
  SlidersHorizontal,
  Star,
  TrendingDown,
  TrendingUp,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { BookmarkButton } from "@/components/watchlist/BookmarkButton";
import { useWatchlist } from "@/hooks/useWatchlist";
import { cn } from "@/lib/utils";
import type { WatchlistItem } from "@/types";

type SortKey = "recent" | "change" | "volume" | "yes" | "no";
type FilterKey = "all" | "gainers" | "losers";

const sortOptions: Array<{ value: SortKey; label: string }> = [
  { value: "recent", label: "Recently added" },
  { value: "change", label: "Biggest move" },
  { value: "volume", label: "Highest volume" },
  { value: "yes", label: "Highest YES" },
  { value: "no", label: "Highest NO" },
];

const filterOptions: Array<{ value: FilterKey; label: string }> = [
  { value: "all", label: "All" },
  { value: "gainers", label: "Gainers" },
  { value: "losers", label: "Losers" },
];

const formatPrice = (price: number) => `${(price * 100).toFixed(1)}¢`;

const formatChange = (change: number) => {
  const sign = change >= 0 ? "+" : "";
  return `${sign}${(change * 100).toFixed(1)}%`;
};

const formatVolume = (value?: number) => {
  if (typeof value !== "number" || Number.isNaN(value)) return "—";
  if (value >= 1_000_000_000) return `$${(value / 1_000_000_000).toFixed(2)}B`;
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(2)}K`;
  return `$${value.toFixed(2)}`;
};

const formatDate = (value?: string) => {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
};

function WatchlistCard({ item }: { item: WatchlistItem }) {
  const isUp = item.one_day_change >= 0;
  const marketHref = `/market/${item.slug || item.market_id}`;

  return (
    <Card className="group h-full w-full border-border/60 bg-card/70 transition hover:border-primary/40 hover:shadow-xl hover:shadow-black/20">
      <CardContent className="flex h-full flex-col gap-4 p-4">
        <div className="flex items-start gap-3">
          <div className="h-12 w-12 shrink-0 overflow-hidden rounded-md border border-border/50 bg-background/40">
            {item.image_url ? (
              <img
                src={item.image_url}
                alt={item.title}
                className="h-full w-full object-cover"
                loading="lazy"
              />
            ) : (
              <div className="flex h-full w-full items-center justify-center bg-primary/10">
                <Star className="h-4 w-4 text-primary" />
              </div>
            )}
          </div>

          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 space-y-1 pr-2">
                <Link
                  href={marketHref}
                  className="block line-clamp-2 text-sm font-semibold leading-snug text-foreground transition-colors group-hover:text-primary"
                >
                  {item.title}
                </Link>
                <p className="text-xs text-muted-foreground truncate">
                  Added {formatDate(item.created_at)}
                </p>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <span
                  className={cn(
                    "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-mono",
                    isUp
                      ? "border-emerald-400/40 bg-emerald-400/10 text-emerald-400"
                      : "border-rose-400/40 bg-rose-400/10 text-rose-400"
                  )}
                >
                  {isUp ? (
                    <TrendingUp className="h-3 w-3" />
                  ) : (
                    <TrendingDown className="h-3 w-3" />
                  )}
                  {formatChange(item.one_day_change)}
                </span>
                <BookmarkButton
                  marketId={item.market_id}
                  size="icon"
                  variant="ghost"
                  className="h-8 w-8 text-muted-foreground hover:text-yellow-400"
                />
              </div>
            </div>
          </div>
        </div>

        <div className="grid gap-2 sm:grid-cols-3">
          <div className="rounded-md border border-border/40 bg-background/40 px-3 py-2">
            <p className="text-[10px] uppercase tracking-wide text-muted-foreground">Yes</p>
            <p className="font-mono text-sm text-emerald-400">
              {formatPrice(item.yes_price)}
            </p>
          </div>
          <div className="rounded-md border border-border/40 bg-background/40 px-3 py-2">
            <p className="text-[10px] uppercase tracking-wide text-muted-foreground">No</p>
            <p className="font-mono text-sm text-rose-400">
              {formatPrice(item.no_price)}
            </p>
          </div>
          <div className="rounded-md border border-border/40 bg-background/40 px-3 py-2">
            <p className="text-[10px] uppercase tracking-wide text-muted-foreground">24h Volume</p>
            <p className="font-mono text-sm text-foreground">
              {formatVolume(item.volume_24h)}
            </p>
          </div>
        </div>

        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span className="font-mono">Market activity synced live</span>
          <Button asChild size="sm" variant="ghost" className="h-8 px-2 text-xs">
            <Link href={marketHref} className="flex items-center gap-1">
              View market <ArrowUpRight className="h-3 w-3" />
            </Link>
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

export default function WatchlistPage() {
  const { isSignedIn, isLoaded } = useAuth();
  const { data, isLoading } = useWatchlist();
  const [search, setSearch] = React.useState("");
  const [sortBy, setSortBy] = React.useState<SortKey>("recent");
  const [filterBy, setFilterBy] = React.useState<FilterKey>("all");

  const watchlist = data?.watchlist ?? [];
  const watchlistCount = data?.count ?? watchlist.length;

  const totalVolume = React.useMemo(
    () => watchlist.reduce((sum, item) => sum + (item.volume_24h || 0), 0),
    [watchlist]
  );

  const movers = React.useMemo(
    () =>
      [...watchlist]
        .sort((a, b) => Math.abs(b.one_day_change) - Math.abs(a.one_day_change))
        .slice(0, 3),
    [watchlist]
  );

  const highVolume = React.useMemo(
    () => [...watchlist].sort((a, b) => b.volume_24h - a.volume_24h).slice(0, 3),
    [watchlist]
  );

  const visibleItems = React.useMemo(() => {
    const query = search.trim().toLowerCase();
    const filtered = watchlist.filter((item) => {
      if (filterBy === "gainers" && item.one_day_change < 0) return false;
      if (filterBy === "losers" && item.one_day_change >= 0) return false;
      if (!query) return true;
      return item.title.toLowerCase().includes(query);
    });

    const sorted = [...filtered].sort((a, b) => {
      switch (sortBy) {
        case "change":
          return Math.abs(b.one_day_change) - Math.abs(a.one_day_change);
        case "volume":
          return b.volume_24h - a.volume_24h;
        case "yes":
          return b.yes_price - a.yes_price;
        case "no":
          return b.no_price - a.no_price;
        case "recent":
        default:
          return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
      }
    });

    return sorted;
  }, [filterBy, search, sortBy, watchlist]);

  if (!isLoaded) {
    return (
      <div className="container max-w-[1200px] py-10">
        <Card className="border-border/60 bg-card/60">
          <CardContent className="flex min-h-[220px] items-center justify-center">
            <Loader2 className="h-6 w-6 animate-spin text-primary" />
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!isSignedIn) {
    return (
      <div className="container max-w-[1200px] py-10">
        <Card className="border-border/60 bg-card/60">
          <CardContent className="p-10 text-center">
            <div className="mx-auto flex max-w-md flex-col items-center gap-4">
              <div className="flex h-12 w-12 items-center justify-center rounded-full border border-border bg-primary/10">
                <Star className="h-6 w-6 text-primary" />
              </div>
              <h1 className="text-2xl font-semibold text-foreground">Your Watchlist</h1>
              <p className="text-sm text-muted-foreground">
                Sign in to curate markets you want to track in one focused, professional workspace.
              </p>
              <SignInButton mode="modal">
                <Button className="font-mono text-xs">Sign in to continue</Button>
              </SignInButton>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container max-w-[1600px] py-6 space-y-6">
      <section className="relative overflow-hidden rounded-2xl border border-border/60 bg-gradient-to-br from-background via-background to-card/60 px-6 py-6">
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_top,_rgba(56,189,248,0.15),_transparent_55%)]" />
        <div className="absolute -right-16 top-6 h-48 w-48 rounded-full bg-primary/10 blur-3xl" />
        <div className="relative flex flex-col gap-6 lg:flex-row lg:items-center lg:justify-between">
          <div className="space-y-3">
            <div className="flex items-center gap-2 text-xs font-mono uppercase tracking-widest text-muted-foreground">
              <Star className="h-3.5 w-3.5 text-yellow-400" />
              Watchlist Control Room
            </div>
            <h1 className="text-3xl font-semibold tracking-tight text-foreground">
              Your Market Watchlist
            </h1>
            <p className="max-w-2xl text-sm text-muted-foreground">
              Track the markets that matter most, keep an eye on momentum shifts, and jump into
              action without leaving your flow.
            </p>
            <div className="flex flex-wrap gap-2 text-xs font-mono">
              <span className="rounded-full border border-border/60 bg-background/50 px-3 py-1 text-muted-foreground">
                {watchlistCount} markets
              </span>
              <span className="rounded-full border border-border/60 bg-background/50 px-3 py-1 text-muted-foreground">
                {formatVolume(totalVolume)} 24h volume
              </span>
              <span className="rounded-full border border-border/60 bg-background/50 px-3 py-1 text-muted-foreground">
                {movers.length} active movers
              </span>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button asChild variant="outline" size="sm" className="font-mono text-xs">
              <Link href="/dashboard">Back to Radar</Link>
            </Button>
            <Button asChild size="sm" className="font-mono text-xs">
              <Link href="/markets">Browse markets</Link>
            </Button>
          </div>
        </div>
      </section>

      <Card className="border-border/60 bg-card/60">
        <CardContent className="p-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
            <div className="relative w-full lg:max-w-sm">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="Search watchlist markets..."
                className="pl-9"
              />
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {filterOptions.map((filter) => (
                <Button
                  key={filter.value}
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => setFilterBy(filter.value)}
                  className={cn(
                    "h-8 px-3 text-xs font-mono",
                    filterBy === filter.value
                      ? "bg-primary/10 text-primary"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  {filter.label}
                </Button>
              ))}
            </div>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <SlidersHorizontal className="h-4 w-4" />
              <select
                value={sortBy}
                onChange={(event) => setSortBy(event.target.value as SortKey)}
                className="h-9 rounded-md border border-input bg-background px-3 text-xs font-mono text-foreground"
              >
                {sortOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-6 lg:grid-cols-[1.7fr,1fr]">
        <div className="space-y-4">
          {isLoading ? (
            <Card className="border-border/60 bg-card/60">
              <CardContent className="flex min-h-[240px] items-center justify-center">
                <Loader2 className="h-6 w-6 animate-spin text-primary" />
              </CardContent>
            </Card>
          ) : visibleItems.length === 0 ? (
            <Card className="border-border/60 bg-card/60">
              <CardContent className="flex min-h-[240px] flex-col items-center justify-center gap-3 text-center">
                <Star className="h-8 w-8 text-muted-foreground/40" />
                <p className="text-sm text-muted-foreground">
                  No markets match your watchlist filters.
                </p>
                <Button asChild size="sm" variant="outline" className="font-mono text-xs">
                  <Link href="/markets">Discover markets</Link>
                </Button>
              </CardContent>
            </Card>
          ) : (
            <div className="grid auto-rows-fr gap-4 md:grid-cols-2">
              {visibleItems.map((item) => (
                <WatchlistCard key={item.id} item={item} />
              ))}
            </div>
          )}
        </div>

        <div className="space-y-4">
          <Card className="border-border/60 bg-card/60">
            <CardHeader className="pb-2">
              <CardTitle className="text-lg">Momentum Focus</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              {movers.length === 0 ? (
                <p className="text-sm text-muted-foreground">Add markets to see top movers.</p>
              ) : (
                movers.map((item) => {
                  const isUp = item.one_day_change >= 0;
                  const marketHref = `/market/${item.slug || item.market_id}`;
                  return (
                    <div
                      key={item.id}
                      className="flex items-center justify-between gap-3 rounded-md border border-border/50 bg-background/40 px-3 py-2"
                    >
                      <Link
                        href={marketHref}
                        className="truncate text-sm font-medium text-foreground hover:text-primary"
                      >
                        {item.title}
                      </Link>
                      <span
                        className={cn(
                          "text-xs font-mono",
                          isUp ? "text-emerald-400" : "text-rose-400"
                        )}
                      >
                        {formatChange(item.one_day_change)}
                      </span>
                    </div>
                  );
                })
              )}
            </CardContent>
          </Card>

          <Card className="border-border/60 bg-card/60">
            <CardHeader className="pb-2">
              <CardTitle className="text-lg">High Volume</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              {highVolume.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Your watchlist volume highlights will appear here.
                </p>
              ) : (
                highVolume.map((item) => (
                  <div
                    key={item.id}
                    className="flex items-center justify-between gap-3 rounded-md border border-border/50 bg-background/40 px-3 py-2"
                  >
                    <Link
                      href={`/market/${item.slug || item.market_id}`}
                      className="truncate text-sm font-medium text-foreground hover:text-primary"
                    >
                      {item.title}
                    </Link>
                    <span className="text-xs font-mono text-muted-foreground">
                      {formatVolume(item.volume_24h)}
                    </span>
                  </div>
                ))
              )}
            </CardContent>
          </Card>

          <Card className="border-border/60 bg-card/60">
            <CardHeader className="pb-2">
              <CardTitle className="text-lg">Watchlist Summary</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <div className="flex items-center justify-between">
                <span>Markets tracked</span>
                <span className="font-mono text-foreground">{watchlistCount}</span>
              </div>
              <div className="flex items-center justify-between">
                <span>24h volume tracked</span>
                <span className="font-mono text-foreground">{formatVolume(totalVolume)}</span>
              </div>
              <div className="flex items-center justify-between">
                <span>Sort mode</span>
                <span className="font-mono text-foreground">
                  {sortOptions.find((option) => option.value === sortBy)?.label ?? "—"}
                </span>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
