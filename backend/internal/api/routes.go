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
 */

package api

import (
	"github.com/bankai-project/backend/internal/api/handlers"
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket/clob"
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

	// 3. Initialize Services
	marketService := services.NewMarketService(db, rdb, gammaClient)
	walletManager := services.NewWalletManager(db, relayerClient, gammaClient)
	tradeService := services.NewTradeService(db, clobClient)

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
	tradeHandler := handlers.NewTradeHandler(tradeService, cfg)

	// 5. Define Routes
	// Root route for easy health checks
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"service": "Bankai Trading Terminal API",
			"version": "1.0.0",
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
	markets.Get("/:slug", marketHandler.GetMarketBySlug)

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
	trade.Post("/", tradeHandler.PostTrade)
	trade.Post("", tradeHandler.PostTrade)
}
