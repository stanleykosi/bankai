package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// AdminOnly enforces wallet-based admin access.
// Wallets must be configured in ADMIN_WALLET_ALLOWLIST.
func AdminOnly(allowedWallets []string) fiber.Handler {
	allowed := make(map[string]struct{}, len(allowedWallets))
	for _, wallet := range allowedWallets {
		w := strings.ToLower(strings.TrimSpace(wallet))
		if w == "" {
			continue
		}
		allowed[w] = struct{}{}
	}

	return func(c *fiber.Ctx) error {
		if len(allowed) == 0 {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin access is not configured"})
		}

		wallet, err := GetWalletAddress(c)
		if err != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "wallet context missing"})
		}

		if _, ok := allowed[strings.ToLower(strings.TrimSpace(wallet))]; !ok {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin access denied"})
		}

		c.Locals("is_admin", true)
		return c.Next()
	}
}
