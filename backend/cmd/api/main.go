/**
 * @description
 * Main entry point for the Bankai Backend API.
 * Initializes the Fiber web server, loads configuration, and sets up routes.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2: Web framework
 * - github.com/bankai-project/backend/internal/config: Config loader
 * - github.com/bankai-project/backend/internal/db: Database connections
 * - github.com/bankai-project/backend/internal/api: Route definitions
 *
 * @notes
 * - Connects to Postgres and Redis on startup.
 * - Sets up basic middleware (CORS, Logger, Recover).
 */

package main

import (
	"log"
	"os"

	"github.com/bankai-project/backend/internal/api"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/db"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Initialize Database Connections
	// Postgres (Supabase)
	pgDB, err := db.ConnectPostgres(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to Postgres: %v", err)
	}

	// Redis (Cache & Queue)
	redisClient, err := db.ConnectRedis(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	// We'll use redisClient for handlers later
	_ = redisClient

	// 3. Initialize Fiber App
	app := fiber.New(fiber.Config{
		AppName:       "Bankai Trading Terminal",
		StrictRouting: true,
		CaseSensitive: true,
	})

	// 4. Global Middleware
	app.Use(recover.New()) // Panic recovery
	app.Use(logger.New())  // Request logging
	
	// CORS Configuration
	// By default, allow all origins (useful for local testing and development)
	// Set FRONTEND_URL in production if you want to restrict to a specific domain
	allowedOrigins := "*"
	if frontendURL := os.Getenv("FRONTEND_URL"); frontendURL != "" {
		// If FRONTEND_URL is set, use it (but still allow localhost for testing)
		allowedOrigins = frontendURL
	}
	
	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowMethods:     "GET, POST, PUT, DELETE, OPTIONS",
		AllowCredentials: true,
	}))

	// 5. Setup Routes
	// Updated to pass redisClient
	api.SetupRoutes(app, pgDB, redisClient, cfg)

	// 6. Start Server
	log.Printf("🚀 Starting Bankai Backend on port %s", cfg.Server.Port)
	if err := app.Listen(":" + cfg.Server.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

