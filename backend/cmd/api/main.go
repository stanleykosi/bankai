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
	"os"
	"strings"

	"github.com/bankai-project/backend/internal/api"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/db"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberLogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load config: %v", err)
	}

	// 2. Initialize Database Connections
	// Postgres (Supabase)
	pgDB, err := db.ConnectPostgres(cfg)
	if err != nil {
		logger.Fatal("Failed to connect to Postgres: %v", err)
	}

	// Redis (Cache & Queue)
	redisClient, err := db.ConnectRedis(cfg)
	if err != nil {
		logger.Fatal("Failed to connect to Redis: %v", err)
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
	app.Use(recover.New())        // Panic recovery
	app.Use(fiberLogger.New())    // Request logging
	
	// CORS Configuration
	// By default, allow all origins (useful for local testing and development).
	// Set FRONTEND_URL in production to restrict to specific origins (comma-separated).
	allowCredentials := true
	allowedOrigins := []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	if frontendURL := strings.TrimSpace(os.Getenv("FRONTEND_URL")); frontendURL != "" {
		for _, origin := range strings.Split(frontendURL, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				allowedOrigins = append(allowedOrigins, origin)
			}
		}
	} else {
		allowedOrigins = []string{"*"}
		// Browsers reject credentials with wildcard origins.
		allowCredentials = false
	}

	app.Use(cors.New(cors.Config{
		AllowOrigins:     strings.Join(dedupeStrings(allowedOrigins), ","),
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowMethods:     "GET, POST, PUT, DELETE, OPTIONS",
		AllowCredentials: allowCredentials,
	}))

	// 5. Setup Routes
	// Updated to pass redisClient
	api.SetupRoutes(app, pgDB, redisClient, cfg)

	// 6. Start Server
	logger.Info("🚀 Starting Bankai Backend on port %s", cfg.Server.Port)
	if err := app.Listen(":" + cfg.Server.Port); err != nil {
		logger.Fatal("Failed to start server: %v", err)
	}
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
