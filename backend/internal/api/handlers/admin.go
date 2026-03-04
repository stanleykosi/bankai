package handlers

import (
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type AdminHandler struct {
	service *services.AdminService
	jobs    *services.JobQueue
}

func NewAdminHandler(service *services.AdminService, jobs *services.JobQueue) *AdminHandler {
	return &AdminHandler{
		service: service,
		jobs:    jobs,
	}
}

type BlockAccountRequest struct {
	UserID          string `json:"user_id,omitempty"`
	Wallet          string `json:"wallet,omitempty"`
	Reason          string `json:"reason,omitempty"`
	DurationMinutes int    `json:"duration_minutes,omitempty"`
}

func (h *AdminHandler) BlockAccount(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "admin service unavailable"})
	}
	actor, _ := middleware.GetWalletAddress(c)

	var req BlockAccountRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	var duration time.Duration
	if req.DurationMinutes > 0 {
		duration = time.Duration(req.DurationMinutes) * time.Minute
	}
	if err := h.service.BlockAccount(c.Context(), actor, req.UserID, req.Wallet, req.Reason, duration); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

type UnblockAccountRequest struct {
	UserID string `json:"user_id,omitempty"`
	Wallet string `json:"wallet,omitempty"`
}

func (h *AdminHandler) UnblockAccount(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "admin service unavailable"})
	}
	actor, _ := middleware.GetWalletAddress(c)

	var req UnblockAccountRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := h.service.UnblockAccount(c.Context(), actor, req.UserID, req.Wallet); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *AdminHandler) ListBlockedAccounts(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "admin service unavailable"})
	}
	accounts, err := h.service.ListBlockedAccounts(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"accounts": accounts,
		"count":    len(accounts),
	})
}

type ModerateMarketRequest struct {
	Restricted *bool `json:"restricted,omitempty"`
	Featured   *bool `json:"featured,omitempty"`
	Archived   *bool `json:"archived,omitempty"`
}

func (h *AdminHandler) ModerateMarket(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "admin service unavailable"})
	}

	conditionID := strings.TrimSpace(c.Params("condition_id"))
	if conditionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "condition_id is required"})
	}

	actor, _ := middleware.GetWalletAddress(c)
	var req ModerateMarketRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	err := h.service.UpdateMarketModeration(c.Context(), conditionID, services.MarketModerationPatch{
		Restricted: req.Restricted,
		Featured:   req.Featured,
		Archived:   req.Archived,
	}, actor)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "condition_id": conditionID})
}

type BroadcastRequest struct {
	Title   string                 `json:"title"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
	Async   bool                   `json:"async"`
}

func (h *AdminHandler) Broadcast(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "admin service unavailable"})
	}
	actor, _ := middleware.GetWalletAddress(c)

	var req BroadcastRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	title := strings.TrimSpace(req.Title)
	message := strings.TrimSpace(req.Message)
	if title == "" || message == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "title and message are required"})
	}

	if req.Async && h.jobs != nil {
		payload, err := services.MarshalJobPayload(map[string]interface{}{
			"actor_wallet": actor,
			"title":        title,
			"message":      message,
			"data":         req.Data,
		})
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		jobID, err := h.jobs.Enqueue(c.Context(), services.Job{
			Type:        services.JobTypeBroadcastNotification,
			Payload:     payload,
			MaxAttempts: 5,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"queued": true,
			"job_id": jobID,
		})
	}

	count, err := h.service.BroadcastSystemNotification(c.Context(), actor, title, message, req.Data)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"queued":    false,
		"delivered": count,
	})
}

func (h *AdminHandler) ActionLog(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "admin service unavailable"})
	}

	limit := int64(c.QueryInt("limit", 100))
	actions, err := h.service.GetActionLog(c.Context(), limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"actions": actions,
		"count":   len(actions),
	})
}
