/**
 * @description
 * Main entry point for the Bankai Backend API.
 * Initializes the Fiber web server, loads configuration, and sets up routes.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2: Web framework
 * - github.com/joho/godotenv: Environment variable loader
 *
 * @notes
 * - Currently sets up a basic health check.
 * - Future integration points for Database, Redis, and Background Workers are marked.
 */

package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

func main() {
	// 1. Load Environment Variables
	// We attempt to load from .env, but don't fail hard if it's missing (prod might use system envs)
	if err := godotenv.Load(); err != nil {
		log.Println("Info: No .env file found, relying on system environment variables")
	}

	// 2. Initialize Fiber App
	// optimized for high concurrency
	app := fiber.New(fiber.Config{
		AppName:       "Bankai Trading Terminal",
		StrictRouting: true,
		CaseSensitive: true,
	})

	// 3. Global Middleware
	app.Use(recover.New()) // Panic recovery
	app.Use(logger.New())  // Request logging
	app.Use(cors.New(cors.Config{
		// TODO: In production, replace "*" with specific frontend URL (e.g., "https://bankai.vercel.app")
		AllowOrigins:     "*",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowMethods:     "GET, POST, PUT, DELETE, OPTIONS",
		AllowCredentials: true,
	}))

	// 4. Routes
	api := app.Group("/api")

	// Health Check
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  "ok",
			"service": "bankai-backend",
		})
	})

	// 5. Start Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting Bankai Backend on port %s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

