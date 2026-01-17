/**
 * @description
 * Global client-side state for the terminal.
 * Tracks the active market and the Oracle sidebar visibility.
 */

import { create } from "zustand";
import { Market } from "@/types";
import type { UserSettings } from "@/types";

interface TerminalStore {
  activeMarket: Market | null;
  setActiveMarket: (market: Market | null) => void;
  isOracleOpen: boolean;
  setOracleOpen: (open: boolean) => void;
  toggleOracle: () => void;

  settings: UserSettings | null;
  setSettings: (settings: UserSettings | null) => void;
  slippageToleranceBps: () => number;
  tradeConfirmationEnabled: () => boolean;
  notificationChannel: () => "IN_APP" | "NONE";
}

export const useTerminalStore = create<TerminalStore>((set, get) => ({
  activeMarket: null,
  setActiveMarket: (market) => set({ activeMarket: market }),
  isOracleOpen: false,
  setOracleOpen: (open) => set({ isOracleOpen: open }),
  toggleOracle: () => set((state) => ({ isOracleOpen: !state.isOracleOpen })),

  settings: null,
  setSettings: (settings) => set({ settings }),
  slippageToleranceBps: () => get().settings?.slippage_tolerance_bps ?? 100,
  tradeConfirmationEnabled: () => get().settings?.trade_confirmation_enabled ?? true,
  notificationChannel: () => get().settings?.notification_channel ?? "IN_APP",
}));
