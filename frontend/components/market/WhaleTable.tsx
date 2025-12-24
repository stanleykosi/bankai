"use client";

/**
 * @description
 * Whale Table component - shows top holders for a market.
 * Lists the top 10 holders of each outcome (YES/NO) with links to profiles.
 */

import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Loader2, ExternalLink, Users } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { fetchMarketHolders } from "@/lib/market-data";
import type { Holder } from "@/types";

interface WhaleTableProps {
  conditionId: string;
  tokenYesId?: string;
  tokenNoId?: string;
  onExpand?: () => void;
  expandLabel?: string;
}

function formatCurrency(value: number): string {
  if (value >= 1_000_000) {
    return `$${(value / 1_000_000).toFixed(2)}M`;
  }
  if (value >= 1_000) {
    return `$${(value / 1_000).toFixed(1)}K`;
  }
  return `$${value.toFixed(2)}`;
}

function formatSize(size: number): string {
  if (size >= 1_000_000) {
    return `${(size / 1_000_000).toFixed(2)}M`;
  }
  if (size >= 1_000) {
    return `${(size / 1_000).toFixed(1)}K`;
  }
  return size.toFixed(2);
}

function formatPercent(value: number): string {
  return `${value.toFixed(2)}%`;
}

function truncateAddress(address: string): string {
  if (!address || address.length <= 12) return address;
  return `${address.slice(0, 6)}...${address.slice(-4)}`;
}

function HolderRow({ holder, rank }: { holder: Holder; rank: number }) {
  const displayName =
    holder.profileName || truncateAddress(holder.address);

  return (
    <tr className="hover:bg-muted/20 transition-colors">
      <td className="py-2.5 text-center font-mono text-muted-foreground">
        {rank}
      </td>
      <td className="py-2.5 pr-2">
        <Link
          href={`/profile/${holder.address}`}
          className="flex min-w-0 items-center gap-2 group"
        >
          {/* Avatar */}
          {holder.profileImage ? (
            <img
              src={holder.profileImage}
              alt={displayName}
              className="h-6 w-6 rounded-full"
            />
          ) : (
            <div className="h-6 w-6 rounded-full bg-primary/10 flex items-center justify-center">
              <span className="text-[10px] font-bold text-primary">
                {displayName.charAt(0).toUpperCase()}
              </span>
            </div>
          )}
          <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground group-hover:text-primary transition-colors">
            {displayName}
          </span>
          <ExternalLink className="h-3 w-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
        </Link>
      </td>
      <td className="py-2.5 text-right font-mono">
        {formatSize(holder.size)}
      </td>
      <td className="py-2.5 text-right font-mono">
        {formatCurrency(holder.value)}
      </td>
      <td className="py-2.5 text-right font-mono text-muted-foreground">
        {formatPercent(holder.percentage)}
      </td>
    </tr>
  );
}

function HoldersTable({
  holders,
  isLoading,
  outcome,
}: {
  holders: Holder[];
  isLoading: boolean;
  outcome: "YES" | "NO";
}) {
  const outcomeColor = outcome === "YES" ? "text-green-500" : "text-red-500";

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!holders || holders.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-muted-foreground">
        No holders found for {outcome}
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full table-fixed text-sm">
        <colgroup>
          <col className="w-10" />
          <col className="w-[44%]" />
          <col className="w-[18%]" />
          <col className="w-[18%]" />
          <col className="w-[10%]" />
        </colgroup>
        <thead>
          <tr className="border-b border-border/50 text-xs uppercase tracking-wide text-muted-foreground">
            <th className="w-12 pb-2 text-center">#</th>
            <th className="pb-2 text-left">Holder</th>
            <th className="pb-2 text-right">Size</th>
            <th className="pb-2 text-right">Value</th>
            <th className="pb-2 text-right">%</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/30">
          {holders.map((holder, index) => (
            <HolderRow key={holder.address} holder={holder} rank={index + 1} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function WhaleTable({
  conditionId,
  tokenYesId,
  tokenNoId,
  onExpand,
  expandLabel = "Full view",
}: WhaleTableProps) {
  const [activeOutcome, setActiveOutcome] = useState<"YES" | "NO">("YES");

  const activeTokenId = activeOutcome === "YES" ? tokenYesId : tokenNoId;

  const { data, isLoading } = useQuery({
    queryKey: ["market-holders", conditionId, activeTokenId],
    queryFn: () => fetchMarketHolders(conditionId, activeTokenId, 10),
    enabled: Boolean(conditionId),
    staleTime: 60_000,
  });

  return (
    <Card className="border-border/50 bg-card/60 backdrop-blur">
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base font-semibold">
            <Users className="h-4 w-4 text-primary" />
            Top Holders
          </CardTitle>
          <div className="flex items-center gap-2">
            {onExpand && (
              <Button
                variant="outline"
                size="sm"
                onClick={onExpand}
                className="text-xs"
              >
                {expandLabel}
              </Button>
            )}
            <div className="flex gap-1">
              <Button
                variant={activeOutcome === "YES" ? "default" : "outline"}
                size="sm"
                onClick={() => setActiveOutcome("YES")}
                className={`text-xs ${activeOutcome === "YES" ? "bg-green-500 hover:bg-green-600" : ""
                  }`}
              >
                YES
              </Button>
              <Button
                variant={activeOutcome === "NO" ? "default" : "outline"}
                size="sm"
                onClick={() => setActiveOutcome("NO")}
                className={`text-xs ${activeOutcome === "NO" ? "bg-red-500 hover:bg-red-600" : ""
                  }`}
              >
                NO
              </Button>
            </div>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <HoldersTable
          holders={data?.holders || []}
          isLoading={isLoading}
          outcome={activeOutcome}
        />
      </CardContent>
    </Card>
  );
}
