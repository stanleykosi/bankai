"use client";

import { useEffect, useMemo, useState } from "react";
import { Settings as SettingsIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import type {
  DefaultOrderType,
  FollowedTraderAlerts,
  NotificationChannel,
  UpdateSettingsPayload,
  UserSettings,
} from "@/types";
import { useSettingsManager } from "@/hooks/useSettings";

type Tab = "trading" | "notifications";

interface SettingsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const DEFAULTS: Pick<
  UserSettings,
  | "default_order_type"
  | "slippage_tolerance_bps"
  | "trade_confirmation_enabled"
  | "notification_channel"
  | "whale_alert_threshold_usd"
  | "order_fill_notifications"
  | "resolution_alerts"
  | "followed_trader_alerts"
> = {
  default_order_type: "GTC",
  slippage_tolerance_bps: 100,
  trade_confirmation_enabled: true,
  notification_channel: "IN_APP",
  whale_alert_threshold_usd: 5000,
  order_fill_notifications: true,
  resolution_alerts: true,
  followed_trader_alerts: "ALL",
};

export function SettingsModal({ open, onOpenChange }: SettingsModalProps) {
  const { settings, isLoading, update, reset, isUpdating, isResetting } =
    useSettingsManager();

  const [tab, setTab] = useState<Tab>("trading");
  const [draft, setDraft] = useState(DEFAULTS);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setError(null);
      return;
    }
    if (!settings) {
      setDraft(DEFAULTS);
      return;
    }
    setDraft({
      default_order_type: settings.default_order_type,
      slippage_tolerance_bps: settings.slippage_tolerance_bps,
      trade_confirmation_enabled: settings.trade_confirmation_enabled,
      notification_channel: settings.notification_channel,
      whale_alert_threshold_usd: settings.whale_alert_threshold_usd,
      order_fill_notifications: settings.order_fill_notifications,
      resolution_alerts: settings.resolution_alerts,
      followed_trader_alerts: settings.followed_trader_alerts,
    });
  }, [open, settings]);

  const isBusy = isUpdating || isResetting;

  const slippagePercent = useMemo(() => draft.slippage_tolerance_bps / 100, [draft.slippage_tolerance_bps]);

  const handleSave = async () => {
    setError(null);
    const payload: UpdateSettingsPayload = {
      default_order_type: draft.default_order_type,
      slippage_tolerance_bps: draft.slippage_tolerance_bps,
      trade_confirmation_enabled: draft.trade_confirmation_enabled,
      notification_channel: draft.notification_channel,
      whale_alert_threshold_usd: draft.whale_alert_threshold_usd,
      order_fill_notifications: draft.order_fill_notifications,
      resolution_alerts: draft.resolution_alerts,
      followed_trader_alerts: draft.followed_trader_alerts,
    };

    try {
      await update(payload);
      onOpenChange(false);
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || "Failed to save settings");
    }
  };

  const handleReset = async () => {
    setError(null);
    try {
      await reset();
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || "Failed to reset settings");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <SettingsIcon className="h-4 w-4" />
            Settings
          </DialogTitle>
          <DialogDescription>
            Trading defaults and notification preferences.
          </DialogDescription>
        </DialogHeader>

        <div className="flex rounded-md border border-border bg-background/40 p-1 text-[10px] font-mono uppercase tracking-wide">
          <button
            type="button"
            onClick={() => setTab("trading")}
            className={cn(
              "flex-1 rounded-sm px-3 py-2 transition-colors",
              tab === "trading"
                ? "bg-primary/20 text-foreground"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Trading
          </button>
          <button
            type="button"
            onClick={() => setTab("notifications")}
            className={cn(
              "flex-1 rounded-sm px-3 py-2 transition-colors",
              tab === "notifications"
                ? "bg-primary/20 text-foreground"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Notifications
          </button>
        </div>

        {error && (
          <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3">
            <p className="text-sm text-destructive font-mono">{error}</p>
          </div>
        )}

        {tab === "trading" ? (
          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                Default Order Type
              </label>
              <select
                className="w-full rounded-md border border-border bg-background/60 px-3 py-2 text-sm font-mono"
                value={draft.default_order_type}
                disabled={isBusy || isLoading}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    default_order_type: e.target.value as DefaultOrderType,
                  }))
                }
              >
                <option value="GTC">Limit • Good Til Cancel</option>
                <option value="GTD">Limit • Good Til Date</option>
                <option value="FAK">Market • Fill And Kill</option>
                <option value="FOK">Market • Fill Or Kill</option>
              </select>
            </div>

            <div className="space-y-2">
              <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                Slippage Tolerance (bps)
              </label>
              <Input
                type="number"
                min={10}
                max={500}
                step={10}
                disabled={isBusy || isLoading}
                className="font-mono"
                value={draft.slippage_tolerance_bps}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    slippage_tolerance_bps: Math.max(
                      10,
                      Math.min(500, Number(e.target.value || 0))
                    ),
                  }))
                }
              />
              <p className="text-[10px] text-muted-foreground font-mono">
                {slippagePercent.toFixed(2)}% max price deviation for market orders.
              </p>
            </div>

            <label className="flex items-center justify-between rounded-md border border-border bg-background/40 px-3 py-2">
              <span className="text-sm font-mono">Trade confirmation</span>
              <input
                type="checkbox"
                className="h-4 w-4 accent-primary"
                checked={draft.trade_confirmation_enabled}
                disabled={isBusy || isLoading}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    trade_confirmation_enabled: e.target.checked,
                  }))
                }
              />
            </label>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                Channel
              </label>
              <select
                className="w-full rounded-md border border-border bg-background/60 px-3 py-2 text-sm font-mono"
                value={draft.notification_channel}
                disabled={isBusy || isLoading}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    notification_channel: e.target.value as NotificationChannel,
                  }))
                }
              >
                <option value="IN_APP">In-app</option>
                <option value="NONE">None</option>
              </select>
            </div>

            <div className="space-y-2">
              <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                Followed Trader Alerts
              </label>
              <select
                className="w-full rounded-md border border-border bg-background/60 px-3 py-2 text-sm font-mono"
                value={draft.followed_trader_alerts}
                disabled={isBusy || isLoading}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    followed_trader_alerts: e.target.value as FollowedTraderAlerts,
                  }))
                }
              >
                <option value="ALL">All trades</option>
                <option value="LARGE_ONLY">Large trades only</option>
                <option value="NONE">None</option>
              </select>
            </div>

            <div className="space-y-2">
              <label className="text-[10px] uppercase tracking-wide text-muted-foreground font-mono">
                Large Trade Threshold (USD)
              </label>
              <Input
                type="number"
                min={0}
                step={100}
                disabled={isBusy || isLoading}
                className="font-mono"
                value={draft.whale_alert_threshold_usd}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    whale_alert_threshold_usd: Math.max(
                      0,
                      Number(e.target.value || 0)
                    ),
                  }))
                }
              />
            </div>

            <label className="flex items-center justify-between rounded-md border border-border bg-background/40 px-3 py-2">
              <span className="text-sm font-mono">Order fill notifications</span>
              <input
                type="checkbox"
                className="h-4 w-4 accent-primary"
                checked={draft.order_fill_notifications}
                disabled={isBusy || isLoading}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    order_fill_notifications: e.target.checked,
                  }))
                }
              />
            </label>

            <label className="flex items-center justify-between rounded-md border border-border bg-background/40 px-3 py-2">
              <span className="text-sm font-mono">Resolution alerts</span>
              <input
                type="checkbox"
                className="h-4 w-4 accent-primary"
                checked={draft.resolution_alerts}
                disabled={isBusy || isLoading}
                onChange={(e) =>
                  setDraft((prev) => ({
                    ...prev,
                    resolution_alerts: e.target.checked,
                  }))
                }
              />
            </label>
          </div>
        )}

        <div className="flex items-center justify-between gap-3 pt-2">
          <Button
            type="button"
            variant="outline"
            disabled={isBusy || isLoading}
            onClick={handleReset}
          >
            Reset
          </Button>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="ghost"
              disabled={isBusy}
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="button" disabled={isBusy || isLoading} onClick={handleSave}>
              Save
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
