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
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/db"
	"github.com/bankai-project/backend/internal/integrations/openai"
	"github.com/bankai-project/backend/internal/integrations/tavily"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/polymarket/data_api"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/bankai-project/backend/internal/polymarket/rtds"
	"github.com/bankai-project/backend/internal/services"
)

const enableDBWrites = false

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
	clobClient := clob.NewClient(cfg)
	dataAPIClient := data_api.NewClient(cfg)
	openaiClient := openai.NewClient(cfg)
	tavilyClient := tavily.NewClient(cfg)
	subgraphClient := polymarket.NewSubgraphClient(cfg)

	marketService := services.NewMarketService(pgDB, redisClient, gammaClient, clobClient)
	profileService := services.NewProfileService(dataAPIClient, gammaClient, clobClient, redisClient)
	socialService := services.NewSocialService(pgDB, gammaClient)
	settingsService := services.NewSettingsService(pgDB)
	notificationService := services.NewNotificationService(pgDB, socialService, settingsService)
	adminService := services.NewAdminService(pgDB, redisClient, notificationService)
	tpslService := services.NewTPSLService(redisClient, clobClient, notificationService)
	alphaHubService := services.NewAlphaHubService(marketService, profileService, clobClient, tavilyClient, openaiClient, dataAPIClient, subgraphClient, cfg.Services.AIPicksMarketLimit, redisClient)
	jobQueue := services.NewJobQueue(redisClient, "jobs:default")
	jobProcessor := services.NewJobProcessor(marketService, tpslService, notificationService, adminService)
	cacheAllowlist := rtds.NewCacheAllowlist(20 * time.Minute)
	msgHandler := rtds.NewMessageHandler(pgDB, redisClient)
	msgHandler.SetCacheAllowlist(cacheAllowlist)
	msgHandler.SetCacheTTLs(10*time.Minute, 0)
	wsClient := rtds.NewClient(cfg, msgHandler)
	var activityClient *rtds.ActivityClient
	chainlinkHandler := rtds.NewChainlinkHandler(redisClient)
	chainlinkClient := rtds.NewChainlinkClient(chainlinkHandler)

	// 4. Context with Cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobQueue.StartWorkers(ctx, cfg.Services.JobWorkerPoolSize, jobProcessor.Process)
	go enqueueRecurringJobs(ctx, jobQueue, cfg)

	// 5. Connect WebSocket
	go func() {
		if err := wsClient.Connect(ctx); err != nil {
			logger.Error("❌ WebSocket Client failed: %v", err)
			// In prod, might want to restart the pod, but here we just log
		}
	}()

	if cfg.Services.RTDSActivityEnabled {
		activityHandler := rtds.NewActivityHandler(redisClient, alphaHubService)
		activityClient = rtds.NewActivityClient(cfg, activityHandler)
		go func() {
			if err := activityClient.Connect(ctx); err != nil {
				logger.Error("❌ RTDS Activity Client failed: %v", err)
			}
		}()
	}
	go func() {
		if err := chainlinkClient.Connect(ctx); err != nil {
			logger.Error("❌ Chainlink RTDS Client failed: %v", err)
		}
	}()

	go watchStreamRequests(ctx, marketService, wsClient, cacheAllowlist)
	go alphaHubDailyLoop(ctx, alphaHubService, cfg.Services.AlphaSnapshotHour)

	if enableDBWrites {
		go persistMarketsLoop(ctx, marketService)
	}

	// 6. Subscription Loop
	// Periodically fetch "Active Markets" and subscribe to their tokens
	go func() {
		ticker := time.NewTicker(2 * time.Minute) // Refresh subscriptions every 2 mins
		defer ticker.Stop()

		first := true
		// Initial sync (also persists to Postgres)
		syncSubscriptions(ctx, marketService, wsClient, cfg, first)
		first = false

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncSubscriptions(ctx, marketService, wsClient, cfg, first)
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
	if activityClient != nil {
		if err := activityClient.Close(); err != nil {
			logger.Error("Error closing RTDS Activity WebSocket: %v", err)
		}
	}
	if err := chainlinkClient.Close(); err != nil {
		logger.Error("Error closing Chainlink RTDS WebSocket: %v", err)
	}

	time.Sleep(1 * time.Second) // Give connections time to close
	logger.Info("Worker exited.")
}

// syncSubscriptions fetches active markets and subscribes to their tokens.
// Optionally persists markets on the first run to avoid empty DB reads after restarts.
func syncSubscriptions(ctx context.Context, ms *services.MarketService, ws *rtds.Client, cfg *config.Config, persist bool) {
	logger.Info("🔄 Syncing market subscriptions...")

	// 1. Ensure our local DB has fresh data from Gamma
	// Sync active markets (cache only; no DB writes)
	if err := ms.SyncActiveMarkets(ctx); err != nil {
		logger.Error("Failed to sync active markets from Gamma: %v", err)
		return
	}

	// Optional: skip fresh drops to avoid DB upserts when writes are disabled
	if enableDBWrites {
		if err := ms.SyncFreshDrops(ctx); err != nil {
			logger.Error("Failed to sync fresh drops from Gamma: %v", err)
			// Don't return - continue with active markets even if fresh drops fail
		}
	}

	if persist && enableDBWrites {
		if err := ms.PersistActiveMarkets(ctx); err != nil {
			logger.Error("PersistActiveMarkets (initial) failed: %v", err)
		}
	}

	// 2. Get market assets
	marketAssets, err := resolveMarketAssets(ctx, ms, cfg)
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
	requestLimit := cfg.Services.MaxTrackedAssets * 2
	if requestLimit <= 0 {
		requestLimit = 4000
	}
	if requested, err := ms.PopRequestedStreamTokens(ctx, requestLimit); err != nil {
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

func watchStreamRequests(ctx context.Context, ms *services.MarketService, ws *rtds.Client, allowlist *rtds.CacheAllowlist) {
	sub := ms.SubscribeStreamRequests(ctx)
	defer sub.Close()

	for {
		msg, err := sub.ReceiveMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logger.Error("Stream request listener error: %v", err)
			continue
		}

		var payload services.StreamRequestPayload
		if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
			logger.Error("Invalid stream request payload: %v", err)
			continue
		}

		if len(payload.Tokens) == 0 {
			continue
		}

		if allowlist != nil {
			allowlist.Allow(payload.Tokens)
		}
		if err := ws.Subscribe(payload.Tokens); err != nil {
			logger.Error("Failed to subscribe to requested tokens: %v", err)
		}
	}
}

func persistMarketsLoop(ctx context.Context, ms *services.MarketService) {
	ticker := time.NewTicker(12 * time.Hour) // reduce DB writes to twice a day
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := ms.PersistActiveMarkets(ctx); err != nil {
				logger.Error("PersistActiveMarkets failed: %v", err)
			}
		}
	}
}

// resolveMarketAssets selects which markets to subscribe to for RTDS streams.
// If STREAM_RECENT_HOURS > 0, include all markets with non-zero 24h volume (approximating "seen in last window").
// Otherwise, include all active markets and apply STREAM_MAX_TRACKED_ASSETS only if set (>0).
func resolveMarketAssets(ctx context.Context, ms *services.MarketService, cfg *config.Config) ([]services.MarketAsset, error) {
	assets, err := ms.GetMarketAssets(ctx, 0)
	if err != nil {
		return nil, err
	}

	// Filter to recent-volume markets if configured
	if cfg.Services.StreamRecentHours > 0 {
		filtered := make([]services.MarketAsset, 0, len(assets))
		for _, a := range assets {
			if a.Volume24h > 0 {
				filtered = append(filtered, a)
			}
		}
		assets = filtered
	}

	// Apply optional cap
	maxAssets := cfg.Services.MaxTrackedAssets
	if maxAssets > 0 && len(assets) > maxAssets {
		assets = services.TrimMarketAssetsByLiquidity(assets, maxAssets)
	}
	return assets, nil
}

// alphaHubDailyLoop precomputes the /analysis snapshot once per day to keep the UI instant and avoid per-request LLM/Tavily calls.
func alphaHubDailyLoop(ctx context.Context, svc *services.AlphaHubService, snapshotHourUTC int) {
	if svc == nil {
		return
	}

	run := func(tag string) {
		if _, err := svc.GetDailySnapshot(ctx, 24*time.Hour, false); err != nil {
			logger.Error("AlphaHub daily snapshot (%s) failed: %v", tag, err)
		}
	}

	waitUntil := func(targetHour int) time.Duration {
		now := time.Now().UTC()
		if targetHour < 0 || targetHour > 23 {
			return 0
		}
		today := time.Date(now.Year(), now.Month(), now.Day(), targetHour, 0, 0, 0, time.UTC)
		if now.After(today) || now.Equal(today) {
			// schedule for next day
			next := today.Add(24 * time.Hour)
			return next.Sub(now)
		}
		return today.Sub(now)
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	// If snapshot already exists for today, don't rerun on deploy.
	hasSnapshot := svc.HasDailySnapshot(ctx, dateKey)

	if snapshotHourUTC < 0 {
		// Immediate run if missing; skip if already present to survive redeploys.
		if !hasSnapshot {
			logger.Info("AlphaHub: running daily snapshot now (no existing cache for %s)", dateKey)
			run("startup")
		} else {
			logger.Info("AlphaHub: snapshot already exists for %s, skipping startup run", dateKey)
		}
	} else {
		// Run immediately if we're past the target hour and no snapshot exists; otherwise wait until the target hour.
		now := time.Now().UTC()
		if now.Hour() >= snapshotHourUTC && !hasSnapshot {
			logger.Info("AlphaHub: running daily snapshot now (past target hour %d, no cache for %s)", snapshotHourUTC, dateKey)
			run("startup-after-hour")
		} else {
			delay := waitUntil(snapshotHourUTC)
			if delay > 0 {
				logger.Info("AlphaHub daily snapshot scheduled in %v (UTC hour=%d)", delay, snapshotHourUTC)
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
					run("scheduled-wait")
				}
			}
		}
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Info("AlphaHub: running scheduled daily snapshot")
			run("scheduled")
		}
	}
}

func enqueueRecurringJobs(ctx context.Context, queue *services.JobQueue, cfg *config.Config) {
	if queue == nil {
		return
	}

	tpslInterval := time.Duration(cfg.Services.TPSLCheckIntervalSeconds) * time.Second
	if tpslInterval <= 0 {
		tpslInterval = 15 * time.Second
	}
	marketInterval := time.Duration(cfg.Services.MarketReconcileIntervalSeconds) * time.Second
	if marketInterval <= 0 {
		marketInterval = 2 * time.Minute
	}
	bookInterval := time.Duration(cfg.Services.OrderbookReconcileIntervalSecs) * time.Second
	if bookInterval <= 0 {
		bookInterval = 45 * time.Second
	}
	cleanupInterval := time.Duration(cfg.Services.NotificationCleanupMinutes) * time.Minute
	if cleanupInterval <= 0 {
		cleanupInterval = 2 * time.Hour
	}

	enqueue := func(jobType services.JobType, payload interface{}) {
		// Keep backlog bounded; stale jobs are worse than dropped periodic ticks.
		if queue.QueueLength(ctx) > 20000 {
			return
		}
		raw, err := services.MarshalJobPayload(payload)
		if err != nil {
			return
		}
		_, _ = queue.Enqueue(ctx, services.Job{
			Type:        jobType,
			Payload:     raw,
			MaxAttempts: 3,
		})
	}

	enqueue(services.JobTypeTPSLEvaluate, map[string]int{"limit": 300})
	enqueue(services.JobTypeReconcileMarkets, nil)
	enqueue(services.JobTypeReconcileOrderBooks, map[string]int{"max_assets": 250})

	tpslTicker := time.NewTicker(tpslInterval)
	marketTicker := time.NewTicker(marketInterval)
	bookTicker := time.NewTicker(bookInterval)
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer tpslTicker.Stop()
	defer marketTicker.Stop()
	defer bookTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tpslTicker.C:
			enqueue(services.JobTypeTPSLEvaluate, map[string]int{"limit": 300})
		case <-marketTicker.C:
			enqueue(services.JobTypeReconcileMarkets, nil)
		case <-bookTicker.C:
			enqueue(services.JobTypeReconcileOrderBooks, map[string]int{"max_assets": 250})
		case <-cleanupTicker.C:
			enqueue(services.JobTypeCleanupNotifications, map[string]int{"retention_hours": 24 * 30})
		}
	}
}
