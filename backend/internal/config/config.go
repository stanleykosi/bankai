/**
 * @description
 * Configuration loader for Bankai Backend.
 * Responsible for reading environment variables, setting defaults, and performing strict validation.
 *
 * @dependencies
 * - github.com/joho/godotenv: For loading .env files
 * - standard "os": For reading env vars
 * - standard "fmt" & "log": For error reporting
 *
 * @notes
 * - Fails fast if critical variables (Database URLs, API Keys) are missing.
 * - Uses a Singleton-like pattern where Load() returns a Config struct.
 */

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	Server     ServerConfig
	DB         DBConfig
	Redis      RedisConfig
	Polymarket PolymarketConfig
	Services   ServicesConfig
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port string
	Env  string // "development" or "production"
}

// DBConfig holds PostgreSQL settings
type DBConfig struct {
	URL string
}

// RedisConfig holds Redis settings
type RedisConfig struct {
	URL string
}

// PolymarketConfig holds Polymarket API endpoints and keys
type PolymarketConfig struct {
	ClobURL              string
	GammaURL             string
	DataAPIURL           string // Polymarket Data API for positions, holders, trades
	OrderbookSubgraphURL string // Goldsky Orderbook subgraph for deep trade history
	CollateralAssetID    string // Native USDC on Polygon (used to price trades)
	BuilderAPIKey        string
	BuilderSecret        string
	BuilderPass          string
	RelayerURL           string // Optional, used for gasless wallets
}

// ServicesConfig holds external service keys (AI, Auth, etc.)
type ServicesConfig struct {
	ClerkSecretKey     string
	ClerkJWKSURL       string // URL to fetch JSON Web Key Set for JWT validation
	TavilyAPIKey       string
	OpenAIAPIKey       string
	OpenAIBaseURL      string
	OpenAIModel        string
	OpenAIMaxTokens    int
	PolygonRPCURL      string
	SyncJobSecret      string
	AIPicksMarketLimit int
	AlphaSnapshotHour  int // UTC hour to run daily AI snapshot; -1 to run immediately
	MaxTrackedAssets   int // RTDS subscription cap; 0 = no cap
	StreamRecentHours  int // When >0, subscribe all markets with volume in the last N hours; 0 = use cap-only behavior
}

// Load reads .env file and populates the Config struct
func Load() (*Config, error) {
	// Attempt to load .env, but don't crash if it fails (k8s/prod might inject env vars directly)
	_ = godotenv.Load()

	cfg := &Config{
		Server: ServerConfig{
			Port: getEnv("PORT", "8080"),
			Env:  getEnv("GO_ENV", "development"),
		},
		DB: DBConfig{
			URL: getEnv("DATABASE_URL", ""),
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL", "redis://localhost:6379"),
		},
		Polymarket: PolymarketConfig{
			ClobURL:              getEnv("POLYMARKET_CLOB_URL", "https://clob.polymarket.com"),
			GammaURL:             getEnv("POLYMARKET_GAMMA_URL", "https://gamma-api.polymarket.com"),
			DataAPIURL:           getEnv("POLYMARKET_DATA_API_URL", "https://data-api.polymarket.com"),
			OrderbookSubgraphURL: getEnv("POLYMARKET_ORDERBOOK_SUBGRAPH_URL", "https://api.goldsky.com/api/public/project_cl6mb8i9h0003e201j6li0diw/subgraphs/orderbook-subgraph/0.0.1/gn"),
			CollateralAssetID:    strings.ToLower(getEnv("POLYMARKET_COLLATERAL_ASSET_ID", "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359")),
			BuilderAPIKey:        sanitizeCredential(getEnv("POLY_BUILDER_API_KEY", "")),
			BuilderSecret:        sanitizeCredential(getEnv("POLY_BUILDER_SECRET", "")), // Often empty/not used for local signing depending on setup, but good to have
			BuilderPass:          sanitizeCredential(getEnv("POLY_BUILDER_PASSPHRASE", "")),
			RelayerURL:           getEnv("POLYMARKET_RELAYER_URL", "https://relayer-v2.polymarket.com"),
		},
		Services: ServicesConfig{
			ClerkSecretKey:     getEnv("CLERK_SECRET_KEY", ""),
			ClerkJWKSURL:       getEnv("CLERK_JWKS_URL", ""),
			TavilyAPIKey:       getEnv("TAVILY_API_KEY", ""),
			OpenAIAPIKey:       getEnv("OPENAI_API_KEY", ""),
			OpenAIBaseURL:      getEnv("OPENAI_BASE_URL", "https://openrouter.ai/api/v1/chat/completions"),
			OpenAIModel:        getEnv("OPENAI_MODEL", "minimax/minimax-m2.1"),
			OpenAIMaxTokens:    getEnvAsInt("OPENAI_MAX_TOKENS", 10000),
			PolygonRPCURL:      getEnv("POLYGON_RPC_URL", ""),
			SyncJobSecret:      getEnv("JOB_SYNC_SECRET", ""),
			AIPicksMarketLimit: getEnvAsInt("AI_PICKS_MARKET_LIMIT", 0),
			AlphaSnapshotHour:  getEnvAsInt("ALPHA_SNAPSHOT_HOUR_UTC", -1),
			MaxTrackedAssets:   getEnvAsInt("STREAM_MAX_TRACKED_ASSETS", 1200),
			StreamRecentHours:  getEnvAsInt("STREAM_RECENT_HOURS", 24),
		},
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate checks for required variables
func validate(cfg *Config) error {
	if cfg.DB.URL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.Services.ClerkSecretKey == "" && cfg.Server.Env != "test" {
		// Warning: strictly required for Auth middleware
		fmt.Println("Warning: CLERK_SECRET_KEY is missing. Auth middleware will fail.")
	}
	return nil
}

// Helper to get env var with default
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func sanitizeCredential(value string) string {
	trimmed := strings.TrimSpace(value)
	return strings.Trim(trimmed, "\"")
}

// Helper to get env var as int
func getEnvAsInt(key string, fallback int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return fallback
	}
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return fallback
}
