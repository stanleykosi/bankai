export type DefaultOrderType = "GTC" | "GTD" | "FOK" | "FAK";
export type NotificationChannel = "IN_APP" | "NONE";
export type FollowedTraderAlerts = "ALL" | "LARGE_ONLY" | "NONE";

export interface UserSettings {
  id: string;
  user_id: string;
  default_order_type: DefaultOrderType;
  slippage_tolerance_bps: number;
  trade_confirmation_enabled: boolean;
  notification_channel: NotificationChannel;
  whale_alert_threshold_usd: number;
  order_fill_notifications: boolean;
  resolution_alerts: boolean;
  followed_trader_alerts: FollowedTraderAlerts;
  created_at: string;
  updated_at: string;
}

export interface UpdateSettingsPayload {
  default_order_type?: DefaultOrderType;
  slippage_tolerance_bps?: number;
  trade_confirmation_enabled?: boolean;
  notification_channel?: NotificationChannel;
  whale_alert_threshold_usd?: number;
  order_fill_notifications?: boolean;
  resolution_alerts?: boolean;
  followed_trader_alerts?: FollowedTraderAlerts;
}

