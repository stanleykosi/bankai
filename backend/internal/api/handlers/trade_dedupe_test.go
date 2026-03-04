package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/models"
	"github.com/redis/go-redis/v9"
)

func TestTradeAlertDedupeLifecycle(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	h := &TradeHandler{Redis: rdb}
	ctx := context.Background()

	reserved, err := h.reserveOrderAlert(ctx, "user-1", "order-1")
	if err != nil {
		t.Fatalf("reserveOrderAlert failed: %v", err)
	}
	if !reserved {
		t.Fatalf("expected initial reserve to succeed")
	}

	reserved, err = h.reserveOrderAlert(ctx, "user-1", "order-1")
	if err != nil {
		t.Fatalf("second reserveOrderAlert failed: %v", err)
	}
	if reserved {
		t.Fatalf("expected second reserve to be rejected while pending")
	}

	pendingTTL := rdb.TTL(ctx, tradeAlertSeenKey("user-1", "order-1")).Val()
	if pendingTTL <= 0 || pendingTTL > 5*time.Minute {
		t.Fatalf("expected short pending TTL, got %s", pendingTTL)
	}

	h.releaseOrderAlert(ctx, "user-1", "order-1")
	if exists := rdb.Exists(ctx, tradeAlertSeenKey("user-1", "order-1")).Val(); exists != 0 {
		t.Fatalf("expected pending key to be deleted after release")
	}

	reserved, err = h.reserveOrderAlert(ctx, "user-1", "order-1")
	if err != nil {
		t.Fatalf("reserve after release failed: %v", err)
	}
	if !reserved {
		t.Fatalf("expected reserve after release to succeed")
	}

	if err := h.confirmOrderAlert(ctx, "user-1", "order-1"); err != nil {
		t.Fatalf("confirmOrderAlert failed: %v", err)
	}

	reserved, err = h.reserveOrderAlert(ctx, "user-1", "order-1")
	if err != nil {
		t.Fatalf("reserve after confirm failed: %v", err)
	}
	if reserved {
		t.Fatalf("expected reserve after confirm to be rejected")
	}

	confirmedTTL := rdb.TTL(ctx, tradeAlertSeenKey("user-1", "order-1")).Val()
	if confirmedTTL < 24*time.Hour {
		t.Fatalf("expected long confirmed TTL, got %s", confirmedTTL)
	}
}

func TestReserveOrderAlertFailsOpenWithoutRedis(t *testing.T) {
	h := &TradeHandler{}
	reserved, err := h.reserveOrderAlert(context.Background(), "user-1", "order-1")
	if err != nil {
		t.Fatalf("expected reserveOrderAlert to fail-open when redis is unavailable: %v", err)
	}
	if !reserved {
		t.Fatalf("expected reserveOrderAlert to continue when redis is unavailable")
	}
}

func TestNormalizeSyncedOrderMakersRewritesMismatchedAddress(t *testing.T) {
	user := &models.User{
		EOAAddress:   "0x1111111111111111111111111111111111111111",
		VaultAddress: "0x2222222222222222222222222222222222222222",
	}
	orders := []SyncedOrder{
		{OrderID: "o-1", MakerAddress: "0x3333333333333333333333333333333333333333"},
	}

	normalized := normalizeSyncedOrderMakers(user, orders)
	if len(normalized) != 1 {
		t.Fatalf("expected normalized orders to preserve item count")
	}
	if got := normalized[0].MakerAddress; got != strings.ToLower(user.VaultAddress) {
		t.Fatalf("expected mismatched makerAddress to be rewritten to preferred wallet, got %q", got)
	}
}

func TestNormalizeSyncedOrderMakersKeepsOwnedAndEmptyAddress(t *testing.T) {
	user := &models.User{
		EOAAddress:   "0x1111111111111111111111111111111111111111",
		VaultAddress: "0x2222222222222222222222222222222222222222",
	}
	orders := []SyncedOrder{
		{OrderID: "o-1", MakerAddress: ""},
		{OrderID: "o-2", MakerAddress: "0x1111111111111111111111111111111111111111"},
		{OrderID: "o-3", MakerAddress: "0x2222222222222222222222222222222222222222"},
	}

	normalized := normalizeSyncedOrderMakers(user, orders)
	if len(normalized) != len(orders) {
		t.Fatalf("expected normalized orders to preserve item count")
	}
	if got := normalized[0].MakerAddress; got != strings.ToLower(user.VaultAddress) {
		t.Fatalf("expected empty makerAddress to default to preferred wallet, got %q", got)
	}
	if got := normalized[1].MakerAddress; got != strings.ToLower(user.EOAAddress) {
		t.Fatalf("expected owned eoa makerAddress to be preserved, got %q", got)
	}
	if got := normalized[2].MakerAddress; got != strings.ToLower(user.VaultAddress) {
		t.Fatalf("expected owned vault makerAddress to be preserved, got %q", got)
	}
}
