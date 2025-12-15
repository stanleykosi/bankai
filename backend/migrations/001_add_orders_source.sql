-- Adds source column to orders to distinguish Bankai vs external orders
ALTER TABLE orders
ADD COLUMN IF NOT EXISTS source VARCHAR(16) DEFAULT 'UNKNOWN';

CREATE INDEX IF NOT EXISTS idx_orders_source ON orders(source);
