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

// UI Font - with explicit weights to ensure proper loading
const inter = Inter({ 
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-inter",
  display: "swap",
  fallback: ["system-ui", "-apple-system", "BlinkMacSystemFont", "Segoe UI", "Roboto", "Arial", "sans-serif"],
  adjustFontFallback: true,
});

// Data/Terminal Font - with explicit weights
const jetbrainsMono = JetBrains_Mono({ 
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-mono",
  display: "swap",
  fallback: ["ui-monospace", "SFMono-Regular", "Menlo", "Monaco", "Consolas", "Liberation Mono", "Courier New", "monospace"],
  adjustFontFallback: true,
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
  const clerkPublishableKey = process.env.NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY;
  
  // Always render the same structure, conditionally wrap with ClerkProvider
  const content = (
    <html lang="en" className="dark">
      <body className={`${inter.variable} ${jetbrainsMono.variable} font-sans bg-background text-foreground antialiased`}>
        <Providers>
          <main className="min-h-screen w-full bg-black">
            {children}
          </main>
        </Providers>
      </body>
    </html>
  );

  // Only wrap with ClerkProvider if key is available
  if (clerkPublishableKey) {
    return (
      <ClerkProvider publishableKey={clerkPublishableKey}>
        {content}
      </ClerkProvider>
    );
  }

  return content;
}
