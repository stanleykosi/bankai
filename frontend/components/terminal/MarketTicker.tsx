/**
 * @description
 * MarketTicker Component.
 * Displays a high-density feed of markets categorized by "Fresh Drops" and "High Velocity".
 * Uses CSS Grid for layout and Tailwind for "Cyber-Terminal" styling.
 *
 * @dependencies
 * - framer-motion: For entry animations
 * - lucide-react: For icons
 * - frontend/types: Market interface
 */

"use client";

import React from "react";
import { motion } from "framer-motion";
import { TrendingUp, Zap, Clock, ArrowUpRight } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Market } from "@/types";
import { useRouter } from "next/navigation";

// Temporary Badge component if Shadcn one isn't fully set up yet in previous steps, 
// but usually 'npx shadcn-ui@latest add badge' would add it. 
// I'll implement a simple inline one to be safe or assume standard class usage.
const SimpleBadge = ({ children, className }: { children: React.ReactNode; className?: string }) => (
  <span className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 ${className}`}>
    {children}
  </span>
);

interface MarketLaneProps {
  title: string;
  icon: React.ReactNode;
  markets: Market[];
  colorClass: string; // e.g., "text-constructive"
}

const MarketCard = ({ market }: { market: Market }) => {
  const router = useRouter();

  return (
    <motion.div
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      whileHover={{ scale: 1.02, backgroundColor: "hsl(var(--muted))" }}
      className="group cursor-pointer rounded-md border border-border bg-card/50 p-3 transition-all hover:border-primary"
      onClick={() => router.push(`/market/${market.slug}`)}
    >
      <div className="flex justify-between items-start mb-2">
        <h4 className="text-sm font-medium leading-tight line-clamp-2 text-foreground/90 group-hover:text-primary">
          {market.title}
        </h4>
        <ArrowUpRight className="h-4 w-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
      </div>
      
      <div className="flex items-center justify-between text-xs font-mono text-muted-foreground mt-2">
        <div className="flex items-center gap-2">
          {market.volume_24h > 1000 ? (
             <span className="flex items-center text-accent">
               <TrendingUp className="h-3 w-3 mr-1" />
               ${(market.volume_24h / 1000).toFixed(1)}k
             </span>
          ) : (
             <span>Vol: ${market.volume_24h.toFixed(0)}</span>
          )}
        </div>
        <div className="text-[10px] opacity-70">
          {new Date(market.created_at).toLocaleDateString()}
        </div>
      </div>
    </motion.div>
  );
};

const MarketLane = ({ title, icon, markets, colorClass }: MarketLaneProps) => {
  return (
    <div className="flex flex-col h-full overflow-hidden border-r border-border/50 last:border-r-0 bg-background/30 backdrop-blur-sm">
      <div className={`flex items-center gap-2 p-4 border-b border-border/50 font-mono uppercase tracking-wider text-sm ${colorClass}`}>
        {icon}
        <h3>{title}</h3>
        <SimpleBadge className="ml-auto border-border bg-background text-foreground/70">
          {markets.length}
        </SimpleBadge>
      </div>
      <div className="flex-1 overflow-y-auto p-3 space-y-3 custom-scrollbar">
        {markets.map((market) => (
          <MarketCard key={market.condition_id} market={market} />
        ))}
        {markets.length === 0 && (
          <div className="text-center text-muted-foreground text-xs py-10">
            No active markets found in this lane.
          </div>
        )}
      </div>
    </div>
  );
};

interface MarketTickerProps {
  freshDrops: Market[];
  activeMarkets: Market[];
}

export const MarketTicker = ({ freshDrops, activeMarkets }: MarketTickerProps) => {
  // We can split activeMarkets into "High Velocity" and "Contested" based on logic
  // For now, we'll just use the raw lists passed down
  
  // Sort active by volume for High Velocity
  const highVelocity = [...activeMarkets].sort((a, b) => b.volume_24h - a.volume_24h).slice(0, 20);
  
  // Arbitrary split for "Contested" or "Trending" - using remaining active for now
  // In a real app, "Contested" implies close spread or high flip rate.
  const contested = [...activeMarkets].sort((a, b) => b.liquidity - a.liquidity).slice(0, 20);

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 h-[600px] border border-border rounded-lg overflow-hidden shadow-2xl shadow-black/50">
      <MarketLane 
        title="Fresh Drops" 
        icon={<Clock className="h-4 w-4" />} 
        markets={freshDrops}
        colorClass="text-blue-400"
      />
      <MarketLane 
        title="High Velocity" 
        icon={<Zap className="h-4 w-4" />} 
        markets={highVelocity}
        colorClass="text-yellow-400"
      />
      <MarketLane 
        title="Deep Liquidity" 
        icon={<TrendingUp className="h-4 w-4" />} 
        markets={contested}
        colorClass="text-constructive"
      />
    </div>
  );
};

