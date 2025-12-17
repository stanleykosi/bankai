/**
 * @description
 * Terminal Layout.
 * Provides the main structure for the trading interface, including Navigation and the Oracle Sidebar.
 */

"use client";

import React from "react";
import { Header } from "@/components/layout/Header";
import { OracleSidebar } from "@/components/oracle/OracleSidebar";
import { useTerminalStore } from "@/lib/store";
import { cn } from "@/lib/utils";

// Force dynamic rendering to ensure auth state is fresh
export const dynamic = "force-dynamic";

export default function TerminalLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { isOracleOpen } = useTerminalStore();

  return (
    <div className="flex min-h-screen flex-col overflow-x-hidden bg-background">
      <Header />
      <div className="relative flex flex-1">
        <main
          className={cn(
            "flex-1 transition-all duration-300 ease-in-out",
            isOracleOpen ? "mr-[350px]" : "mr-0",
          )}
        >
          {children}
        </main>
        <OracleSidebar />
      </div>
    </div>
  );
}
