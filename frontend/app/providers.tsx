"use client";

/**
 * @description
 * Client-side providers wrapper for React Query and Wagmi.
 * Separated from layout.tsx because these are client components.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WagmiProvider } from "wagmi";
import { config } from "@/lib/web3";
import { useEffect, useState } from "react";
import { installLogRedaction } from "@/lib/logRedaction";
import { LinkClickInterceptor } from "@/components/debug/LinkClickInterceptor";

export function Providers({ children }: { children: React.ReactNode }) {
  // Create QueryClient instance with default options
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 60 * 1000, // 1 minute
            refetchOnWindowFocus: false,
          },
        },
      })
  );

  // Install once on client to avoid leaking credentials in console logs
  useEffect(() => {
    installLogRedaction();
  }, []);

  return (
    <WagmiProvider config={config}>
      <QueryClientProvider client={queryClient}>
        {children}
        <LinkClickInterceptor />
      </QueryClientProvider>
    </WagmiProvider>
  );
}
