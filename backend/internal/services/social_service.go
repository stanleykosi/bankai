/**
 * @description
 * Social Service for follow/unfollow operations.
 * Manages follower relationships in the database.
 *
 * @dependencies
 * - gorm.io/gorm
 * - backend/internal/models
 */

package services

import (
	"context"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SocialService handles social features (follows, notifications)
type SocialService struct {
	db          *gorm.DB
	gammaClient *gamma.Client
}

// NewSocialService creates a new SocialService
func NewSocialService(db *gorm.DB, gammaClient *gamma.Client) *SocialService {
	return &SocialService{
		db:          db,
		gammaClient: gammaClient,
	}
}

// FollowTrader adds a follow relationship
func (s *SocialService) FollowTrader(ctx context.Context, userID uuid.UUID, targetAddress string) error {
	targetAddress = strings.ToLower(strings.TrimSpace(targetAddress))
	if targetAddress == "" {
		return nil
	}

	follow := &models.Follow{
		FollowerID:    userID,
		TargetAddress: targetAddress,
		CreatedAt:     time.Now(),
	}

	// Use ON CONFLICT DO NOTHING to avoid duplicates
	result := s.db.WithContext(ctx).
		Clauses().
		Where("follower_id = ? AND target_address = ?", userID, targetAddress).
		FirstOrCreate(follow)

	if result.Error != nil {
		logger.Error("SocialService: Failed to follow trader: %v", result.Error)
		return result.Error
	}

	return nil
}

// UnfollowTrader removes a follow relationship
func (s *SocialService) UnfollowTrader(ctx context.Context, userID uuid.UUID, targetAddress string) error {
	targetAddress = strings.ToLower(strings.TrimSpace(targetAddress))

	result := s.db.WithContext(ctx).
		Where("follower_id = ? AND target_address = ?", userID, targetAddress).
		Delete(&models.Follow{})

	if result.Error != nil {
		logger.Error("SocialService: Failed to unfollow trader: %v", result.Error)
		return result.Error
	}

	return nil
}

// GetFollowing returns list of addresses the user is following
func (s *SocialService) GetFollowing(ctx context.Context, userID uuid.UUID) ([]models.Follow, error) {
	var follows []models.Follow

	result := s.db.WithContext(ctx).
		Where("follower_id = ?", userID).
		Order("created_at DESC").
		Find(&follows)

	if result.Error != nil {
		return nil, result.Error
	}

	return follows, nil
}

// GetFollowingWithProfiles returns follows with profile info
func (s *SocialService) GetFollowingWithProfiles(ctx context.Context, userID uuid.UUID) ([]models.FollowWithProfile, error) {
	follows, err := s.GetFollowing(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]models.FollowWithProfile, len(follows))
	for i, f := range follows {
		result[i] = models.FollowWithProfile{
			Follow: f,
		}

		// Try to get profile from Gamma
		profiles, err := s.gammaClient.SearchProfiles(ctx, f.TargetAddress, 1)
		if err == nil && len(profiles) > 0 {
			p := profiles[0]
			result[i].ProfileName = p.Name
			if result[i].ProfileName == "" {
				result[i].ProfileName = p.Pseudonym
			}
			result[i].ProfileImage = p.ProfileImage
		}
	}

	return result, nil
}

// GetFollowers returns list of users following a specific address
func (s *SocialService) GetFollowers(ctx context.Context, targetAddress string) ([]models.Follow, error) {
	targetAddress = strings.ToLower(strings.TrimSpace(targetAddress))

	var follows []models.Follow

	result := s.db.WithContext(ctx).
		Where("target_address = ?", targetAddress).
		Order("created_at DESC").
		Find(&follows)

	if result.Error != nil {
		return nil, result.Error
	}

	return follows, nil
}

// GetFollowerUserIDs returns user IDs of followers for a target address
// Used for sending notifications when target makes a trade
func (s *SocialService) GetFollowerUserIDs(ctx context.Context, targetAddress string) ([]uuid.UUID, error) {
	targetAddress = strings.ToLower(strings.TrimSpace(targetAddress))

	var userIDs []uuid.UUID

	result := s.db.WithContext(ctx).
		Model(&models.Follow{}).
		Where("target_address = ?", targetAddress).
		Pluck("follower_id", &userIDs)

	if result.Error != nil {
		return nil, result.Error
	}

	return userIDs, nil
}

// IsFollowing checks if user is following a target address
func (s *SocialService) IsFollowing(ctx context.Context, userID uuid.UUID, targetAddress string) (bool, error) {
	targetAddress = strings.ToLower(strings.TrimSpace(targetAddress))

	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.Follow{}).
		Where("follower_id = ? AND target_address = ?", userID, targetAddress).
		Count(&count)

	if result.Error != nil {
		return false, result.Error
	}

	return count > 0, nil
}

// GetFollowingCount returns the number of traders a user is following
func (s *SocialService) GetFollowingCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.Follow{}).
		Where("follower_id = ?", userID).
		Count(&count)

	if result.Error != nil {
		return 0, result.Error
	}

	return count, nil
}

// GetFollowerCount returns the number of followers for an address
func (s *SocialService) GetFollowerCount(ctx context.Context, targetAddress string) (int64, error) {
	targetAddress = strings.ToLower(strings.TrimSpace(targetAddress))

	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.Follow{}).
		Where("target_address = ?", targetAddress).
		Count(&count)

	if result.Error != nil {
		return 0, result.Error
	}

	return count, nil
}
