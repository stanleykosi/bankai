package middleware

import (
	"strings"

	"github.com/bankai-project/backend/internal/models"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	blockedUserKeyPrefix   = "moderation:user:block:"
	blockedWalletKeyPrefix = "moderation:wallet:block:"
)

// AccountGuard blocks requests for moderated users/wallets.
func AccountGuard(rdb *redis.Client, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if rdb == nil {
			return c.Next()
		}

		userID, _ := c.Locals("user_id").(string)
		userID = strings.ToLower(strings.TrimSpace(userID))
		wallet, _ := c.Locals("wallet_address").(string)
		wallet = strings.TrimSpace(wallet)

		walletCandidates := make(map[string]struct{}, 3)
		if wallet != "" {
			walletCandidates[strings.ToLower(wallet)] = struct{}{}
		}

		// Resolve stored account identities (EOA + vault) so moderation blocks apply
		// regardless of which wallet was used during SIWE authentication.
		if userID != "" && db != nil {
			var user models.User
			if err := db.WithContext(c.Context()).
				Select("eoa_address", "vault_address").
				Where("id = ?", userID).
				First(&user).Error; err == nil {
				if eoa := strings.TrimSpace(user.EOAAddress); eoa != "" {
					walletCandidates[strings.ToLower(eoa)] = struct{}{}
				}
				if vault := strings.TrimSpace(user.VaultAddress); vault != "" {
					walletCandidates[strings.ToLower(vault)] = struct{}{}
				}
			}
		}

		if userID == "" && len(walletCandidates) == 0 {
			return c.Next()
		}

		pipe := rdb.Pipeline()
		var userCmd *redis.IntCmd
		walletCmds := make([]*redis.IntCmd, 0, len(walletCandidates))
		if userID != "" {
			userCmd = pipe.Exists(c.Context(), blockedUserKeyPrefix+userID)
		}
		for candidate := range walletCandidates {
			walletCmds = append(walletCmds, pipe.Exists(c.Context(), blockedWalletKeyPrefix+candidate))
		}
		if _, err := pipe.Exec(c.Context()); err != nil {
			// Fail-open for availability.
			return c.Next()
		}

		if userCmd != nil && userCmd.Val() > 0 {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "account is restricted"})
		}
		for _, walletCmd := range walletCmds {
			if walletCmd != nil && walletCmd.Val() > 0 {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "wallet is restricted"})
			}
		}

		return c.Next()
	}
}
