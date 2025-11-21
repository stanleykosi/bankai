/**
 * @description
 * Worker Service Entry Point.
 * Responsible for background tasks:
 * 1. Ingesting Real-Time Data (RTDS) from Polymarket via WebSocket.
 * 2. Processing background jobs (if queue is added later).
 * 3. Syncing active markets list to keep subscriptions fresh.
 *
 * @dependencies
 * - backend/internal/config
 * - backend/internal/db
 * - backend/internal/polymarket/rtds
 * - backend/internal/polymarket/gamma
 * - backend/internal/services
 */

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/db"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/bankai-project/backend/internal/polymarket/rtds"
	"github.com/bankai-project/backend/internal/services"
)

func main() {
	log.Println("🔥 Starting Bankai Worker...")

	// 1. Load Config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Connect DBs
	pgDB, err := db.ConnectPostgres(cfg)
	if err != nil {
		log.Fatalf("Postgres connection failed: %v", err)
	}

	redisClient, err := db.ConnectRedis(cfg)
	if err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}

	// 3. Initialize Services
	gammaClient := gamma.NewClient(cfg)
	marketService := services.NewMarketService(pgDB, redisClient, gammaClient)
	msgHandler := rtds.NewMessageHandler(pgDB, redisClient)
	wsClient := rtds.NewClient(cfg, msgHandler)

	// 4. Context with Cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5. Connect WebSocket
	go func() {
		if err := wsClient.Connect(ctx); err != nil {
			log.Printf("❌ WebSocket Client failed: %v", err)
			// In prod, might want to restart the pod, but here we just log
		}
	}()

	// 6. Subscription Loop
	// Periodically fetch "Active Markets" and subscribe to their tokens
	go func() {
		ticker := time.NewTicker(2 * time.Minute) // Refresh subscriptions every 2 mins
		defer ticker.Stop()

		// Initial sync
		syncSubscriptions(ctx, marketService, wsClient)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncSubscriptions(ctx, marketService, wsClient)
			}
		}
	}()

	// 7. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down worker...")
	cancel()
	
	// Close WebSocket connection gracefully
	if err := wsClient.Close(); err != nil {
		log.Printf("Error closing WebSocket: %v", err)
	}
	
	time.Sleep(1 * time.Second) // Give connections time to close
	log.Println("Worker exited.")
}

// syncSubscriptions fetches active markets and subscribes to their tokens
func syncSubscriptions(ctx context.Context, ms *services.MarketService, ws *rtds.Client) {
	log.Println("🔄 Syncing market subscriptions...")

	// 1. Ensure our local DB has fresh data from Gamma
	if err := ms.SyncActiveMarkets(ctx); err != nil {
		log.Printf("Failed to sync active markets from Gamma: %v", err)
		return
	}

	// 2. Get active markets
	markets, err := ms.GetActiveMarkets(ctx)
	if err != nil {
		log.Printf("Failed to get active markets: %v", err)
		return
	}

	// 3. Collect Token IDs
	var assetIDs []string
	for _, m := range markets {
		if m.TokenIDYes != "" {
			assetIDs = append(assetIDs, m.TokenIDYes)
		}
		if m.TokenIDNo != "" {
			assetIDs = append(assetIDs, m.TokenIDNo)
		}
	}

	if len(assetIDs) == 0 {
		log.Println("No assets to subscribe to.")
		return
	}

	log.Printf("Subscribing to %d assets...", len(assetIDs))

	// 4. Subscribe via WebSocket
	// Note: The CLOB WS might have limits on message size for subscriptions.
	// We might need to batch this in chunks if len > 500.
	// For MVP, assuming < 500 active tokens.
	if err := ws.Subscribe(assetIDs); err != nil {
		log.Printf("Failed to subscribe: %v", err)
	}
}

