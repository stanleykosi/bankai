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
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/db"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/bankai-project/backend/internal/polymarket/rtds"
	"github.com/bankai-project/backend/internal/services"
)

const maxTrackedAssets = 800

func main() {
	logger.Info("🔥 Starting Bankai Worker...")

	// 1. Load Config
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load config: %v", err)
	}

	// 2. Connect DBs
	pgDB, err := db.ConnectPostgres(cfg)
	if err != nil {
		logger.Fatal("Postgres connection failed: %v", err)
	}

	redisClient, err := db.ConnectRedis(cfg)
	if err != nil {
		logger.Fatal("Redis connection failed: %v", err)
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
			logger.Error("❌ WebSocket Client failed: %v", err)
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

	logger.Info("Shutting down worker...")
	cancel()

	// Close WebSocket connection gracefully
	if err := wsClient.Close(); err != nil {
		logger.Error("Error closing WebSocket: %v", err)
	}

	time.Sleep(1 * time.Second) // Give connections time to close
	logger.Info("Worker exited.")
}

// syncSubscriptions fetches active markets and subscribes to their tokens
func syncSubscriptions(ctx context.Context, ms *services.MarketService, ws *rtds.Client) {
	logger.Info("🔄 Syncing market subscriptions...")

	// 1. Ensure our local DB has fresh data from Gamma
	// Sync both active markets and fresh drops
	if err := ms.SyncActiveMarkets(ctx); err != nil {
		logger.Error("Failed to sync active markets from Gamma: %v", err)
		return
	}

	// Also sync fresh drops to populate that endpoint
	if err := ms.SyncFreshDrops(ctx); err != nil {
		logger.Error("Failed to sync fresh drops from Gamma: %v", err)
		// Don't return - continue with active markets even if fresh drops fail
	}

	// 2. Get prioritised market assets (top liquidity/volume)
	marketAssets, err := ms.GetMarketAssets(ctx, maxTrackedAssets)
	if err != nil {
		logger.Error("Failed to get market assets: %v", err)
		return
	}

	// 3. Collect token IDs
	var assetIDs []string
	for _, asset := range marketAssets {
		if asset.TokenIDYes != "" {
			assetIDs = append(assetIDs, asset.TokenIDYes)
		}
		if asset.TokenIDNo != "" {
			assetIDs = append(assetIDs, asset.TokenIDNo)
		}
	}

	// 4. Include any ad-hoc stream requests (e.g., markets opened in the UI)
	if requested, err := ms.PopRequestedStreamTokens(ctx, maxTrackedAssets*2); err != nil {
		logger.Error("Failed to pop requested stream tokens: %v", err)
	} else if len(requested) > 0 {
		logger.Info("Including %d requested assets in subscription set...", len(requested))
		assetIDs = append(assetIDs, requested...)
	}

	if len(assetIDs) == 0 {
		logger.Info("No assets to subscribe to.")
		return
	}

	logger.Info("Subscribing to %d assets...", len(assetIDs))

	// 5. Subscribe via WebSocket (client batches internally)
	if err := ws.ReplaceSubscriptions(assetIDs); err != nil {
		logger.Error("Failed to subscribe: %v", err)
	}
}
