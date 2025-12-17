/**
 * @description
 * Global client-side state for the terminal.
 * Tracks the active market and the Oracle sidebar visibility.
 */

import { create } from "zustand";
import { Market } from "@/types";

interface TerminalStore {
  activeMarket: Market | null;
  setActiveMarket: (market: Market | null) => void;
  isOracleOpen: boolean;
  setOracleOpen: (open: boolean) => void;
  toggleOracle: () => void;
}

export const useTerminalStore = create<TerminalStore>((set) => ({
  activeMarket: null,
  setActiveMarket: (market) => set({ activeMarket: market }),
  isOracleOpen: false,
  setOracleOpen: (open) => set({ isOracleOpen: open }),
  toggleOracle: () => set((state) => ({ isOracleOpen: !state.isOracleOpen })),
}));

