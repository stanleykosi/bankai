/**
 * Migration: Up/Down Market Window Tracking
 *
 * Adds:
 * - updown_market_windows (uniform per-window tracking)
 */

CREATE TABLE IF NOT EXISTS updown_market_windows (
    id BIGSERIAL PRIMARY KEY,
    slug VARCHAR(255) NOT NULL,
    condition_id VARCHAR(66) NOT NULL,
    asset VARCHAR(16) NOT NULL,
    window_type VARCHAR(8) NOT NULL,
    resolution_source_type VARCHAR(24) NOT NULL,
    event_start_time TIMESTAMPTZ NOT NULL,
    event_end_time TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'scheduled',
    reference_start_price DECIMAL,
    reference_current_price DECIMAL,
    reference_end_price DECIMAL,
    resolved_outcome VARCHAR(16) NOT NULL DEFAULT 'PENDING',
    outcome_resolved_at TIMESTAMPTZ,
    p_final_up DECIMAL,
    recommendation_id VARCHAR(320),
    recommendation_decision VARCHAR(24),
    recommendation_side VARCHAR(12),
    recommendation_expected_value DECIMAL,
    recommendation_confidence DECIMAL,
    recommendation_limit_price DECIMAL,
    recommendation_size_shares DECIMAL,
    signal_timestamp TIMESTAMPTZ NOT NULL,
    signal_payload JSONB NOT NULL,
    recommendation_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(condition_id, event_start_time)
);

CREATE INDEX IF NOT EXISTS idx_updown_windows_slug_start ON updown_market_windows(slug, event_start_time DESC);
CREATE INDEX IF NOT EXISTS idx_updown_windows_asset_window_start ON updown_market_windows(asset, window_type, event_start_time DESC);
CREATE INDEX IF NOT EXISTS idx_updown_windows_resolved ON updown_market_windows(resolved_outcome, event_start_time DESC);
