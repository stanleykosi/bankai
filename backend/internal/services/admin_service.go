package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	adminActionLogKey      = "moderation:actions"
	blockedUsersSetKey     = "moderation:user:blocked"
	blockedWalletsSetKey   = "moderation:wallet:blocked"
	adminActionLogMaxItems = 1000
	blockedUserKeyPrefix   = "moderation:user:block:"
	blockedWalletKeyPrefix = "moderation:wallet:block:"
)

type AdminService struct {
	db            *gorm.DB
	redis         *redis.Client
	notifications *NotificationService
}

type ModerationAction struct {
	ID          string                 `json:"id"`
	Action      string                 `json:"action"`
	ActorWallet string                 `json:"actor_wallet"`
	UserID      string                 `json:"user_id,omitempty"`
	Wallet      string                 `json:"wallet,omitempty"`
	Reason      string                 `json:"reason,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

type MarketModerationPatch struct {
	Restricted *bool `json:"restricted,omitempty"`
	Featured   *bool `json:"featured,omitempty"`
	Archived   *bool `json:"archived,omitempty"`
}

type BlockedAccount struct {
	UserID    string     `json:"user_id,omitempty"`
	Wallet    string     `json:"wallet,omitempty"`
	Blocked   bool       `json:"blocked"`
	Reason    string     `json:"reason,omitempty"`
	BlockedBy string     `json:"blocked_by,omitempty"`
	BlockedAt *time.Time `json:"blocked_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func NewAdminService(db *gorm.DB, rdb *redis.Client, notifications *NotificationService) *AdminService {
	return &AdminService{
		db:            db,
		redis:         rdb,
		notifications: notifications,
	}
}

func normalizeModerationUserID(userID string) string {
	return strings.ToLower(strings.TrimSpace(userID))
}

func (s *AdminService) BlockAccount(ctx context.Context, actorWallet, userID, wallet, reason string, duration time.Duration) error {
	if s == nil || s.redis == nil {
		return errors.New("admin service unavailable")
	}

	userID = normalizeModerationUserID(userID)
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	if userID == "" && wallet == "" {
		return errors.New("user_id or wallet is required")
	}

	payload := map[string]interface{}{
		"reason":     strings.TrimSpace(reason),
		"blocked_by": strings.ToLower(strings.TrimSpace(actorWallet)),
		"blocked_at": time.Now().UTC().Format(time.RFC3339),
	}
	if duration > 0 {
		payload["expires_at"] = time.Now().UTC().Add(duration).Format(time.RFC3339)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if userID != "" {
		key := blockedUserKeyPrefix + userID
		if duration > 0 {
			if err := s.redis.Set(ctx, key, data, duration).Err(); err != nil {
				return err
			}
		} else if err := s.redis.Set(ctx, key, data, 0).Err(); err != nil {
			return err
		}
		_ = s.redis.SAdd(ctx, blockedUsersSetKey, userID).Err()
	}
	if wallet != "" {
		key := blockedWalletKeyPrefix + wallet
		if duration > 0 {
			if err := s.redis.Set(ctx, key, data, duration).Err(); err != nil {
				return err
			}
		} else if err := s.redis.Set(ctx, key, data, 0).Err(); err != nil {
			return err
		}
		_ = s.redis.SAdd(ctx, blockedWalletsSetKey, wallet).Err()
	}

	return s.logAction(ctx, ModerationAction{
		ID:          uuid.NewString(),
		Action:      "BLOCK_ACCOUNT",
		ActorWallet: strings.ToLower(strings.TrimSpace(actorWallet)),
		UserID:      userID,
		Wallet:      wallet,
		Reason:      strings.TrimSpace(reason),
		CreatedAt:   time.Now().UTC(),
	})
}

func (s *AdminService) UnblockAccount(ctx context.Context, actorWallet, userID, wallet string) error {
	if s == nil || s.redis == nil {
		return errors.New("admin service unavailable")
	}
	userID = normalizeModerationUserID(userID)
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	if userID == "" && wallet == "" {
		return errors.New("user_id or wallet is required")
	}

	if userID != "" {
		_ = s.redis.Del(ctx, blockedUserKeyPrefix+userID).Err()
		_ = s.redis.SRem(ctx, blockedUsersSetKey, userID).Err()
	}
	if wallet != "" {
		_ = s.redis.Del(ctx, blockedWalletKeyPrefix+wallet).Err()
		_ = s.redis.SRem(ctx, blockedWalletsSetKey, wallet).Err()
	}

	return s.logAction(ctx, ModerationAction{
		ID:          uuid.NewString(),
		Action:      "UNBLOCK_ACCOUNT",
		ActorWallet: strings.ToLower(strings.TrimSpace(actorWallet)),
		UserID:      userID,
		Wallet:      wallet,
		CreatedAt:   time.Now().UTC(),
	})
}

func (s *AdminService) ListBlockedAccounts(ctx context.Context) ([]BlockedAccount, error) {
	if s == nil || s.redis == nil {
		return nil, errors.New("admin service unavailable")
	}

	users, err := s.redis.SMembers(ctx, blockedUsersSetKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	wallets, err := s.redis.SMembers(ctx, blockedWalletsSetKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	out := make([]BlockedAccount, 0, len(users)+len(wallets))
	for _, userID := range users {
		key := blockedUserKeyPrefix + userID
		exists, existsErr := s.redis.Exists(ctx, key).Result()
		if existsErr != nil {
			return nil, existsErr
		}
		if exists == 0 {
			_ = s.redis.SRem(ctx, blockedUsersSetKey, userID).Err()
			continue
		}
		acc := BlockedAccount{UserID: userID, Blocked: true}
		s.populateBlockMetadata(ctx, key, &acc)
		out = append(out, acc)
	}
	for _, wallet := range wallets {
		key := blockedWalletKeyPrefix + wallet
		exists, existsErr := s.redis.Exists(ctx, key).Result()
		if existsErr != nil {
			return nil, existsErr
		}
		if exists == 0 {
			_ = s.redis.SRem(ctx, blockedWalletsSetKey, wallet).Err()
			continue
		}
		acc := BlockedAccount{Wallet: wallet, Blocked: true}
		s.populateBlockMetadata(ctx, key, &acc)
		out = append(out, acc)
	}
	return out, nil
}

func (s *AdminService) UpdateMarketModeration(ctx context.Context, conditionID string, patch MarketModerationPatch, actorWallet string) error {
	if s == nil || s.db == nil {
		return errors.New("admin service unavailable")
	}
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return errors.New("condition_id is required")
	}

	updates := map[string]interface{}{}
	meta := map[string]interface{}{}
	if patch.Restricted != nil {
		updates["restricted"] = *patch.Restricted
		meta["restricted"] = *patch.Restricted
	}
	if patch.Featured != nil {
		updates["featured"] = *patch.Featured
		meta["featured"] = *patch.Featured
	}
	if patch.Archived != nil {
		updates["archived"] = *patch.Archived
		meta["archived"] = *patch.Archived
	}
	if len(updates) == 0 {
		return errors.New("at least one patch field is required")
	}

	result := s.db.WithContext(ctx).Model(&models.Market{}).
		Where("condition_id = ?", conditionID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("market %s not found", conditionID)
	}

	return s.logAction(ctx, ModerationAction{
		ID:          uuid.NewString(),
		Action:      "MODERATE_MARKET",
		ActorWallet: strings.ToLower(strings.TrimSpace(actorWallet)),
		Metadata: map[string]interface{}{
			"condition_id": conditionID,
			"patch":        meta,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func (s *AdminService) BroadcastSystemNotification(ctx context.Context, actorWallet, title, message string, payload map[string]interface{}) (int, error) {
	if s == nil || s.db == nil || s.notifications == nil {
		return 0, errors.New("notification pipeline unavailable")
	}
	title = strings.TrimSpace(title)
	message = strings.TrimSpace(message)
	if title == "" || message == "" {
		return 0, errors.New("title and message are required")
	}

	const batchSize = 1000
	total := 0
	lastID := ""
	for {
		query := s.db.WithContext(ctx).Model(&models.User{}).Select("id").Order("id ASC").Limit(batchSize)
		if lastID != "" {
			query = query.Where("id > ?", lastID)
		}

		var users []models.User
		if err := query.Find(&users).Error; err != nil {
			return total, err
		}
		if len(users) == 0 {
			break
		}

		userIDs := make([]uuid.UUID, 0, len(users))
		for _, user := range users {
			userIDs = append(userIDs, user.ID)
			lastID = user.ID.String()
		}

		inserted, err := s.notifications.CreateSystemNotifications(ctx, userIDs, title, message, payload)
		if err != nil {
			return total, err
		}
		total += inserted

		if len(users) < batchSize {
			break
		}
	}

	_ = s.logAction(ctx, ModerationAction{
		ID:          uuid.NewString(),
		Action:      "BROADCAST_NOTIFICATION",
		ActorWallet: strings.ToLower(strings.TrimSpace(actorWallet)),
		Metadata: map[string]interface{}{
			"title":     title,
			"message":   message,
			"delivered": total,
		},
		CreatedAt: time.Now().UTC(),
	})

	return total, nil
}

func (s *AdminService) GetActionLog(ctx context.Context, limit int64) ([]ModerationAction, error) {
	if s == nil || s.redis == nil {
		return nil, errors.New("admin service unavailable")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	values, err := s.redis.LRange(ctx, adminActionLogKey, 0, limit-1).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []ModerationAction{}, nil
		}
		return nil, err
	}

	actions := make([]ModerationAction, 0, len(values))
	for _, value := range values {
		var action ModerationAction
		if err := json.Unmarshal([]byte(value), &action); err == nil {
			actions = append(actions, action)
		}
	}
	return actions, nil
}

func (s *AdminService) populateBlockMetadata(ctx context.Context, key string, acc *BlockedAccount) {
	raw, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return
	}
	var payload struct {
		Reason    string `json:"reason"`
		BlockedBy string `json:"blocked_by"`
		BlockedAt string `json:"blocked_at"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return
	}
	acc.Reason = payload.Reason
	acc.BlockedBy = payload.BlockedBy
	if payload.BlockedAt != "" {
		if ts, err := time.Parse(time.RFC3339, payload.BlockedAt); err == nil {
			acc.BlockedAt = &ts
		}
	}
	if payload.ExpiresAt != "" {
		if ts, err := time.Parse(time.RFC3339, payload.ExpiresAt); err == nil {
			acc.ExpiresAt = &ts
		}
	}
}

func (s *AdminService) logAction(ctx context.Context, action ModerationAction) error {
	if s == nil || s.redis == nil {
		return nil
	}
	data, err := json.Marshal(action)
	if err != nil {
		return err
	}
	pipe := s.redis.Pipeline()
	pipe.LPush(ctx, adminActionLogKey, data)
	pipe.LTrim(ctx, adminActionLogKey, 0, adminActionLogMaxItems-1)
	_, err = pipe.Exec(ctx)
	return err
}
