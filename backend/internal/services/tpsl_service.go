package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	tpslRuleKeyPrefix = "tpsl:rule:"
	tpslUserSetPrefix = "tpsl:user:"
	tpslDueZSetKey    = "tpsl:due"
	tpslLockPrefix    = "tpsl:lock:"
)

var ErrNoValidTPSLPrice = errors.New("no valid tp/sl price available")

type TPSLTriggerType string
type TPSLRuleStatus string

const (
	TriggerTakeProfit TPSLTriggerType = "TAKE_PROFIT"
	TriggerStopLoss   TPSLTriggerType = "STOP_LOSS"

	TPSLStatusActive    TPSLRuleStatus = "ACTIVE"
	TPSLStatusTriggered TPSLRuleStatus = "TRIGGERED"
	TPSLStatusCancelled TPSLRuleStatus = "CANCELLED"
	TPSLStatusExpired   TPSLRuleStatus = "EXPIRED"
	TPSLStatusFailed    TPSLRuleStatus = "FAILED"
)

type TPSLRule struct {
	ID            string          `json:"id"`
	UserID        string          `json:"user_id"`
	WalletAddress string          `json:"wallet_address"`
	MarketID      string          `json:"market_id"`
	TokenID       string          `json:"token_id"`
	Side          string          `json:"side"`
	TriggerType   TPSLTriggerType `json:"trigger_type"`
	TargetPrice   float64         `json:"target_price"`
	Size          float64         `json:"size"`
	Status        TPSLRuleStatus  `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	NextCheckAt   time.Time       `json:"next_check_at"`
	ExpiresAt     *time.Time      `json:"expires_at,omitempty"`
	TriggeredAt   *time.Time      `json:"triggered_at,omitempty"`
	LastPrice     float64         `json:"last_price,omitempty"`
	LastError     string          `json:"last_error,omitempty"`
}

type CreateTPSLRuleInput struct {
	UserID        string
	WalletAddress string
	MarketID      string
	TokenID       string
	Side          string
	TriggerType   string
	TargetPrice   float64
	Size          float64
	ExpiresAt     *time.Time
}

type TPSLService struct {
	redis         *redis.Client
	clob          *clob.Client
	notifications *NotificationService
}

func NewTPSLService(rdb *redis.Client, clobClient *clob.Client, notifications *NotificationService) *TPSLService {
	return &TPSLService{
		redis:         rdb,
		clob:          clobClient,
		notifications: notifications,
	}
}

func (s *TPSLService) CreateRule(ctx context.Context, input CreateTPSLRuleInput) (*TPSLRule, error) {
	if s == nil || s.redis == nil {
		return nil, errors.New("tp/sl service unavailable")
	}

	userID := strings.TrimSpace(input.UserID)
	wallet := strings.ToLower(strings.TrimSpace(input.WalletAddress))
	marketID := strings.TrimSpace(input.MarketID)
	tokenID := strings.TrimSpace(input.TokenID)
	if userID == "" || wallet == "" || marketID == "" || tokenID == "" {
		return nil, errors.New("user_id, wallet_address, market_id, and token_id are required")
	}

	side := strings.ToUpper(strings.TrimSpace(input.Side))
	if side != "BUY" && side != "SELL" {
		return nil, errors.New("side must be BUY or SELL")
	}

	triggerType := strings.ToUpper(strings.TrimSpace(input.TriggerType))
	if triggerType != string(TriggerTakeProfit) && triggerType != string(TriggerStopLoss) {
		return nil, errors.New("trigger_type must be TAKE_PROFIT or STOP_LOSS")
	}

	if input.TargetPrice <= 0 {
		return nil, errors.New("target_price must be greater than zero")
	}
	if input.Size <= 0 {
		return nil, errors.New("size must be greater than zero")
	}

	now := time.Now().UTC()
	rule := &TPSLRule{
		ID:            uuid.NewString(),
		UserID:        userID,
		WalletAddress: wallet,
		MarketID:      marketID,
		TokenID:       tokenID,
		Side:          side,
		TriggerType:   TPSLTriggerType(triggerType),
		TargetPrice:   input.TargetPrice,
		Size:          input.Size,
		Status:        TPSLStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
		NextCheckAt:   now,
		ExpiresAt:     input.ExpiresAt,
	}

	if err := s.saveRule(ctx, rule); err != nil {
		return nil, err
	}
	if err := s.redis.SAdd(ctx, tpslUserSetPrefix+userID, rule.ID).Err(); err != nil {
		return nil, err
	}
	if err := s.redis.ZAdd(ctx, tpslDueZSetKey, redis.Z{
		Score:  float64(rule.NextCheckAt.Unix()),
		Member: rule.ID,
	}).Err(); err != nil {
		return nil, err
	}

	return rule, nil
}

func (s *TPSLService) ListRules(ctx context.Context, userID string, status string) ([]TPSLRule, error) {
	if s == nil || s.redis == nil {
		return nil, errors.New("tp/sl service unavailable")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("user id is required")
	}

	ids, err := s.redis.SMembers(ctx, tpslUserSetPrefix+userID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []TPSLRule{}, nil
		}
		return nil, err
	}
	if len(ids) == 0 {
		return []TPSLRule{}, nil
	}

	filterStatus := strings.ToUpper(strings.TrimSpace(status))
	items := make([]TPSLRule, 0, len(ids))
	for _, id := range ids {
		rule, err := s.getRuleByID(ctx, id)
		if err != nil || rule == nil {
			continue
		}
		if filterStatus != "" && string(rule.Status) != filterStatus {
			continue
		}
		items = append(items, *rule)
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *TPSLService) CancelRule(ctx context.Context, userID, ruleID string) (*TPSLRule, error) {
	rule, err := s.getRuleByID(ctx, strings.TrimSpace(ruleID))
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, errors.New("rule not found")
	}
	if rule.UserID != strings.TrimSpace(userID) {
		return nil, errors.New("rule does not belong to user")
	}
	if rule.Status != TPSLStatusActive {
		return nil, errors.New("rule is not active")
	}

	rule.Status = TPSLStatusCancelled
	rule.UpdatedAt = time.Now().UTC()
	if err := s.saveRule(ctx, rule); err != nil {
		return nil, err
	}
	_ = s.redis.ZRem(ctx, tpslDueZSetKey, rule.ID).Err()
	return rule, nil
}

func (s *TPSLService) EvaluateDueRules(ctx context.Context, limit int) (int, error) {
	if s == nil || s.redis == nil || s.clob == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 200
	}

	now := time.Now().UTC()
	ids, err := s.redis.ZRangeByScore(ctx, tpslDueZSetKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatInt(now.Unix(), 10),
		Count: int64(limit),
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, err
	}

	processed := 0
	for _, id := range ids {
		lockKey := tpslLockPrefix + id
		locked, err := s.redis.SetNX(ctx, lockKey, "1", 20*time.Second).Result()
		if err != nil || !locked {
			continue
		}

		func() {
			defer s.redis.Del(ctx, lockKey)
			rule, err := s.getRuleByID(ctx, id)
			if err != nil || rule == nil {
				_ = s.redis.ZRem(ctx, tpslDueZSetKey, id).Err()
				return
			}
			if rule.Status != TPSLStatusActive {
				_ = s.redis.ZRem(ctx, tpslDueZSetKey, id).Err()
				return
			}
			if rule.ExpiresAt != nil && now.After(rule.ExpiresAt.UTC()) {
				rule.Status = TPSLStatusExpired
				rule.UpdatedAt = now
				_ = s.saveRule(ctx, rule)
				_ = s.redis.ZRem(ctx, tpslDueZSetKey, id).Err()
				processed++
				return
			}

			price, fetchErr := s.fetchCurrentPrice(ctx, rule.TokenID)
			if fetchErr != nil {
				rule.LastError = fetchErr.Error()
				rule.LastPrice = 0
				rule.UpdatedAt = now
				rule.NextCheckAt = now.Add(20 * time.Second)
				_ = s.saveRule(ctx, rule)
				_ = s.redis.ZAdd(ctx, tpslDueZSetKey, redis.Z{
					Score:  float64(rule.NextCheckAt.Unix()),
					Member: rule.ID,
				}).Err()
				processed++
				return
			}

			rule.LastError = ""
			rule.LastPrice = price
			rule.UpdatedAt = now
			if isTriggered(rule, price) {
				rule.Status = TPSLStatusTriggered
				triggeredAt := now
				rule.TriggeredAt = &triggeredAt
				if err := s.saveRule(ctx, rule); err == nil {
					_ = s.redis.ZRem(ctx, tpslDueZSetKey, id).Err()
					s.notifyTriggered(ctx, rule, price)
				} else {
					// Persistence failed, so do not emit trigger notifications yet.
					_ = s.redis.ZAdd(ctx, tpslDueZSetKey, redis.Z{
						Score:  float64(now.Add(30 * time.Second).Unix()),
						Member: rule.ID,
					}).Err()
				}
			} else {
				rule.NextCheckAt = now.Add(30 * time.Second)
				if err := s.saveRule(ctx, rule); err == nil {
					_ = s.redis.ZAdd(ctx, tpslDueZSetKey, redis.Z{
						Score:  float64(rule.NextCheckAt.Unix()),
						Member: rule.ID,
					}).Err()
				}
			}
			processed++
		}()
	}

	return processed, nil
}

func (s *TPSLService) fetchCurrentPrice(ctx context.Context, tokenID string) (float64, error) {
	price, _, err := s.clob.GetLastTradePrice(ctx, tokenID)
	if err == nil && price > 0 {
		return price, nil
	}

	price, midErr := s.clob.GetMidpoint(ctx, tokenID)
	if midErr != nil {
		if err != nil {
			return 0, err
		}
		return 0, midErr
	}
	if price <= 0 {
		if err != nil {
			return 0, err
		}
		return 0, ErrNoValidTPSLPrice
	}
	return price, nil
}

func isTriggered(rule *TPSLRule, price float64) bool {
	if rule == nil || price <= 0 {
		return false
	}

	switch rule.TriggerType {
	case TriggerTakeProfit:
		if rule.Side == "BUY" {
			return price >= rule.TargetPrice
		}
		return price <= rule.TargetPrice
	case TriggerStopLoss:
		if rule.Side == "BUY" {
			return price <= rule.TargetPrice
		}
		return price >= rule.TargetPrice
	default:
		return false
	}
}

func (s *TPSLService) saveRule(ctx context.Context, rule *TPSLRule) error {
	if rule == nil {
		return errors.New("rule is required")
	}
	data, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, tpslRuleKeyPrefix+rule.ID, data, 0).Err()
}

func (s *TPSLService) getRuleByID(ctx context.Context, id string) (*TPSLRule, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("rule id is required")
	}
	raw, err := s.redis.Get(ctx, tpslRuleKeyPrefix+strings.TrimSpace(id)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}

	var rule TPSLRule
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *TPSLService) notifyTriggered(ctx context.Context, rule *TPSLRule, currentPrice float64) {
	if s.notifications == nil || rule == nil {
		return
	}
	userID, err := uuid.Parse(rule.UserID)
	if err != nil {
		return
	}

	payload := map[string]interface{}{
		"rule_id":       rule.ID,
		"market_id":     rule.MarketID,
		"token_id":      rule.TokenID,
		"trigger_type":  rule.TriggerType,
		"target_price":  rule.TargetPrice,
		"current_price": currentPrice,
		"side":          rule.Side,
		"size":          rule.Size,
	}
	title := fmt.Sprintf("%s Triggered", strings.ReplaceAll(string(rule.TriggerType), "_", " "))
	message := fmt.Sprintf("Rule %s triggered at %.4f (target %.4f).", rule.ID, currentPrice, rule.TargetPrice)
	_ = s.notifications.CreateSystemNotification(ctx, userID, title, message, payload)
}
