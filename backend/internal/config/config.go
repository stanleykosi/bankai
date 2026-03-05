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
	"net/url"
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
	Auth       AuthConfig
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
	CollateralAssetID    string // USDC contract on Polygon used for collateral/pricing
	BuilderAPIKey        string
	BuilderSecret        string
	BuilderPass          string
	RelayerURL           string // Optional, used for gasless wallets
}

// ServicesConfig holds external service keys (AI, Auth, etc.)
type ServicesConfig struct {
	TavilyAPIKey                   string
	OpenAIAPIKey                   string
	OpenAIBaseURL                  string
	SynthDataAPIKey                string
	SynthDataBaseURL               string
	OpenAIModel                    string
	OpenAIMaxTokens                int
	OpenAIMaxContext               int
	PolygonRPCURL                  string
	SyncJobSecret                  string
	AIPicksMarketLimit             int
	AlphaSnapshotHour              int // UTC hour to run daily AI snapshot; -1 to run immediately
	MaxTrackedAssets               int // RTDS subscription cap; 0 = no cap
	StreamRecentHours              int // When >0, subscribe all markets with volume in the last N hours; 0 = subscribe all active markets
	RTDSActivityEnabled            bool
	APIRateLimitPerMin             int
	AuthRateLimitPerMin            int
	SignRateLimitPerMin            int
	TradeRateLimitPerMin           int
	WalletRateLimitPerMin          int
	AdminRateLimitPerMin           int
	RTDSWorkerPoolSize             int
	RTDSQueueSize                  int
	RetryMaxAttempts               int
	RetryBaseDelayMs               int
	MetricsEnabled                 bool
	MetricsToken                   string
	AdminWalletAllow               []string
	JobWorkerPoolSize              int
	TPSLCheckIntervalSeconds       int
	MarketReconcileIntervalSeconds int
	OrderbookReconcileIntervalSecs int
	NotificationCleanupMinutes     int
	UpDownEnabled                  bool
	UpDownReadOnly                 bool
	UpDownKillSwitch               bool
	UpDownEnterpriseEnabled        bool
	UpDownPollIntervalSeconds      int
	UpDownStaleThresholdSeconds    int
	UpDownClockDriftMaxSeconds     int
	UpDownEVMinThreshold           float64
	UpDownFeeBps                   float64
	UpDownDepthProbeShares         float64
	UpDownKellyFraction            float64
	UpDownMaxFractionPerTrade      float64
	UpDownAssetExposureCap         float64
	UpDownDailyDrawdownThrottle    float64
	UpDownNotionalBankroll         float64
	UpDownMinConfidence            float64
	UpDownMaxSpreadToTrade         float64
	UpDownMinTopDepth              float64
	UpDownNoTradeCutoff5mSeconds   int
	UpDownNoTradeCutoff15mSeconds  int
	UpDownNoTradeCutoff1hSeconds   int
	UpDownNoTradeCutoff4hSeconds   int
	UpDownMaxMarkets               int
}

// AuthConfig holds wallet-only auth configuration
type AuthConfig struct {
	JWTSecret       string
	JWTIssuer       string
	TokenTTLMinutes int
	NonceTTLMinutes int
	Domain          string
	URI             string
	CookieName      string
	CookieDomain    string
	CookieSecure    bool
	CookieSameSite  string
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
			CollateralAssetID:    strings.ToLower(getEnv("POLYMARKET_COLLATERAL_ASSET_ID", "0x2791bca1f2de4661ed88a30c99a7a9449aa84174")),
			BuilderAPIKey:        sanitizeCredential(getEnv("POLY_BUILDER_API_KEY", "")),
			BuilderSecret:        sanitizeCredential(getEnv("POLY_BUILDER_SECRET", "")), // Often empty/not used for local signing depending on setup, but good to have
			BuilderPass:          sanitizeCredential(getEnv("POLY_BUILDER_PASSPHRASE", "")),
			RelayerURL:           getEnv("POLYMARKET_RELAYER_URL", "https://relayer-v2.polymarket.com"),
		},
		Auth: AuthConfig{
			JWTSecret:       getEnv("AUTH_JWT_SECRET", ""),
			JWTIssuer:       getEnv("AUTH_JWT_ISSUER", "bankai"),
			TokenTTLMinutes: getEnvAsInt("AUTH_TOKEN_TTL_MINUTES", 60),
			NonceTTLMinutes: getEnvAsInt("AUTH_NONCE_TTL_MINUTES", 10),
			Domain:          "",
			URI:             "",
			CookieName:      getEnv("AUTH_COOKIE_NAME", "bankai_auth"),
			CookieDomain:    getEnv("AUTH_COOKIE_DOMAIN", ""),
			CookieSecure:    getEnvAsBool("AUTH_COOKIE_SECURE", false),
			CookieSameSite:  getEnv("AUTH_COOKIE_SAMESITE", "lax"),
		},
		Services: ServicesConfig{
			TavilyAPIKey:                   getEnv("TAVILY_API_KEY", ""),
			OpenAIAPIKey:                   getEnv("OPENAI_API_KEY", ""),
			OpenAIBaseURL:                  getEnv("OPENAI_BASE_URL", "https://openrouter.ai/api/v1/chat/completions"),
			SynthDataAPIKey:                getEnv("SYNTHDATA_API_KEY", ""),
			SynthDataBaseURL:               getEnv("SYNTHDATA_BASE_URL", "https://api.synthdata.co"),
			OpenAIModel:                    getEnv("OPENAI_MODEL", "minimax/minimax-m2.1"),
			OpenAIMaxTokens:                getEnvAsInt("OPENAI_MAX_TOKENS", 10000),
			OpenAIMaxContext:               getEnvAsInt("OPENAI_MAX_CONTEXT_TOKENS", 204800),
			PolygonRPCURL:                  getEnv("POLYGON_RPC_URL", ""),
			SyncJobSecret:                  getEnv("JOB_SYNC_SECRET", ""),
			AIPicksMarketLimit:             getEnvAsInt("AI_PICKS_MARKET_LIMIT", 0),
			AlphaSnapshotHour:              getEnvAsInt("ALPHA_SNAPSHOT_HOUR_UTC", -1),
			MaxTrackedAssets:               getEnvAsInt("STREAM_MAX_TRACKED_ASSETS", 0),
			StreamRecentHours:              getEnvAsInt("STREAM_RECENT_HOURS", 0),
			RTDSActivityEnabled:            getEnvAsBool("RTDS_ACTIVITY_ENABLED", true),
			APIRateLimitPerMin:             getEnvAsInt("API_RATE_LIMIT_PER_MINUTE", 240),
			AuthRateLimitPerMin:            getEnvAsInt("AUTH_RATE_LIMIT_PER_MINUTE", 30),
			SignRateLimitPerMin:            getEnvAsInt("SIGN_RATE_LIMIT_PER_MINUTE", 60),
			TradeRateLimitPerMin:           getEnvAsInt("TRADE_RATE_LIMIT_PER_MINUTE", 120),
			WalletRateLimitPerMin:          getEnvAsInt("WALLET_RATE_LIMIT_PER_MINUTE", 60),
			AdminRateLimitPerMin:           getEnvAsInt("ADMIN_RATE_LIMIT_PER_MINUTE", 30),
			RTDSWorkerPoolSize:             getEnvAsInt("RTDS_WORKER_POOL_SIZE", 64),
			RTDSQueueSize:                  getEnvAsInt("RTDS_QUEUE_SIZE", 4096),
			RetryMaxAttempts:               getEnvAsInt("POLY_RETRY_MAX_ATTEMPTS", 4),
			RetryBaseDelayMs:               getEnvAsInt("POLY_RETRY_BASE_DELAY_MS", 200),
			MetricsEnabled:                 getEnvAsBool("METRICS_ENABLED", false),
			MetricsToken:                   getEnv("METRICS_TOKEN", ""),
			AdminWalletAllow:               normalizeAddresses(getEnvAsCSV("ADMIN_WALLET_ALLOWLIST")),
			JobWorkerPoolSize:              getEnvAsInt("JOB_WORKER_POOL_SIZE", 6),
			TPSLCheckIntervalSeconds:       getEnvAsInt("TPSL_CHECK_INTERVAL_SECONDS", 15),
			MarketReconcileIntervalSeconds: getEnvAsInt("MARKET_RECONCILE_INTERVAL_SECONDS", 120),
			OrderbookReconcileIntervalSecs: getEnvAsInt("ORDERBOOK_RECONCILE_INTERVAL_SECONDS", 45),
			NotificationCleanupMinutes:     getEnvAsInt("NOTIFICATION_CLEANUP_INTERVAL_MINUTES", 120),
			UpDownEnabled:                  getEnvAsBool("UPDOWN_ENABLED", true),
			UpDownReadOnly:                 getEnvAsBool("UPDOWN_READ_ONLY", false),
			UpDownKillSwitch:               getEnvAsBool("UPDOWN_KILL_SWITCH", false),
			UpDownEnterpriseEnabled:        getEnvAsBool("UPDOWN_ENTERPRISE_ENABLED", true),
			UpDownPollIntervalSeconds:      getEnvAsInt("UPDOWN_POLL_INTERVAL_SECONDS", 2),
			UpDownStaleThresholdSeconds:    getEnvAsInt("UPDOWN_STALE_THRESHOLD_SECONDS", 8),
			UpDownClockDriftMaxSeconds:     getEnvAsInt("UPDOWN_CLOCK_DRIFT_MAX_SECONDS", 5),
			UpDownEVMinThreshold:           getEnvAsFloat("UPDOWN_EV_MIN_THRESHOLD", 0.0125),
			UpDownFeeBps:                   getEnvAsFloat("UPDOWN_FEE_BPS", 8),
			UpDownDepthProbeShares:         getEnvAsFloat("UPDOWN_DEPTH_PROBE_SHARES", 50),
			UpDownKellyFraction:            getEnvAsFloat("UPDOWN_KELLY_FRACTION", 0.35),
			UpDownMaxFractionPerTrade:      getEnvAsFloat("UPDOWN_MAX_FRACTION_PER_TRADE", 0.06),
			UpDownAssetExposureCap:         getEnvAsFloat("UPDOWN_ASSET_EXPOSURE_CAP", 0.18),
			UpDownDailyDrawdownThrottle:    getEnvAsFloat("UPDOWN_DAILY_DRAWDOWN_THROTTLE", 0.65),
			UpDownNotionalBankroll:         getEnvAsFloat("UPDOWN_NOTIONAL_BANKROLL", 1000),
			UpDownMinConfidence:            getEnvAsFloat("UPDOWN_MIN_CONFIDENCE", 0.50),
			UpDownMaxSpreadToTrade:         getEnvAsFloat("UPDOWN_MAX_SPREAD_TO_TRADE", 0.05),
			UpDownMinTopDepth:              getEnvAsFloat("UPDOWN_MIN_TOP_DEPTH", 50),
			UpDownNoTradeCutoff5mSeconds:   getEnvAsInt("UPDOWN_NO_TRADE_CUTOFF_5M_SECONDS", 30),
			UpDownNoTradeCutoff15mSeconds:  getEnvAsInt("UPDOWN_NO_TRADE_CUTOFF_15M_SECONDS", 30),
			UpDownNoTradeCutoff1hSeconds:   getEnvAsInt("UPDOWN_NO_TRADE_CUTOFF_1H_SECONDS", 120),
			UpDownNoTradeCutoff4hSeconds:   getEnvAsInt("UPDOWN_NO_TRADE_CUTOFF_4H_SECONDS", 300),
			UpDownMaxMarkets:               getEnvAsInt("UPDOWN_MAX_MARKETS", 64),
		},
	}

	applyAuthDefaults(cfg)

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
	if cfg.Auth.JWTSecret == "" && cfg.Server.Env != "test" {
		return fmt.Errorf("AUTH_JWT_SECRET is required")
	}
	return nil
}

func applyAuthDefaults(cfg *Config) {
	frontendURL := strings.TrimSpace(getEnv("FRONTEND_URL", ""))
	if frontendURL == "" {
		if cfg.Auth.URI == "" {
			cfg.Auth.URI = "http://localhost:3000"
		}
		if cfg.Auth.Domain == "" {
			cfg.Auth.Domain = "localhost:3000"
		}
		return
	}

	if cfg.Auth.URI == "" {
		cfg.Auth.URI = frontendURL
	}
	if cfg.Auth.Domain == "" {
		cfg.Auth.Domain = extractDomainFromURL(frontendURL)
	}
}

func extractDomainFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimPrefix(raw, "https://")
	}
	if parsed.Host != "" {
		return parsed.Host
	}
	return parsed.Path
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

func getEnvAsFloat(key string, fallback float64) float64 {
	if value, exists := os.LookupEnv(key); exists {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvAsBool(key string, fallback bool) bool {
	valueStr := strings.TrimSpace(getEnv(key, ""))
	if valueStr == "" {
		return fallback
	}
	switch strings.ToLower(valueStr) {
	case "1", "true", "t", "yes", "y":
		return true
	case "0", "false", "f", "no", "n":
		return false
	default:
		return fallback
	}
}

func getEnvAsCSV(key string) []string {
	value := strings.TrimSpace(getEnv(key, ""))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		clean := strings.TrimSpace(p)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func normalizeAddresses(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		addr := strings.ToLower(strings.TrimSpace(item))
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}
