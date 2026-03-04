package services

import (
	"context"
	"encoding/json"
	"time"
)

type JobProcessor struct {
	Market        *MarketService
	TPSL          *TPSLService
	Notifications *NotificationService
	Admin         *AdminService
}

func NewJobProcessor(market *MarketService, tpsl *TPSLService, notifications *NotificationService, admin *AdminService) *JobProcessor {
	return &JobProcessor{
		Market:        market,
		TPSL:          tpsl,
		Notifications: notifications,
		Admin:         admin,
	}
}

func (p *JobProcessor) Process(ctx context.Context, job Job) error {
	switch job.Type {
	case JobTypeReconcileMarkets:
		if p.Market == nil {
			return nil
		}
		return p.Market.SyncActiveMarkets(ctx)
	case JobTypeReconcileOrderBooks:
		if p.Market == nil {
			return nil
		}
		var payload struct {
			MaxAssets int `json:"max_assets"`
		}
		_ = json.Unmarshal(job.Payload, &payload)
		return p.Market.ReconcileOrderBooks(ctx, payload.MaxAssets)
	case JobTypeTPSLEvaluate:
		if p.TPSL == nil {
			return nil
		}
		var payload struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(job.Payload, &payload)
		_, err := p.TPSL.EvaluateDueRules(ctx, payload.Limit)
		return err
	case JobTypeCleanupNotifications:
		if p.Notifications == nil {
			return nil
		}
		var payload struct {
			RetentionHours int `json:"retention_hours"`
		}
		_ = json.Unmarshal(job.Payload, &payload)
		retention := 24 * 30 * time.Hour
		if payload.RetentionHours > 0 {
			retention = time.Duration(payload.RetentionHours) * time.Hour
		}
		return p.Notifications.DeleteOldNotifications(ctx, retention)
	case JobTypeBroadcastNotification:
		if p.Admin == nil {
			return nil
		}
		var payload struct {
			ActorWallet string                 `json:"actor_wallet"`
			Title       string                 `json:"title"`
			Message     string                 `json:"message"`
			Data        map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return err
		}
		_, err := p.Admin.BroadcastSystemNotification(ctx, payload.ActorWallet, payload.Title, payload.Message, payload.Data)
		return err
	default:
		return nil
	}
}
