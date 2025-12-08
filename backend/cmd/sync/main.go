package main

import (
	"context"
	"log"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/db"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/bankai-project/backend/internal/services"
	"github.com/redis/go-redis/v9"
)

func main() {
	log.Println("🚀 Starting manual market sync from Gamma...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	pgDB, err := db.ConnectPostgres(cfg)
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		log.Fatalf("failed to start in-memory redis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	gammaClient := gamma.NewClient(cfg)
	service := services.NewMarketService(pgDB, redisClient, gammaClient, nil)

	ctx := context.Background()

	if err := service.SyncActiveMarkets(ctx); err != nil {
		log.Fatalf("active market sync failed: %v", err)
	}

	if err := service.SyncFreshDrops(ctx); err != nil {
		log.Printf("fresh drops sync failed: %v", err)
	}

	var activeCount int64
	if err := pgDB.Model(&models.Market{}).Where("active = ?", true).Count(&activeCount).Error; err == nil {
		log.Printf("✅ Active markets stored in Postgres: %d", activeCount)
	} else {
		log.Printf("⚠️ Failed to count active markets: %v", err)
	}

	log.Println("✅ Manual market sync completed successfully.")
}
