/**
 * @description
 * Social API Handlers.
 * Handles follow/unfollow operations and notifications.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 * - backend/internal/api/middleware
 */

package handlers

import (
	"strconv"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SocialHandler handles social-related requests
type SocialHandler struct {
	db                  *gorm.DB
	socialService       *services.SocialService
	notificationService *services.NotificationService
}

// NewSocialHandler creates a new SocialHandler
func NewSocialHandler(db *gorm.DB, socialService *services.SocialService, notificationService *services.NotificationService) *SocialHandler {
	return &SocialHandler{
		db:                  db,
		socialService:       socialService,
		notificationService: notificationService,
	}
}

// FollowRequest represents a follow request body
type FollowRequest struct {
	TargetAddress string `json:"target_address"`
}

// FollowTrader follows a trader
// POST /api/v1/social/follow
func (h *SocialHandler) FollowTrader(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	// Get user ID from clerk ID
	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	var req FollowRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.TargetAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Target address is required",
		})
	}

	err = h.socialService.FollowTrader(c.Context(), user.ID, req.TargetAddress)
	if err != nil {
		logger.Error("SocialHandler: Failed to follow: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to follow trader",
		})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"following":  true,
		"target":     req.TargetAddress,
	})
}

// UnfollowTrader unfollows a trader
// DELETE /api/v1/social/follow/:address
func (h *SocialHandler) UnfollowTrader(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	targetAddress := c.Params("address")
	if targetAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Target address is required",
		})
	}

	err = h.socialService.UnfollowTrader(c.Context(), user.ID, targetAddress)
	if err != nil {
		logger.Error("SocialHandler: Failed to unfollow: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to unfollow trader",
		})
	}

	return c.JSON(fiber.Map{
		"success":   true,
		"following": false,
		"target":    targetAddress,
	})
}

// GetFollowing returns list of traders the user is following
// GET /api/v1/social/following
func (h *SocialHandler) GetFollowing(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	following, err := h.socialService.GetFollowingWithProfiles(c.Context(), user.ID)
	if err != nil {
		logger.Error("SocialHandler: Failed to get following: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch following list",
		})
	}

	return c.JSON(fiber.Map{
		"following": following,
		"count":     len(following),
	})
}

// CheckIsFollowing checks if user is following a target
// GET /api/v1/social/following/:address
func (h *SocialHandler) CheckIsFollowing(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	targetAddress := c.Params("address")
	isFollowing, err := h.socialService.IsFollowing(c.Context(), user.ID, targetAddress)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to check follow status",
		})
	}

	return c.JSON(fiber.Map{
		"is_following": isFollowing,
		"target":       targetAddress,
	})
}

// GetNotifications returns user's notifications
// GET /api/v1/social/notifications
func (h *SocialHandler) GetNotifications(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	notifications, err := h.notificationService.GetNotifications(c.Context(), user.ID, limit, offset)
	if err != nil {
		logger.Error("SocialHandler: Failed to get notifications: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch notifications",
		})
	}

	unreadCount, _ := h.notificationService.GetUnreadCount(c.Context(), user.ID)

	return c.JSON(fiber.Map{
		"notifications": notifications,
		"unread_count":  unreadCount,
		"count":         len(notifications),
	})
}

// MarkNotificationRead marks a notification as read
// POST /api/v1/social/notifications/:id/read
func (h *SocialHandler) MarkNotificationRead(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	notifID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid notification ID",
		})
	}

	err = h.notificationService.MarkAsRead(c.Context(), user.ID, notifID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to mark notification as read",
		})
	}

	return c.JSON(fiber.Map{"success": true})
}

// MarkAllNotificationsRead marks all notifications as read
// POST /api/v1/social/notifications/read-all
func (h *SocialHandler) MarkAllNotificationsRead(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	err = h.notificationService.MarkAllAsRead(c.Context(), user.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to mark all as read",
		})
	}

	return c.JSON(fiber.Map{"success": true})
}
