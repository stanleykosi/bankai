/**
 * @description
 * Collapsible sidebar shell for the AI Oracle.
 */

"use client";

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Bot, ChevronLeft, X } from "lucide-react";

import { useTerminalStore } from "@/lib/store";
import { Button } from "@/components/ui/button";
import { ChatInterface } from "./ChatInterface";

export function OracleSidebar() {
  const { isOracleOpen, setOracleOpen, activeMarket } = useTerminalStore();

  return (
    <>
      <AnimatePresence>
        {!isOracleOpen && (
          <motion.div
            initial={{ opacity: 0, x: 20 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 20 }}
            className="fixed right-0 top-1/2 z-40 -translate-y-1/2"
          >
            <Button
              onClick={() => setOracleOpen(true)}
              variant="outline"
              size="sm"
              className="h-24 w-8 rounded-l-md rounded-r-none border-r-0 border-primary/50 bg-background/80 p-0 shadow-[0_0_15px_rgba(0,0,0,0.5)] backdrop-blur-md hover:bg-primary/10 hover:text-primary"
            >
              <div className="flex flex-col items-center justify-center gap-2">
                <ChevronLeft className="h-4 w-4" />
                <span className="rotate-[270deg] whitespace-nowrap font-mono text-[10px] font-bold uppercase tracking-widest">
                  Oracle
                </span>
              </div>
            </Button>
          </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {isOracleOpen && (
          <motion.div
            initial={{ width: 0, opacity: 0 }}
            animate={{ width: 350, opacity: 1 }}
            exit={{ width: 0, opacity: 0 }}
            transition={{ type: "spring", stiffness: 300, damping: 30 }}
            className="fixed inset-y-0 right-0 z-50 flex h-screen flex-col border-l border-border bg-card/95 backdrop-blur-xl shadow-2xl"
          >
            <div className="flex h-14 items-center justify-between border-b border-border bg-background/50 px-4">
              <div className="flex items-center gap-2">
                <Bot className="h-5 w-5 text-primary" />
                <span className="font-mono text-sm font-bold tracking-wider text-foreground">
                  BANKAI_ORACLE
                </span>
                <span className="flex h-2 w-2">
                  <span className="absolute inline-flex h-2 w-2 animate-ping rounded-full bg-primary opacity-75" />
                  <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
                </span>
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setOracleOpen(false)}
                className="h-8 w-8 text-muted-foreground hover:text-foreground"
              >
                <X className="h-4 w-4" />
              </Button>
            </div>

            <div className="relative flex-1 overflow-hidden">
              <ChatInterface />
              {!activeMarket && (
                <div className="pointer-events-none absolute inset-0 bg-background/50 backdrop-blur-[1px]" />
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

