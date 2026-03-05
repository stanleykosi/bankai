/**
 * Migration: Up/Down Pro Trading Module
 *
 * Adds:
 * - updown_signal_snapshots
 * - updown_recommendations
 * - updown_decisions
 * - updown_performance_daily
 */

CREATE TABLE IF NOT EXISTS updown_signal_snapshots (
    id BIGSERIAL PRIMARY KEY,
    slug VARCHAR(255) NOT NULL,
    condition_id VARCHAR(66) NOT NULL,
    asset VARCHAR(16) NOT NULL,
    window_type VARCHAR(8) NOT NULL,
    resolution_source_type VARCHAR(24) NOT NULL,
    p_market_up DECIMAL,
    p_synth_up DECIMAL,
    p_model_up DECIMAL,
    p_final_up DECIMAL NOT NULL,
    ev_up DECIMAL NOT NULL,
    ev_down DECIMAL NOT NULL,
    confidence DECIMAL NOT NULL,
    signal_payload JSONB NOT NULL,
    reason_codes JSONB DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_updown_signal_slug_created ON updown_signal_snapshots(slug, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_updown_signal_asset_window ON updown_signal_snapshots(asset, window_type, created_at DESC);

CREATE TABLE IF NOT EXISTS updown_recommendations (
    id BIGSERIAL PRIMARY KEY,
    recommendation_id VARCHAR(320) UNIQUE NOT NULL,
    slug VARCHAR(255) NOT NULL,
    condition_id VARCHAR(66) NOT NULL,
    asset VARCHAR(16) NOT NULL,
    window_type VARCHAR(8) NOT NULL,
    decision VARCHAR(24) NOT NULL,
    recommended_side VARCHAR(12) NOT NULL,
    expected_value DECIMAL NOT NULL,
    confidence DECIMAL NOT NULL,
    suggested_limit_price DECIMAL NOT NULL,
    suggested_size_shares DECIMAL NOT NULL,
    kelly_raw DECIMAL NOT NULL,
    kelly_capped DECIMAL NOT NULL,
    risk_flags JSONB DEFAULT '{}'::jsonb,
    recommendation_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_updown_recommendations_slug_created ON updown_recommendations(slug, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_updown_recommendations_asset_window ON updown_recommendations(asset, window_type, created_at DESC);

CREATE TABLE IF NOT EXISTS updown_decisions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug VARCHAR(255) NOT NULL,
    recommendation_id VARCHAR(320),
    action VARCHAR(24) NOT NULL CHECK (action IN ('accepted', 'rejected', 'manual_override', 'placed')),
    chosen_side VARCHAR(12),
    override_price DECIMAL,
    override_size DECIMAL,
    notes TEXT,
    eventual_outcome VARCHAR(16),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_updown_decisions_user_created ON updown_decisions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_updown_decisions_slug_created ON updown_decisions(slug, created_at DESC);

CREATE TABLE IF NOT EXISTS updown_performance_daily (
    id BIGSERIAL PRIMARY KEY,
    day DATE NOT NULL,
    asset VARCHAR(16) NOT NULL,
    window_type VARCHAR(8) NOT NULL,
    trades_count INTEGER DEFAULT 0,
    hit_rate DECIMAL DEFAULT 0,
    brier_score DECIMAL DEFAULT 0,
    realized_ev DECIMAL DEFAULT 0,
    max_drawdown DECIMAL DEFAULT 0,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(day, asset, window_type)
);

CREATE INDEX IF NOT EXISTS idx_updown_performance_day ON updown_performance_daily(day DESC);
CREATE INDEX IF NOT EXISTS idx_updown_performance_asset_window ON updown_performance_daily(asset, window_type, day DESC);

