/**
 * Migration: User Settings (Phase 1)
 *
 * Adds:
 * - user_settings: Per-user preferences for trading + notifications.
 *
 * Notes:
 * - Uses a one-to-one relationship with users (UNIQUE user_id).
 * - Application layer performs validation; DB uses sensible defaults.
 */

CREATE TABLE IF NOT EXISTS user_settings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Trading Settings
    default_order_type VARCHAR(10) DEFAULT 'GTC',
    slippage_tolerance_bps INTEGER DEFAULT 100,
    trade_confirmation_enabled BOOLEAN DEFAULT TRUE,

    -- Notification Settings
    notification_channel VARCHAR(20) DEFAULT 'IN_APP',
    whale_alert_threshold_usd DECIMAL(12,2) DEFAULT 5000.00,
    order_fill_notifications BOOLEAN DEFAULT TRUE,
    resolution_alerts BOOLEAN DEFAULT TRUE,
    followed_trader_alerts VARCHAR(20) DEFAULT 'ALL',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_settings_user ON user_settings(user_id);
