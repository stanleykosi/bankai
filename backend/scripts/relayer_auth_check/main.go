package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/polymarket/relayer"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := relayer.NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	err = client.CheckAuth(ctx)
	if err != nil {
		log.Fatalf("Relayer auth check failed: %v", err)
	}

	fmt.Println("Relayer auth check succeeded (payload rejected past authentication).")
}

