/**
 * @description
 * Terminal Layout.
 * Provides the main structure for the trading interface, including Navigation.
 */

import React from "react";
import Link from "next/link";
import { UserButton } from "@clerk/nextjs";
import { Activity, BarChart2, LayoutDashboard, Wallet } from "lucide-react";

export default function TerminalLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Top Navigation Bar */}
      <header className="sticky top-0 z-50 w-full border-b border-border bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
        <div className="container flex h-14 max-w-[1600px] items-center">
          <div className="mr-4 flex">
            <Link className="mr-6 flex items-center space-x-2" href="/dashboard">
              <Activity className="h-6 w-6 text-primary" />
              <span className="hidden font-bold sm:inline-block font-mono tracking-tighter">
                BANKAI<span className="text-primary">.TERMINAL</span>
              </span>
            </Link>
            <nav className="flex items-center space-x-6 text-sm font-medium">
              <Link
                href="/dashboard"
                className="transition-colors hover:text-foreground/80 text-foreground/60 flex items-center gap-2"
              >
                <LayoutDashboard className="h-4 w-4" />
                Radar
              </Link>
              <Link
                href="/portfolio"
                className="transition-colors hover:text-foreground/80 text-foreground/60 flex items-center gap-2"
              >
                <Wallet className="h-4 w-4" />
                Portfolio
              </Link>
              <Link
                href="/analysis"
                className="transition-colors hover:text-foreground/80 text-foreground/60 flex items-center gap-2"
              >
                <BarChart2 className="h-4 w-4" />
                Analysis
              </Link>
            </nav>
          </div>
          <div className="flex flex-1 items-center justify-end space-x-2">
            <UserButton 
              afterSignOutUrl="/"
              appearance={{
                elements: {
                  avatarBox: "h-8 w-8"
                }
              }}
            />
          </div>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="flex-1">
        {children}
      </main>
    </div>
  );
}

