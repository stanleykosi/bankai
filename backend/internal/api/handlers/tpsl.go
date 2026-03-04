package handlers

import (
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type TPSLHandler struct {
	service *services.TPSLService
}

func NewTPSLHandler(service *services.TPSLService) *TPSLHandler {
	return &TPSLHandler{service: service}
}

type CreateTPSLRuleRequest struct {
	MarketID    string  `json:"market_id"`
	TokenID     string  `json:"token_id"`
	Side        string  `json:"side"`
	TriggerType string  `json:"trigger_type"`
	TargetPrice float64 `json:"target_price"`
	Size        float64 `json:"size"`
	ExpiresAt   string  `json:"expires_at,omitempty"`
}

// CreateRule registers a TP/SL trigger rule.
// POST /api/v1/trade/triggers
func (h *TPSLHandler) CreateRule(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "tp/sl service unavailable"})
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	wallet, _ := middleware.GetWalletAddress(c)

	var req CreateTPSLRuleRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	var expiresAt *time.Time
	if strings.TrimSpace(req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ExpiresAt))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "expires_at must be RFC3339"})
		}
		expiresAt = &parsed
	}

	rule, err := h.service.CreateRule(c.Context(), services.CreateTPSLRuleInput{
		UserID:        userID,
		WalletAddress: wallet,
		MarketID:      req.MarketID,
		TokenID:       req.TokenID,
		Side:          req.Side,
		TriggerType:   req.TriggerType,
		TargetPrice:   req.TargetPrice,
		Size:          req.Size,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"rule": rule})
}

// ListRules lists TP/SL rules for the authenticated user.
// GET /api/v1/trade/triggers
func (h *TPSLHandler) ListRules(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "tp/sl service unavailable"})
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	status := c.Query("status")

	rules, err := h.service.ListRules(c.Context(), userID, status)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"rules": rules,
		"count": len(rules),
	})
}

// CancelRule deactivates a TP/SL rule.
// DELETE /api/v1/trade/triggers/:id
func (h *TPSLHandler) CancelRule(c *fiber.Ctx) error {
	if h == nil || h.service == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "tp/sl service unavailable"})
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	ruleID := c.Params("id")
	rule, err := h.service.CancelRule(c.Context(), userID, ruleID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"rule": rule})
}
