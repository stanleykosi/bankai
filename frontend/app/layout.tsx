/**
 * @description
 * Root Layout component for the Next.js application.
 * Wraps the entire application in:
 * 1. ClerkProvider (Authentication)
 * 2. QueryClientProvider (React Query for data fetching)
 * 3. WagmiProvider (Web3 wallet connections)
 * 4. Global Fonts (Inter/JetBrains Mono)
 * 
 * @dependencies
 * - @clerk/nextjs: Auth context
 * - @tanstack/react-query: Data fetching
 * - wagmi: Web3 wallet integration
 * - next/font/google: Typography
 */

import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import { ClerkProvider } from "@clerk/nextjs";
import { Providers } from "./providers";
import "./globals.css";

// UI Font
const inter = Inter({ 
  subsets: ["latin"], 
  variable: "--font-inter" 
});

// Data/Terminal Font
const jetbrainsMono = JetBrains_Mono({ 
  subsets: ["latin"], 
  variable: "--font-mono" 
});

export const metadata: Metadata = {
  title: "Bankai | PolyTerminal",
  description: "High-performance Polymarket Trading Terminal",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <ClerkProvider>
      <html lang="en" className="dark">
        <body className={`${inter.variable} ${jetbrainsMono.variable} font-sans bg-background text-foreground antialiased`}>
          <Providers>
            <main className="min-h-screen w-full bg-black">
              {children}
            </main>
          </Providers>
        </body>
      </html>
    </ClerkProvider>
  );
}

