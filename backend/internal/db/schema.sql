/**
 * Bankai Database Schema
 * Target: PostgreSQL 15+ (Supabase)
 * 
 * Includes:
 * - UUID extension for IDs
 * - pgvector for AI embeddings
 * - Core tables for Trading Terminal
 */

-- Enable necessary extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "vector"; -- For RAG/AI features

-- 1. Users Table
-- Stores identity mapping between Clerk (Auth) and On-chain Wallets
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    clerk_id VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255),
    
    -- The Externally Owned Account (Metamask or Embedded Key)
    eoa_address VARCHAR(42) NOT NULL,
    
    -- The actual fund-holding contract (Proxy or Gnosis Safe)
    vault_address VARCHAR(42),
    
    -- Type of vault wallet
    wallet_type VARCHAR(20) CHECK (wallet_type IN ('PROXY', 'SAFE')),
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_clerk_id ON users(clerk_id);
CREATE INDEX idx_users_eoa_address ON users(eoa_address);

-- 2. Markets Table
-- Stores metadata about Polymarket events
CREATE TABLE IF NOT EXISTS markets (
    condition_id VARCHAR(66) PRIMARY KEY, -- The unique Condition ID from CTF
    gamma_market_id VARCHAR(255),
    question_id VARCHAR(66),              -- UMA Question ID
    slug VARCHAR(255) NOT NULL,
    
    title TEXT NOT NULL,
    description TEXT,
    resolution_rules TEXT,
    image_url TEXT,
    icon_url TEXT,
    
    -- Categorization for AI/Discovery
    category VARCHAR(100),
    tags TEXT[], 
    
    -- Status flags
    active BOOLEAN DEFAULT TRUE,
    closed BOOLEAN DEFAULT FALSE,
    archived BOOLEAN DEFAULT FALSE,
    featured BOOLEAN DEFAULT FALSE,
    is_new BOOLEAN DEFAULT FALSE,
    restricted BOOLEAN DEFAULT FALSE,
    enable_order_book BOOLEAN DEFAULT FALSE,
    
    -- CLOB specific
    token_id_yes VARCHAR(255),
    token_id_no VARCHAR(255),
    market_maker_address TEXT,
    
    -- Date metadata
    start_date TIMESTAMPTZ,
    end_date TIMESTAMPTZ,
    event_start_time TIMESTAMPTZ,
    accepting_orders BOOLEAN DEFAULT FALSE,
    accepting_orders_at TIMESTAMPTZ,
    ready BOOLEAN DEFAULT FALSE,
    funded BOOLEAN DEFAULT FALSE,
    pending_deployment BOOLEAN DEFAULT FALSE,
    deploying BOOLEAN DEFAULT FALSE,
    rfq_enabled BOOLEAN DEFAULT FALSE,
    holding_rewards_enabled BOOLEAN DEFAULT FALSE,
    fees_enabled BOOLEAN DEFAULT FALSE,
    neg_risk BOOLEAN DEFAULT FALSE,
    neg_risk_other BOOLEAN DEFAULT FALSE,
    automatically_active BOOLEAN DEFAULT FALSE,
    manual_activation BOOLEAN DEFAULT FALSE,
    
    -- Volume metrics
    volume_all_time DECIMAL,
    volume_24h DECIMAL,
    volume_24h_amm DECIMAL,
    volume_24h_clob DECIMAL,
    volume_1w DECIMAL,
    volume_1w_amm DECIMAL,
    volume_1w_clob DECIMAL,
    volume_1m DECIMAL,
    volume_1m_amm DECIMAL,
    volume_1m_clob DECIMAL,
    volume_1y DECIMAL,
    volume_1y_amm DECIMAL,
    volume_1y_clob DECIMAL,
    volume_amm DECIMAL,
    volume_clob DECIMAL,
    volume_num DECIMAL,
    
    -- Liquidity metrics
    liquidity DECIMAL,
    liquidity_num DECIMAL,
    liquidity_clob DECIMAL,
    liquidity_amm DECIMAL,
    
    -- Orderbook / pricing
    order_min_size DECIMAL,
    order_price_min_tick DECIMAL,
    best_bid DECIMAL,
    best_ask DECIMAL,
    spread DECIMAL,
    last_trade_price DECIMAL,
    one_hour_price_change DECIMAL,
    one_day_price_change DECIMAL,
    one_week_price_change DECIMAL,
    one_month_price_change DECIMAL,
    one_year_price_change DECIMAL,
    
    competitive DECIMAL,
    rewards_min_size DECIMAL,
    rewards_max_spread DECIMAL,
    
    outcomes TEXT,
    outcome_prices TEXT,
    market_created_at TIMESTAMPTZ,
    market_updated_at TIMESTAMPTZ,
    
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_markets_slug ON markets(slug);
CREATE INDEX idx_markets_active ON markets(active);

-- 3. Price History Table
-- Time-series data for charting. 
-- Designed for high-volume inserts via RTDS worker.
CREATE TABLE IF NOT EXISTS price_history (
    id BIGSERIAL PRIMARY KEY,
    market_id VARCHAR(66) REFERENCES markets(condition_id),
    outcome VARCHAR(10) CHECK (outcome IN ('YES', 'NO')),
    price DECIMAL NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    
    -- Optional: Store volume for this tick/bucket if needed for OHLCV
    volume DECIMAL DEFAULT 0
);

-- Composite index for fast chart queries: "Get prices for Market X ordered by time"
CREATE INDEX idx_price_history_market_time ON price_history(market_id, outcome, timestamp DESC);

-- 4. Orders Table
-- Audit log of orders relayed through Bankai
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    
    -- The ID returned by Polymarket CLOB
    clob_order_id VARCHAR(255),
    
    market_id VARCHAR(66) REFERENCES markets(condition_id),
    side VARCHAR(4) CHECK (side IN ('BUY', 'SELL')),
    outcome VARCHAR(4) CHECK (outcome IN ('YES', 'NO')),
    
    price DECIMAL NOT NULL,
    size DECIMAL NOT NULL,
    order_type VARCHAR(10) CHECK (order_type IN ('MARKET', 'LIMIT', 'FOK', 'FAK', 'GTD', 'GTC')),
    
    status VARCHAR(20) DEFAULT 'PENDING', -- PENDING, OPEN, FILLED, CANCELED, FAILED
    
    tx_hash VARCHAR(66), -- If executed on-chain (for redemption/etc, though matching is off-chain)
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_orders_user ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);

-- 5. AI Analysis Cache (Optional/Advanced)
-- Stores RAG results to avoid re-querying expensive LLMs for same market
CREATE TABLE IF NOT EXISTS market_analysis (
    market_id VARCHAR(66) PRIMARY KEY REFERENCES markets(condition_id),
    analysis_text TEXT,
    sentiment_score DECIMAL, -- -1.0 to 1.0
    last_updated TIMESTAMPTZ DEFAULT NOW()
);

