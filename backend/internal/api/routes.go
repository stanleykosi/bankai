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
	"github.com/bankai-project/backend/internal/api/handlers"
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/integrations/openai"
	"github.com/bankai-project/backend/internal/integrations/tavily"
	"github.com/bankai-project/backend/internal/logger"
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

	// 2. Initialize Clients
	gammaClient := gamma.NewClient(cfg)
	relayerClient := relayer.NewClient(cfg)
	clobClient := clob.NewClient(cfg)
	tavilyClient := tavily.NewClient(cfg)
	openaiClient := openai.NewClient(cfg)
	dataAPIClient := data_api.NewClient(cfg)

	// 3. Initialize Services
	marketService := services.NewMarketService(db, rdb, gammaClient, clobClient)
	walletManager := services.NewWalletManager(db, relayerClient, gammaClient)
	tradeService := services.NewTradeService(db, clobClient)
	oracleService := services.NewOracleService(marketService, tavilyClient, openaiClient)

	// Social & Intelligence Services
	profileService := services.NewProfileService(dataAPIClient, gammaClient, rdb)
	socialService := services.NewSocialService(db, gammaClient)
	watchlistService := services.NewWatchlistService(db)
	notificationService := services.NewNotificationService(db, socialService)

	// Initialize Blockchain Service
	blockchainService, err := services.NewBlockchainService(cfg)
	if err != nil {
		logger.Error("Failed to initialize blockchain service: %v", err)
		// Continue without blockchain service - balance checks will fail but app can still run
		blockchainService = nil
	}

	// 4. Initialize Handlers
	userHandler := handlers.NewUserHandler(db)
	marketHandler := handlers.NewMarketHandler(marketService)
	walletHandler := handlers.NewWalletHandler(walletManager, blockchainService)
	tradeHandler := handlers.NewTradeHandler(tradeService, cfg, db)
	oracleHandler := handlers.NewOracleHandler(oracleService)

	// Social & Intelligence Handlers
	profileHandler := handlers.NewProfileHandler(profileService, socialService)
	socialHandler := handlers.NewSocialHandler(db, socialService, notificationService)
	watchlistHandler := handlers.NewWatchlistHandler(db, watchlistService)
	holdersHandler := handlers.NewHoldersHandler(profileService)

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

	// Profile Routes (Public - trader profiles are public)
	profile := v1.Group("/profile")
	profile.Get("/:address", profileHandler.GetTraderProfile)
	profile.Get("/:address/stats", profileHandler.GetTraderStats)
	profile.Get("/:address/positions", profileHandler.GetTraderPositions)
	profile.Get("/:address/activity", profileHandler.GetActivityHeatmap)
	profile.Get("/:address/trades", profileHandler.GetRecentTrades)

	// User Routes (Protected)
	user := v1.Group("/user", middleware.Protected())
	user.Post("/sync", userHandler.SyncUser)
	user.Get("/me", userHandler.GetMe)

	// Wallet Routes (Protected)
	wallet := v1.Group("/wallet", middleware.Protected())
	wallet.Get("/", walletHandler.GetWallet)
	wallet.Get("", walletHandler.GetWallet)
	wallet.Get("/deploy/typed-data", walletHandler.GetDeployTypedData)
	wallet.Post("/deploy", walletHandler.DeployWallet)
	wallet.Post("/update", walletHandler.UpdateWallet)
	wallet.Get("/deposit", walletHandler.GetDepositAddress)
	wallet.Get("/balance", walletHandler.GetBalance)
	wallet.Post("/withdraw", walletHandler.Withdraw)

	// Trade Routes (Protected)
	trade := v1.Group("/trade", middleware.Protected())
	// PostTrade and PostBatchTrade endpoints removed - frontend uses SDK directly
	// GetAuthTypedData endpoint removed - SDK handles API key derivation
	trade.Get("/orders", tradeHandler.GetOrders)
	trade.Post("/cancel", tradeHandler.CancelOrder)
	trade.Post("/cancel/batch", tradeHandler.CancelOrders)
	trade.Post("/sync", tradeHandler.SyncOrders) // Persist Polymarket orders/trades from SDK ingestion

	// Social Routes (Protected)
	social := v1.Group("/social", middleware.Protected())
	social.Post("/follow", socialHandler.FollowTrader)
	social.Delete("/follow/:address", socialHandler.UnfollowTrader)
	social.Get("/following", socialHandler.GetFollowing)
	social.Get("/following/:address", socialHandler.CheckIsFollowing)
	social.Get("/notifications", socialHandler.GetNotifications)
	social.Post("/notifications/:id/read", socialHandler.MarkNotificationRead)
	social.Post("/notifications/read-all", socialHandler.MarkAllNotificationsRead)

	// Watchlist Routes (Protected)
	watchlist := v1.Group("/watchlist", middleware.Protected())
	watchlist.Get("/", watchlistHandler.GetWatchlist)
	watchlist.Get("", watchlistHandler.GetWatchlist)
	watchlist.Post("/bookmark", watchlistHandler.BookmarkMarket)
	watchlist.Post("/toggle", watchlistHandler.ToggleBookmark)
	watchlist.Delete("/:market_id", watchlistHandler.RemoveBookmark)
	watchlist.Get("/check/:market_id", watchlistHandler.CheckIsBookmarked)

	// Internal sync route (secured via JOB_SYNC_SECRET header) for background workers
	app.Post("/api/v1/trade/sync/internal", tradeHandler.SyncOrdersInternal)
}

