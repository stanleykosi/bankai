export type SmartMoneyStats = {
  net_buy_usd: number;
  net_sell_usd: number;
  buys_count: number;
  sells_count: number;
  whale_hits_count: number;
  wallets_considered: number;
  gold_buys: number;
  silver_buys: number;
  bronze_buys: number;
  avg_entry_vs_mid_bps: number;
};

export type WalletSnapshot = {
  address: string;
  tier: string;
  win_rate: number;
  realized_pnl: number;
};

export type MarketSignal = {
  market_id: string;
  token_id: string;
  title: string;
  slug: string;
  category: string;
  resolves_at?: string;
  yes_price: number;
  best_bid: number;
  best_ask: number;
  spread_bps: number;
  volume_24h: number;
  volume_7d: number;
  p1h: number;
  p24h: number;
  p7d: number;
  smart_money: SmartMoneyStats;
  score: number;
  wallets_sample?: WalletSnapshot[];
};

export type WhaleEvent = {
  ts: string;
  market_id: string;
  token_id: string;
  side: string;
  size_usd: number;
  price: number;
  wallet: string;
  wallet_tier: string;
  win_rate: number;
  realized_pnl: number;
  slug?: string;
  title?: string;
  spread_bps?: number;
  is_wash_trade: boolean;
};

export type SmartMoneyResponse = {
  window_seconds: number;
  markets: MarketSignal[];
  whales: WhaleEvent[];
  generated_at: string;
};

export type AIPick = {
  market_id: string;
  slug: string;
  title: string;
  probability_yes: number;
  conviction: string;
  action: string;
  rationale: string;
};

export type TokenEstimate = {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
};

export type AIChunkMeta = {
  chunk: number;
  markets: number;
  news_fetched: number;
  token_estimate: TokenEstimate;
};

export type AIResponse = {
  ai_picks: AIPick[];
  raw_content?: string;
  model?: string;
  generated_at?: string;
  expires_at?: string;
  window_seconds?: number;
  source?: string;
  stale?: boolean;
  news_markets?: number;
  chunks?: AIChunkMeta[];
  token_estimate?: TokenEstimate;
  completion_note?: string;
};
