/**
 * @description
 * Shared TypeScript definitions for the Bankai application.
 * Mirrors the backend models for frontend consumption.
 */

export * from "./settings";

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
  email: string;
  eoa_address: string;
  vault_address: string | null;
  wallet_type: "PROXY" | "SAFE" | null;
  created_at: string;
}

export type OrderStatus =
  | "PENDING"
  | "OPEN"
  | "PARTIALLY_FILLED"
  | "FILLED"
  | "CANCELED"
  | "EXPIRED"
  | "REJECTED"
  | "FAILED";

export interface OrderRecord {
  id: string;
  user_id: string;
  clob_order_id: string;
  market_id?: string | null;
  side: "BUY" | "SELL";
  outcome: string;
  outcome_token_id: string;
  price: number;
  size: number;
  order_type: string;
  status: OrderStatus;
  status_detail?: string | null;
  order_hashes?: string[] | null;
  error_msg?: string | null;
  tx_hash?: string | null;
  source?: "BANKAI" | "EXTERNAL" | "UNKNOWN";
  maker_address?: string | null;
  created_at: string;
  updated_at: string;
}

export interface OrderHistoryResponse {
  data: OrderRecord[];
  total: number;
  limit: number;
  offset: number;
}

export interface FillRecord {
  id: string;
  market_id?: string | null;
  outcome: string;
  side: "BUY" | "SELL";
  price: number;
  size: number;
  matched_at: string;
}

export interface DepthEstimateLevel {
  price: number;
  available: number;
  used: number;
  cumulativeSize: number;
  cumulativeValue: number;
}

export interface DepthEstimate {
  marketId: string;
  tokenId: string;
  side: "BUY" | "SELL";
  requestedSize: number;
  fillableSize: number;
  estimatedAveragePrice: number;
  estimatedTotalValue: number;
  insufficientLiquidity: boolean;
  levels: DepthEstimateLevel[];
}

// ===== Social & Intelligence Layer Types =====

// Trader Profile
export interface TraderStats {
  win_rate: number;
  portfolio_value: number;
  realized_pnl: number;
  unrealized_pnl: number;
  predictions: number;
  winning_trades: number;
  losing_trades: number;
  open_positions: number;
  closed_positions: number;
}

export interface TraderProfile {
  address: string;
  proxy_wallet?: string;
  profile_name?: string;
  profile_image?: string;
  profile_image_optimized?: string;
  bio?: string;
  is_verified?: boolean;
  ens_name?: string;
  lens_handle?: string;
  joined_at?: string;
  stats?: TraderStats;
}

export interface TraderProfileResponse {
  profile: TraderProfile;
  follower_count: number;
}

// Positions
export interface Position {
  asset: string;
  conditionId: string;
  tokenId: string;
  outcome: string;
  size: number;
  avgPrice: number;
  curPrice: number;
  initialValue: number;
  currentValue: number;
  cashPnl: number;
  percentPnl: number;
  totalBought: number;
  totalSold: number;
  realizedPnl: number;
  unrealizedPnl: number;
  slug: string;
  title?: string;
  proxyWallet: string;
  owner: string;
}

export interface PositionsResponse {
  positions: Position[];
  count: number;
}

// Closed Positions
export interface ClosedPosition {
  asset: string;
  conditionId: string;
  tokenId: string;
  outcome: string;
  size: number;
  avgPrice: number;
  exitPrice: number;
  initialValue: number;
  exitValue: number;
  realizedPnl: number;
  pctPnl: number;
  slug: string;
  title?: string;
  closedAt: string;
  resolved: boolean;
  winner: boolean;
}

export interface ClosedPositionsResponse {
  positions: ClosedPosition[];
  count: number;
}

// Activity Heatmap
export interface ActivityDataPoint {
  date: string;
  trade_count: number;
  volume: number;
  level: number; // 0-4 intensity
}

export interface ActivityResponse {
  activity: ActivityDataPoint[];
}

// Trades
export interface Trade {
  id: string;
  conditionId: string;
  tokenId: string;
  outcome: string;
  side: "BUY" | "SELL";
  price: number;
  size: number;
  value: number;
  maker: string;
  taker: string;
  slug: string;
  title: string;
  timestamp: number;
  transactionHash: string;
}

export interface TradesResponse {
  trades: Trade[];
  count: number;
}

// Rewards
export interface RewardsConfig {
  asset_address: string;
  start_date: string;
  end_date: string;
  rate_per_day: number;
  total_rewards: number;
}

export interface RewardToken {
  token_id: string;
  outcome: string;
  price: number;
}

export interface MarketReward {
  condition_id: string;
  question: string;
  market_slug: string;
  event_slug: string;
  image: string;
  rewards_max_spread: number;
  rewards_min_size: number;
  tokens: RewardToken[];
  rewards_config: RewardsConfig[];
}

export interface RewardEarning {
  asset_address: string;
  earnings: number;
  asset_rate: number;
}

export interface UserRewardsEarning extends MarketReward {
  maker_address: string;
  earning_percentage: number;
  earnings: RewardEarning[];
  market_competitiveness: number;
}

export interface UserRewardTotal {
  date: string;
  asset_address: string;
  maker_address: string;
  earnings: number;
  asset_rate: number;
}

// Holders (Whale Table)
export interface Holder {
  address: string;
  proxyAddress?: string;
  size: number;
  value: number;
  percentage: number;
  profileName?: string;
  profileImage?: string;
}

export interface HoldersResponse {
  holders: Holder[];
  count: number;
  condition_id: string;
  token_id?: string;
}

// Follow System
export interface Follow {
  id: string;
  follower_id: string;
  target_address: string;
  created_at: string;
  profile_name?: string;
  profile_image?: string;
  is_verified?: boolean;
}

export interface FollowingResponse {
  following: Follow[];
  count: number;
}

export interface FollowPerformance {
  target_address: string;
  profile_name?: string;
  profile_image?: string;
  stats?: TraderStats;
  total_pnl: number;
}

export interface FollowingPerformanceResponse {
  following: FollowPerformance[];
  count: number;
}

export interface FollowStatusResponse {
  is_following: boolean;
  target: string;
}

export interface FollowActionResponse {
  success: boolean;
  following: boolean;
  target: string;
}

// Notifications
export type NotificationType = "TRADE_ALERT" | "FOLLOWED" | "SYSTEM";

export interface Notification {
  id: string;
  user_id: string;
  type: NotificationType;
  title: string;
  message: string;
  data?: string; // JSON string with additional data
  read: boolean;
  created_at: string;
}

export interface NotificationsResponse {
  notifications: Notification[];
  unread_count: number;
  count: number;
}

// Watchlist
export interface WatchlistItem {
  id: string;
  user_id: string;
  market_id: string;
  created_at: string;
  title: string;
  slug?: string;
  image_url?: string;
  yes_price: number;
  no_price: number;
  volume_24h: number;
  one_day_change: number;
}

export interface WatchlistResponse {
  watchlist: WatchlistItem[];
  count: number;
}

export interface BookmarkStatusResponse {
  is_bookmarked: boolean;
  market_id: string;
}

export interface BookmarkActionResponse {
  success: boolean;
  bookmarked: boolean;
  market_id: string;
}

// Up/Down Pro Trading
export interface RiskFlags {
  read_only: boolean;
  kill_switch: boolean;
  synth_missing: boolean;
  synth_stale: boolean;
  market_stale: boolean;
  depth_missing: boolean;
  wide_spread: boolean;
  status_boundary: boolean;
  source_mismatch: boolean;
  clock_drift: boolean;
  low_liquidity: boolean;
  high_volatility: boolean;
  data_integrity_failed: boolean;
}

export interface RecommendationPrefill {
  side: "BUY" | "SELL";
  outcome_index: number;
  limit_price: number;
  size_shares: number;
  disabled: boolean;
  disabled_why?: string;
}

export interface Recommendation {
  id: string;
  slug: string;
  condition_id: string;
  asset: string;
  window_type: "5m" | "15m" | "1h" | "4h" | "unknown";
  profile: string;
  decision: "BUY_UP" | "BUY_DOWN" | "NO_TRADE";
  recommended_side: "UP" | "DOWN" | "NONE";
  order_side: "BUY" | "SELL";
  expected_value: number;
  confidence: number;
  suggested_limit_price: number;
  suggested_size_shares: number;
  suggested_notional: number;
  kelly_raw: number;
  kelly_capped: number;
  reason_codes: string[];
  invalidation_conditions: string[];
  risk_flags: RiskFlags;
  prefill: RecommendationPrefill;
  generated_at: string;
}

export interface UpDownSignal {
  slug: string;
  condition_id: string;
  asset: string;
  window_type: "5m" | "15m" | "1h" | "4h" | "unknown";
  resolution_source_type: "chainlink" | "binance" | "unknown";
  timestamp: string;
  reference_start_price?: number;
  reference_current_price?: number;
  reference_end_price?: number;
  reference_updated_at?: string;
  p_market_up?: number;
  p_synth_up?: number;
  p_model_up?: number;
  p_lp_up?: number;
  p_final_up: number;
  executable_ask_up: number;
  executable_ask_down: number;
  executable_bid_up: number;
  executable_bid_down: number;
  spread_up: number;
  spread_down: number;
  depth_imbalance: number;
  expected_slippage: number;
  ev_up: number;
  ev_down: number;
  ev_min_threshold: number;
  fees_bps: number;
  time_to_expiry_ms: number;
  regime: string;
  confidence: number;
  risk_flags: RiskFlags;
  reason_codes: string[];
  recommendation: Recommendation;
  locked_recommendation?: Recommendation;
  recommendation_locked_at?: string;
}

export interface UpDownMarket {
  market_type: "updown_crypto";
  slug: string;
  condition_id: string;
  asset: "BTC" | "ETH" | "SOL" | "XRP" | string;
  window_type: "5m" | "15m" | "1h" | "4h" | "unknown";
  resolution_source_type: "chainlink" | "binance" | "unknown";
  tradable: boolean;
  is_active_window: boolean;
  time_to_start_seconds: number;
  time_to_end_seconds: number;
  created_at?: string;
  event_start_time: string;
  event_end_time: string;
  resolution_rule_summary: string;
  market: Market;
  outcome_index_up: number;
  outcome_index_down: number;
  outcome_label_up: string;
  outcome_label_down: string;
}

export interface DecisionLog {
  id: string;
  user_id: string;
  slug: string;
  recommendation_id: string;
  action: "accepted" | "rejected" | "manual_override" | "placed";
  chosen_side?: string;
  override_price?: number;
  override_size?: number;
  notes?: string;
  created_at: string;
}

export interface PerformanceSlice {
  key: string;
  trades: number;
  hit_rate: number;
  brier_score: number;
  realized_ev: number;
  max_drawdown: number;
}

export interface PerformanceSummary {
  from: string;
  to: string;
  decisions: number;
  accepted: number;
  rejected: number;
  manual_overrides: number;
  trades: number;
  hit_rate: number;
  brier_score: number;
  realized_ev: number;
  max_drawdown: number;
  by_asset: PerformanceSlice[];
  by_window: PerformanceSlice[];
  updated_at: string;
}
