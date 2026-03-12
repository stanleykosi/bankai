/**
 * @description
 * API Route definitions.
 * Sets up the router groups and assigns handlers.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/api/handlers
 * - backend/internal/api/middleware
 * - backend/internal/services
 * - backend/internal/polymarket/gamma
 * - backend/internal/polymarket/relayer
 * - backend/internal/polymarket/data_api
 */

package api

import (
	"time"

	"github.com/bankai-project/backend/internal/api/handlers"
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/integrations/openai"
	"github.com/bankai-project/backend/internal/integrations/synthdata"
	"github.com/bankai-project/backend/internal/integrations/tavily"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/polymarket/data_api"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/bankai-project/backend/internal/polymarket/relayer"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// SetupRoutes configures all API routes
// Updated to accept Redis client for Services
func SetupRoutes(app *fiber.App, db *gorm.DB, rdb *redis.Client, cfg *config.Config) {
	// 1. Initialize Middleware
	if err := middleware.InitAuthMiddleware(cfg); err != nil {
		logger.Error("Failed to init auth middleware: %v", err)
		// We don't panic here to allow app to start in dev modes without valid keys,
		// but protected routes will fail.
	}

	app.Use(middleware.SecurityHeaders())
	app.Use(middleware.TracingMiddleware())
	app.Use(middleware.MetricsMiddleware())
	app.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Redis:  rdb,
		Prefix: "api",
		Limit:  cfg.Services.APIRateLimitPerMin,
		Window: time.Minute,
		KeyFunc: func(c *fiber.Ctx) string {
			return middleware.ClientIdentifier(c)
		},
	}))

	// 2. Initialize Clients
	gammaClient := gamma.NewClient(cfg)
	relayerClient := relayer.NewClient(cfg)
	clobClient := clob.NewClient(cfg)
	tavilyClient := tavily.NewClient(cfg)
	openaiClient := openai.NewClient(cfg)
	synthClient := synthdata.NewClient(cfg)
	dataAPIClient := data_api.NewClient(cfg)
	subgraphClient := polymarket.NewSubgraphClient(cfg)

	// 3. Initialize Services
	marketService := services.NewMarketService(db, rdb, gammaClient, clobClient)
	walletManager := services.NewWalletManager(db, relayerClient, gammaClient)
	tradeService := services.NewTradeService(db, clobClient)
	oracleService := services.NewOracleService(marketService, tavilyClient, openaiClient)

	// Social & Intelligence Services
	profileService := services.NewProfileService(dataAPIClient, gammaClient, clobClient, rdb)
	socialService := services.NewSocialService(db, gammaClient)
	watchlistService := services.NewWatchlistService(db, marketService)
	settingsService := services.NewSettingsService(db)
	notificationService := services.NewNotificationService(db, socialService, settingsService)
	adminService := services.NewAdminService(db, rdb, notificationService)
	jobQueue := services.NewJobQueue(rdb, "jobs:default")
	alphaHubService := services.NewAlphaHubService(marketService, profileService, clobClient, tavilyClient, openaiClient, dataAPIClient, subgraphClient, cfg.Services.AIPicksMarketLimit, rdb)
	tpslService := services.NewTPSLService(rdb, clobClient, notificationService)
	upDownService := services.NewUpDownService(db, rdb, cfg, marketService, synthClient)

	// Initialize Blockchain Service
	blockchainService, err := services.NewBlockchainService(cfg)
	if err != nil {
		logger.Error("Failed to initialize blockchain service: %v", err)
		// Continue without blockchain service - balance checks will fail but app can still run
		blockchainService = nil
	}

	// 4. Initialize Handlers
	authHandler := handlers.NewAuthHandler(db, rdb, cfg)
	userHandler := handlers.NewUserHandler(db)
	marketHandler := handlers.NewMarketHandler(marketService)
	walletHandler := handlers.NewWalletHandler(walletManager, blockchainService, cfg.Polymarket.CollateralAssetID)
	tradeHandler := handlers.NewTradeHandler(tradeService, notificationService, cfg, db, rdb)
	oracleHandler := handlers.NewOracleHandler(oracleService)
	analysisHandler := handlers.NewAnalysisHandler(alphaHubService)

	// Social & Intelligence Handlers
	profileHandler := handlers.NewProfileHandler(profileService, socialService)
	socialHandler := handlers.NewSocialHandler(db, socialService, notificationService, profileService)
	watchlistHandler := handlers.NewWatchlistHandler(db, watchlistService)
	holdersHandler := handlers.NewHoldersHandler(profileService)
	settingsHandler := handlers.NewSettingsHandler(db, settingsService)
	tpslHandler := handlers.NewTPSLHandler(tpslService)
	adminHandler := handlers.NewAdminHandler(adminService, jobQueue)
	upDownHandler := handlers.NewUpDownHandler(upDownService)

	// 5. Define Routes
	// Root route for easy health checks
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"service": "Bankai Trading Terminal API",
			"version": "1.1.0",
			"health":  "/api/v1/health",
		})
	})

	api := app.Group("/api")
	v1 := api.Group("/v1")

	// Public Routes
	v1.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Auth Routes (Public)
	auth := v1.Group("/auth")
	auth.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Redis:  rdb,
		Prefix: "auth",
		Limit:  cfg.Services.AuthRateLimitPerMin,
		Window: time.Minute,
		KeyFunc: func(c *fiber.Ctx) string {
			return middleware.ClientIdentifier(c)
		},
	}))
	auth.Post("/challenge", authHandler.Challenge)
	auth.Post("/verify", authHandler.Verify)
	auth.Post("/logout", authHandler.Logout)
	auth.Get("/signer-assertion", middleware.Protected(), middleware.AccountGuard(rdb, db), authHandler.SignerAssertion)

	// Market Routes (Public)
	markets := v1.Group("/markets")
	markets.Get("/active", marketHandler.GetActiveMarkets)
	markets.Get("/fresh", marketHandler.GetFreshDrops)
	markets.Get("/meta", marketHandler.GetActiveMarketsMeta)
	markets.Get("/lanes", marketHandler.GetMarketLanes)
	markets.Get("/stream", marketHandler.StreamPriceUpdates)
	markets.Get("/:condition_id/history", marketHandler.GetPriceHistory)
	markets.Get("/:condition_id/depth", marketHandler.GetDepthEstimate)
	markets.Get("/:condition_id/holders", holdersHandler.GetMarketHolders) // Whale Table
	markets.Post("/:condition_id/stream", marketHandler.RequestMarketStream)
	markets.Get("/:slug", marketHandler.GetMarketBySlug)

	// Oracle Routes (Public for now, can be protected)
	oracle := v1.Group("/oracle")
	oracle.Get("/analyze/:condition_id", oracleHandler.AnalyzeMarket)

	// Analysis (Alpha Hub) Routes
	analysis := v1.Group("/analysis")
	analysis.Get("/snapshot", analysisHandler.GetSnapshot)
	analysis.Get("/smart-money", analysisHandler.GetSmartMoney)
	analysis.Get("/ai-picks", analysisHandler.GetAIPicks)
	analysis.Get("/whales/recent", analysisHandler.GetRecentWhales)
	analysis.Get("/whales/stream", analysisHandler.StreamWhaleUpdates)
	analysis.Post(
		"/ai-picks/cancel",
		middleware.Protected(),
		middleware.AccountGuard(rdb, db),
		middleware.AdminOnly(cfg.Services.AdminWalletAllow),
		analysisHandler.CancelAIPicks,
	)
	analysis.Post(
		"/ai-picks/resume",
		middleware.Protected(),
		middleware.AccountGuard(rdb, db),
		middleware.AdminOnly(cfg.Services.AdminWalletAllow),
		analysisHandler.ResumeAIPicks,
	)

	// Up/Down Pro Trading Routes
	updown := v1.Group("/updown")
	updown.Get("/markets", upDownHandler.GetMarkets)
	updown.Get("/market/:slug", upDownHandler.GetMarket)
	updown.Get("/signal/:slug", upDownHandler.GetSignal)
	updown.Get("/recommendations", upDownHandler.GetRecommendations)
	updown.Get("/stream", upDownHandler.Stream)
	updown.Get("/performance", upDownHandler.GetPerformance)
	updown.Post("/decisions", middleware.Protected(), middleware.AccountGuard(rdb, db), upDownHandler.LogDecision)

	// Profile Routes (Public - trader profiles are public)
	profile := v1.Group("/profile")
	profile.Get("/:address", profileHandler.GetTraderProfile)
	profile.Get("/:address/stats", profileHandler.GetTraderStats)
	profile.Get("/:address/positions", profileHandler.GetTraderPositions)
	profile.Get("/:address/closed-positions", profileHandler.GetTraderClosedPositions)
	profile.Get("/:address/trades", profileHandler.GetRecentTrades)

	// User Routes (Protected)
	user := v1.Group("/user", middleware.Protected(), middleware.AccountGuard(rdb, db))
	user.Get("/me", userHandler.GetMe)

	// Wallet Routes (Protected)
	wallet := v1.Group("/wallet", middleware.Protected(), middleware.AccountGuard(rdb, db))
	wallet.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Redis:  rdb,
		Prefix: "wallet",
		Limit:  cfg.Services.WalletRateLimitPerMin,
		Window: time.Minute,
		KeyFunc: func(c *fiber.Ctx) string {
			return middleware.ClientIdentifier(c)
		},
	}))
	wallet.Get("/", walletHandler.GetWallet)
	wallet.Get("", walletHandler.GetWallet)
	wallet.Get("/deploy/typed-data", walletHandler.GetDeployTypedData)
	wallet.Post("/deploy", walletHandler.DeployWallet)
	wallet.Post("/update", walletHandler.UpdateWallet)
	wallet.Get("/deposit", walletHandler.GetDepositAddress)
	wallet.Get("/balance", walletHandler.GetBalance)
	wallet.Get("/withdraw/nonce", walletHandler.GetWithdrawNonce)
	wallet.Post("/withdraw", walletHandler.Withdraw)

	// Trade Routes (Protected)
	trade := v1.Group("/trade", middleware.Protected(), middleware.AccountGuard(rdb, db))
	trade.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Redis:  rdb,
		Prefix: "trade",
		Limit:  cfg.Services.TradeRateLimitPerMin,
		Window: time.Minute,
		KeyFunc: func(c *fiber.Ctx) string {
			return middleware.ClientIdentifier(c)
		},
	}))
	// PostTrade and PostBatchTrade endpoints removed - frontend uses SDK directly
	// GetAuthTypedData endpoint removed - SDK handles API key derivation
	trade.Post("/sync", tradeHandler.SyncOrders)
	trade.Get("/triggers", tpslHandler.ListRules)
	trade.Post("/triggers", tpslHandler.CreateRule)
	trade.Delete("/triggers/:id", tpslHandler.CancelRule)

	// Social Routes (Protected)
	social := v1.Group("/social", middleware.Protected(), middleware.AccountGuard(rdb, db))
	social.Post("/follow", socialHandler.FollowTrader)
	social.Delete("/follow/:address", socialHandler.UnfollowTrader)
	social.Get("/following", socialHandler.GetFollowing)
	social.Get("/following/performance", socialHandler.GetFollowingPerformance)
	social.Get("/following/:address", socialHandler.CheckIsFollowing)
	social.Get("/notifications", socialHandler.GetNotifications)
	social.Post("/notifications/:id/read", socialHandler.MarkNotificationRead)
	social.Post("/notifications/read-all", socialHandler.MarkAllNotificationsRead)

	// Watchlist Routes (Protected)
	watchlist := v1.Group("/watchlist", middleware.Protected(), middleware.AccountGuard(rdb, db))
	watchlist.Get("/", watchlistHandler.GetWatchlist)
	watchlist.Get("", watchlistHandler.GetWatchlist)
	watchlist.Post("/bookmark", watchlistHandler.BookmarkMarket)
	watchlist.Post("/toggle", watchlistHandler.ToggleBookmark)
	watchlist.Delete("/:market_id", watchlistHandler.RemoveBookmark)
	watchlist.Get("/check/:market_id", watchlistHandler.CheckIsBookmarked)

	// Settings Routes (Protected)
	settings := v1.Group("/settings", middleware.Protected(), middleware.AccountGuard(rdb, db))
	settings.Get("", settingsHandler.GetSettings)
	settings.Get("/", settingsHandler.GetSettings)
	settings.Patch("", settingsHandler.UpdateSettings)
	settings.Patch("/", settingsHandler.UpdateSettings)
	settings.Post("/reset", settingsHandler.ResetSettings)

	// Admin routes (Protected + wallet allowlist)
	admin := v1.Group("/admin", middleware.Protected(), middleware.AccountGuard(rdb, db), middleware.AdminOnly(cfg.Services.AdminWalletAllow))
	admin.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Redis:  rdb,
		Prefix: "admin",
		Limit:  cfg.Services.AdminRateLimitPerMin,
		Window: time.Minute,
		KeyFunc: func(c *fiber.Ctx) string {
			return middleware.ClientIdentifier(c)
		},
	}))
	admin.Get("/moderation/blocks", adminHandler.ListBlockedAccounts)
	admin.Post("/moderation/block", adminHandler.BlockAccount)
	admin.Post("/moderation/unblock", adminHandler.UnblockAccount)
	admin.Get("/moderation/actions", adminHandler.ActionLog)
	admin.Patch("/markets/:condition_id", adminHandler.ModerateMarket)
	admin.Post("/notifications/broadcast", adminHandler.Broadcast)

	// Order sync routes removed: frontend reads order lifecycle directly from Polymarket.
}
