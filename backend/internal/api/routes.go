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
 */

package api

import (
	"log"

	"github.com/bankai-project/backend/internal/api/handlers"
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
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
		log.Printf("Failed to init auth middleware: %v", err)
		// We don't panic here to allow app to start in dev modes without valid keys,
		// but protected routes will fail.
	}

	// 2. Initialize Services
	gammaClient := gamma.NewClient(cfg)
	marketService := services.NewMarketService(db, rdb, gammaClient)

	// 3. Initialize Handlers
	userHandler := handlers.NewUserHandler(db)
	marketHandler := handlers.NewMarketHandler(marketService)

	// 4. Define Routes
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

	// User Routes (Protected)
	user := v1.Group("/user", middleware.Protected())
	user.Post("/sync", userHandler.SyncUser)
	user.Get("/me", userHandler.GetMe)
}

