package main

import (
	"context"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/db"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/bankai-project/backend/internal/services"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger.Info("starting manual market sync from gamma")

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config: %v", err)
	}

	pgDB, err := db.ConnectPostgres(cfg)
	if err != nil {
		logger.Fatal("failed to connect to postgres: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		logger.Fatal("failed to start in-memory redis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	gammaClient := gamma.NewClient(cfg)
	service := services.NewMarketService(pgDB, redisClient, gammaClient, nil)

	ctx := context.Background()

	if err := service.SyncActiveMarkets(ctx); err != nil {
		logger.Fatal("active market sync failed: %v", err)
	}

	if err := service.SyncFreshDrops(ctx); err != nil {
		logger.Warn("fresh drops sync failed: %v", err)
	}

	var activeCount int64
	if err := pgDB.Model(&models.Market{}).Where("active = ?", true).Count(&activeCount).Error; err == nil {
		logger.Info("active markets stored in postgres: %d", activeCount)
	} else {
		logger.Warn("failed to count active markets: %v", err)
	}

	logger.Info("manual market sync completed successfully")
}
