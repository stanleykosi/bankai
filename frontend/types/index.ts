/**
 * @description
 * Shared TypeScript definitions for the Bankai application.
 * Mirrors the backend models for frontend consumption.
 */

export interface Market {
  condition_id: string;
  question_id: string;
  slug: string;
  title: string;
  description: string;
  category: string;
  tags: string[];
  active: boolean;
  closed: boolean;
  archived: boolean;
  token_id_yes: string;
  token_id_no: string;
  volume_24h: number;
  liquidity: number;
  end_date: string; // ISO String
  created_at: string; // ISO String
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

