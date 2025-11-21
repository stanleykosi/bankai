/**
 * @description
 * API Route definitions.
 * Sets up the router groups and assigns handlers.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/api/handlers
 * - backend/internal/api/middleware
 */

package api

import (
	"log"

	"github.com/bankai-project/backend/internal/api/handlers"
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// SetupRoutes configures all API routes
func SetupRoutes(app *fiber.App, db *gorm.DB, cfg *config.Config) {
	// 1. Initialize Middleware
	if err := middleware.InitAuthMiddleware(cfg); err != nil {
		log.Printf("Failed to init auth middleware: %v", err)
		// We don't panic here to allow app to start in dev modes without valid keys,
		// but protected routes will fail.
	}

	// 2. Initialize Handlers
	userHandler := handlers.NewUserHandler(db)

	// 3. Define Routes
	api := app.Group("/api")
	v1 := api.Group("/v1")

	// Public Routes
	v1.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// User Routes (Protected)
	user := v1.Group("/user", middleware.Protected())
	user.Post("/sync", userHandler.SyncUser)
	user.Get("/me", userHandler.GetMe)
}

