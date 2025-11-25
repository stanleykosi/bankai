/**
 * @description
 * Shared TypeScript definitions for the Bankai application.
 * Mirrors the backend models for frontend consumption.
 */

export interface Market {
  condition_id: string;
  gamma_market_id?: string;
  question_id: string;
  slug: string;
  title: string;
  description: string;
  resolution_rules?: string;
  image_url?: string;
  icon_url?: string;
  category: string;
  tags: string[];
  active: boolean;
  closed: boolean;
  archived: boolean;
  featured?: boolean;
  is_new?: boolean;
  restricted?: boolean;
  enable_order_book?: boolean;
  token_id_yes: string;
  token_id_no: string;
  market_maker_address?: string;
  start_date?: string;
  event_start_time?: string;
  accepting_orders?: boolean;
  accepting_orders_at?: string;
  ready?: boolean;
  funded?: boolean;
  pending_deployment?: boolean;
  deploying?: boolean;
  rfq_enabled?: boolean;
  holding_rewards_enabled?: boolean;
  fees_enabled?: boolean;
  neg_risk?: boolean;
  neg_risk_other?: boolean;
  automatically_active?: boolean;
  manual_activation?: boolean;
  volume_all_time?: number;
  volume_24h: number;
  volume_24h_amm?: number;
  volume_24h_clob?: number;
  volume_1w?: number;
  volume_1w_amm?: number;
  volume_1w_clob?: number;
  volume_1m?: number;
  volume_1m_amm?: number;
  volume_1m_clob?: number;
  volume_1y?: number;
  volume_1y_amm?: number;
  volume_1y_clob?: number;
  volume_amm?: number;
  volume_clob?: number;
  volume_num?: number;
  liquidity: number;
  liquidity_num?: number;
  liquidity_clob?: number;
  liquidity_amm?: number;
  order_min_size?: number;
  order_price_min_tick?: number;
  best_bid?: number;
  best_ask?: number;
  spread?: number;
  last_trade_price?: number;
  one_hour_price_change?: number;
  one_day_price_change?: number;
  one_week_price_change?: number;
  one_month_price_change?: number;
  one_year_price_change?: number;
  competitive?: number;
  rewards_min_size?: number;
  rewards_max_spread?: number;
  outcomes?: string;
  outcome_prices?: string;
  yes_price?: number;
  yes_best_bid?: number;
  yes_best_ask?: number;
  yes_price_updated?: string;
  no_price?: number;
  no_best_bid?: number;
  no_best_ask?: number;
  no_price_updated?: string;
  end_date: string; // ISO String
  created_at: string; // ISO String
  market_created_at?: string;
  market_updated_at?: string;

  trending_score?: number;
}

export interface User {
  id: string;
  clerk_id: string;
  email: string;
  eoa_address: string;
  vault_address: string;
  wallet_type: 'PROXY' | 'SAFE';
  created_at: string;
}

