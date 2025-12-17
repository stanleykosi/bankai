/**
 * @description
 * Oracle Sidebar content. Fetches and renders AI analysis for the active market.
 */

"use client";

import React, { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  BrainCircuit,
  ExternalLink,
  FileText,
  RefreshCcw,
} from "lucide-react";

import { useTerminalStore } from "@/lib/store";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { Card, CardContent } from "@/components/ui/card";

interface Source {
  title: string;
  url: string;
}

interface MarketAnalysis {
  market_id: string;
  question: string;
  probability: number;
  sentiment: string;
  reasoning: string;
  sources: Source[];
  last_updated: string;
}

export function ChatInterface() {
  const { activeMarket } = useTerminalStore();
  const [requestTime, setRequestTime] = useState(0);

  const marketId = activeMarket?.condition_id;

  const {
    data: analysis,
    isLoading,
    isError,
    error,
    refetch,
    isFetching,
  } = useQuery<MarketAnalysis>({
    queryKey: ["oracle-analysis", marketId, requestTime],
    queryFn: async () => {
      if (!marketId) throw new Error("No market selected");
      const { data } = await api.get<MarketAnalysis>(`/oracle/analyze/${marketId}`);
      return data;
    },
    enabled: Boolean(marketId),
    staleTime: 1000 * 60 * 5,
    retry: false,
  });

  if (!activeMarket) {
    return (
      <div className="flex h-full flex-col items-center justify-center p-6 text-center text-muted-foreground">
        <BrainCircuit className="mb-4 h-12 w-12 opacity-20" />
        <p className="font-mono text-sm">Select a market to enable Neural Analysis.</p>
      </div>
    );
  }

  const handleAnalyze = () => {
    setRequestTime(Date.now());
  };

  const probability = typeof analysis?.probability === "number" ? analysis.probability : 0;
  const sources = analysis?.sources ?? [];

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="border-b border-border/50 bg-muted/20 p-4">
        <h3 className="font-mono text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          Target
        </h3>
        <p className="mt-1 line-clamp-2 text-sm font-medium leading-snug text-foreground">
          {activeMarket.title}
        </p>
      </div>

      <div className="flex-1 overflow-y-auto p-4 custom-scrollbar">
        {!analysis && !isLoading && !isError && (
          <div className="flex flex-col items-center justify-center space-y-4 py-12 text-center">
            <div className="rounded-full bg-primary/10 p-3">
              <BrainCircuit className="h-8 w-8 text-primary" />
            </div>
            <div className="space-y-1">
              <h4 className="font-mono text-sm font-bold text-foreground">Awaiting Input</h4>
              <p className="text-xs text-muted-foreground">
                Run a live RAG analysis on this market using <br /> Tavily Search + LLM reasoning.
              </p>
            </div>
            <Button onClick={handleAnalyze} size="sm" className="font-mono font-bold tracking-wide">
              Initialize Oracle
            </Button>
          </div>
        )}

        {(isLoading || isFetching) && (
          <div className="flex flex-col items-center justify-center space-y-4 py-12 animate-pulse">
            <BrainCircuit className="h-8 w-8 text-primary" />
            <p className="font-mono text-xs text-primary">Synthesizing Probability...</p>
            <div className="w-3/4 space-y-2">
              <div className="h-2 rounded bg-muted" />
              <div className="h-2 w-5/6 rounded bg-muted" />
              <div className="h-2 w-4/6 rounded bg-muted" />
            </div>
          </div>
        )}

        {isError && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-center">
            <AlertTriangle className="mx-auto mb-2 h-6 w-6 text-destructive" />
            <p className="font-mono text-xs text-destructive">Analysis Failed</p>
            <p className="mt-1 text-[10px] text-muted-foreground">
              {error instanceof Error ? error.message : "Unknown error occurred"}
            </p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setRequestTime(Date.now());
                void refetch();
              }}
              className="mt-4 border-destructive/30 hover:bg-destructive/20"
            >
              Retry
            </Button>
          </div>
        )}

        {analysis && !isFetching && (
          <div className="space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                  Estimated Probability (YES)
                </span>
                <span
                  className={cn(
                    "font-mono text-xs font-bold uppercase",
                    getSentimentColor(analysis.sentiment),
                  )}
                >
                  {analysis.sentiment || "—"}
                </span>
              </div>
              <div className="relative h-12 w-full overflow-hidden rounded-md border border-border/50 bg-muted/30">
                <div
                  className="absolute inset-0 bg-primary/20"
                  style={{ width: `${Math.min(Math.max(probability, 0), 1) * 100}%` }}
                />
                <div className="absolute inset-0 flex items-center justify-center">
                  <span className="font-mono text-xl font-bold tracking-tight text-foreground drop-shadow-md">
                    {(probability * 100).toFixed(1)}%
                  </span>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <h4 className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                <FileText className="h-3 w-3" />
                Reasoning
              </h4>
              <Card className="border-border/50 bg-card/50">
                <CardContent className="p-3 text-xs leading-relaxed text-foreground/90 font-sans">
                  {analysis.reasoning || "No reasoning provided."}
                </CardContent>
              </Card>
            </div>

            {sources.length > 0 && (
              <div className="space-y-2">
                <h4 className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                  <ExternalLink className="h-3 w-3" />
                  Citations
                </h4>
                <div className="grid gap-2">
                  {sources.map((source, idx) => (
                    <a
                      key={`${source.url}-${idx}`}
                      href={source.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="group flex items-start gap-2 rounded-md border border-border/40 bg-background/50 p-2 text-[10px] transition-colors hover:border-primary/40 hover:bg-muted/50"
                    >
                      <div className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm bg-primary/10 text-primary font-mono text-[9px]">
                        {idx + 1}
                      </div>
                      <span className="line-clamp-2 flex-1 text-muted-foreground group-hover:text-foreground">
                        {source.title}
                      </span>
                    </a>
                  ))}
                </div>
              </div>
            )}

            <div className="border-t border-border/30 pt-4 text-center">
              <p className="font-mono text-[9px] text-muted-foreground">
                Last updated:{" "}
                {analysis.last_updated
                  ? new Date(analysis.last_updated).toLocaleTimeString()
                  : "—"}
              </p>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleAnalyze}
                className="mt-2 h-7 text-[10px] text-muted-foreground hover:text-foreground"
              >
                <RefreshCcw className="mr-1.5 h-3 w-3" />
                Regenerate Analysis
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function getSentimentColor(sentiment?: string) {
  const s = (sentiment || "").toLowerCase();
  if (s.includes("bullish")) return "text-constructive";
  if (s.includes("bearish")) return "text-destructive";
  if (s.includes("neutral")) return "text-yellow-500";
  return "text-muted-foreground";
}
