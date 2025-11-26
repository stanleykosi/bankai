/**
 * @description
 * Terminal Layout.
 * Provides the main structure for the trading interface, including Navigation.
 */

"use client";

import React from "react";
import { Header } from "@/components/layout/Header";

// Force dynamic rendering to ensure auth state is fresh
export const dynamic = "force-dynamic";

export default function TerminalLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen bg-background flex flex-col">
      <Header />
      <main className="flex-1 relative">
        {children}
      </main>
    </div>
  );
}
