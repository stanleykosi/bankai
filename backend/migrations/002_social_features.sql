/**
 * Migration: Social & Intelligence Features
 * 
 * Adds tables for:
 * - follows: Track follower relationships for copy-trading
 * - market_bookmarks: User's watchlist of favorited markets
 * - notifications: Trade alerts for followed traders
 * 
 * Note: Trade activity data (for heatmaps) is cached in Redis, not PostgreSQL.
 * This provides faster reads and automatic TTL expiration.
 */

-- 1. Follows Table
-- Stores follower/target relationships for the Follow System
CREATE TABLE IF NOT EXISTS follows (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_address VARCHAR(42) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(follower_id, target_address)
);

CREATE INDEX IF NOT EXISTS idx_follows_follower ON follows(follower_id);
CREATE INDEX IF NOT EXISTS idx_follows_target ON follows(target_address);

-- 2. Market Bookmarks Table
-- Stores user's starred/bookmarked markets for watchlist
CREATE TABLE IF NOT EXISTS market_bookmarks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    market_id VARCHAR(66) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, market_id)
);

CREATE INDEX IF NOT EXISTS idx_bookmarks_user ON market_bookmarks(user_id);
CREATE INDEX IF NOT EXISTS idx_bookmarks_market ON market_bookmarks(market_id);

-- 3. Notifications Table
-- Stores trade alerts for followed traders
CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type VARCHAR(32) NOT NULL DEFAULT 'TRADE_ALERT',
    title VARCHAR(255) NOT NULL,
    message TEXT,
    data JSONB,
    read BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_read ON notifications(user_id, read);
CREATE INDEX IF NOT EXISTS idx_notifications_created ON notifications(created_at DESC);

