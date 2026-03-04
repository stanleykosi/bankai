package services

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAdminServiceNormalizesUserIDForBlockAndUnblock(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewAdminService(nil, rdb, nil)
	ctx := context.Background()

	upperUserID := "A123E456-E89B-12D3-A456-426614174000"
	lowerUserID := "a123e456-e89b-12d3-a456-426614174000"

	if err := svc.BlockAccount(ctx, "0xadmin", upperUserID, "", "test", time.Minute); err != nil {
		t.Fatalf("BlockAccount failed: %v", err)
	}

	if exists := rdb.Exists(ctx, blockedUserKeyPrefix+upperUserID).Val(); exists != 0 {
		t.Fatalf("expected original-case key to be absent")
	}
	if exists := rdb.Exists(ctx, blockedUserKeyPrefix+lowerUserID).Val(); exists == 0 {
		t.Fatalf("expected normalized lowercase block key to exist")
	}

	if err := svc.UnblockAccount(ctx, "0xadmin", lowerUserID, ""); err != nil {
		t.Fatalf("UnblockAccount failed: %v", err)
	}
	if exists := rdb.Exists(ctx, blockedUserKeyPrefix+lowerUserID).Val(); exists != 0 {
		t.Fatalf("expected lowercase key to be removed after unblock")
	}
}
