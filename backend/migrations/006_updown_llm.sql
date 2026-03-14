/**
 * Migration: Up/Down LLM Directional Engine
 *
 * Adds:
 * - updown_llm_packets
 * - updown_llm_shadow_daily
 */

CREATE TABLE IF NOT EXISTS updown_llm_packets (
    id BIGSERIAL PRIMARY KEY,
    slug VARCHAR(255) NOT NULL,
    condition_id VARCHAR(66) NOT NULL,
    asset VARCHAR(16) NOT NULL,
    window_type VARCHAR(8) NOT NULL,
    event_start_time TIMESTAMPTZ NOT NULL,
    context_hash VARCHAR(128) NOT NULL,
    prompt_hash VARCHAR(128) NOT NULL,
    model VARCHAR(160) NOT NULL,
    decision VARCHAR(24) NOT NULL,
    recommended_side VARCHAR(12) NOT NULL,
    confidence DECIMAL NOT NULL,
    expected_value DECIMAL NOT NULL DEFAULT 0,
    suggested_limit_price DECIMAL NOT NULL DEFAULT 0,
    suggested_size_shares DECIMAL NOT NULL DEFAULT 0,
    suggested_notional DECIMAL NOT NULL DEFAULT 0,
    effective_guard_blocks JSONB NOT NULL DEFAULT '[]'::jsonb,
    freshness_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    allora_proxy_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    trace_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    packet_payload JSONB NOT NULL,
    context_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_updown_llm_packets_slug_window_created
    ON updown_llm_packets(slug, event_start_time, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_updown_llm_packets_asset_window_created
    ON updown_llm_packets(asset, window_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_updown_llm_packets_context_hash
    ON updown_llm_packets(context_hash, created_at DESC);

CREATE TABLE IF NOT EXISTS updown_llm_shadow_daily (
    id BIGSERIAL PRIMARY KEY,
    day DATE NOT NULL,
    asset VARCHAR(16) NOT NULL,
    window_type VARCHAR(8) NOT NULL,
    windows_count INTEGER NOT NULL DEFAULT 0,
    deterministic_ev DECIMAL NOT NULL DEFAULT 0,
    llm_ev DECIMAL NOT NULL DEFAULT 0,
    ev_delta DECIMAL NOT NULL DEFAULT 0,
    deterministic_brier DECIMAL NOT NULL DEFAULT 0,
    llm_brier DECIMAL NOT NULL DEFAULT 0,
    brier_delta DECIMAL NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(day, asset, window_type)
);

CREATE INDEX IF NOT EXISTS idx_updown_llm_shadow_daily_day
    ON updown_llm_shadow_daily(day DESC);
CREATE INDEX IF NOT EXISTS idx_updown_llm_shadow_daily_asset_window
    ON updown_llm_shadow_daily(asset, window_type, day DESC);
