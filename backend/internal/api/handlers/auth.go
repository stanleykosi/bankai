/**
 * @description
 * Wallet-only authentication handlers (SIWE challenge/verify + JWT cookie).
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - github.com/ethereum/go-ethereum: signature recovery
 * - github.com/golang-jwt/jwt/v5
 * - redis: nonce storage
 */

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/models"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type AuthHandler struct {
	DB    *gorm.DB
	Redis *redis.Client
	Cfg   *config.Config
}

func NewAuthHandler(db *gorm.DB, rdb *redis.Client, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		DB:    db,
		Redis: rdb,
		Cfg:   cfg,
	}
}

type ChallengeRequest struct {
	Address string `json:"address"`
	ChainID int    `json:"chain_id"`
}

type ChallengeResponse struct {
	Message   string `json:"message"`
	Nonce     string `json:"nonce"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
}

type VerifyRequest struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

type siweMessage struct {
	Domain   string
	Address  string
	URI      string
	Version  string
	ChainID  int
	Nonce    string
	IssuedAt string
}

const (
	siweHeaderSuffix   = " wants you to sign in with your Ethereum account:"
	nonceKeyPrefix     = "auth:nonce:"
	signerAssertionTTL = 2 * time.Minute
)

var consumeNonceScript = redis.NewScript(`
local key = KEYS[1]
local expected = ARGV[1]
local current = redis.call("GET", key)
if not current then
  return 0
end
if current ~= expected then
  return -1
end
redis.call("DEL", key)
return 1
`)

// Challenge returns a SIWE message for the client to sign.
// POST /api/v1/auth/challenge
func (h *AuthHandler) Challenge(c *fiber.Ctx) error {
	if h.Redis == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Auth store unavailable"})
	}

	var req ChallengeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	address := strings.TrimSpace(req.Address)
	if !common.IsHexAddress(address) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid wallet address"})
	}
	if req.ChainID <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid chain_id"})
	}

	domain := strings.TrimSpace(h.Cfg.Auth.Domain)
	uri := strings.TrimSpace(h.Cfg.Auth.URI)
	if domain == "" || uri == "" {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Auth domain/uri not configured"})
	}

	nonce, err := generateNonce()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate nonce"})
	}

	issuedAt := time.Now().UTC()
	expiresAt := issuedAt.Add(time.Duration(h.Cfg.Auth.NonceTTLMinutes) * time.Minute)
	message := buildSIWEMessage(domain, address, "Sign in to Bankai", uri, req.ChainID, nonce, issuedAt)

	key := nonceKey(nonce)
	value := fmt.Sprintf("%s|%d", strings.ToLower(address), req.ChainID)
	if err := h.Redis.Set(context.Background(), key, value, time.Until(expiresAt)).Err(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to store nonce"})
	}

	return c.JSON(ChallengeResponse{
		Message:   message,
		Nonce:     nonce,
		IssuedAt:  issuedAt.Format(time.RFC3339),
		ExpiresAt: expiresAt.Format(time.RFC3339),
	})
}

// Verify validates the SIWE signature and issues a JWT cookie.
// POST /api/v1/auth/verify
func (h *AuthHandler) Verify(c *fiber.Ctx) error {
	if h.Redis == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Auth store unavailable"})
	}

	var req VerifyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	msg, err := parseSIWEMessage(req.Message)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if strings.TrimSpace(h.Cfg.Auth.Domain) != "" && msg.Domain != h.Cfg.Auth.Domain {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid SIWE domain"})
	}
	if strings.TrimSpace(h.Cfg.Auth.URI) != "" && msg.URI != h.Cfg.Auth.URI {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid SIWE URI"})
	}
	if msg.Version != "1" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unsupported SIWE version"})
	}

	if err := verifySignature(req.Message, req.Signature, msg.Address); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}

	key := nonceKey(msg.Nonce)
	expectedNoncePayload := fmt.Sprintf("%s|%d", strings.ToLower(msg.Address), msg.ChainID)
	consumed, mismatch, err := h.consumeNonce(context.Background(), key, expectedNoncePayload)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Auth store unavailable"})
	}
	if mismatch {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Nonce mismatch"})
	}
	if !consumed {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Nonce expired"})
	}

	user, err := h.upsertUser(strings.ToLower(msg.Address))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create user"})
	}

	if err := h.issueAuthCookie(c, user, strings.ToLower(msg.Address)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to issue session"})
	}

	return c.JSON(fiber.Map{
		"user": user,
	})
}

// Logout clears the auth cookie.
// POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	expired := time.Unix(0, 0).UTC()
	cookie := &fiber.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    "",
		Expires:  expired,
		MaxAge:   -1,
		Path:     "/",
		HTTPOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		SameSite: normalizeSameSite(h.Cfg.Auth.CookieSameSite),
	}
	if domain := strings.TrimSpace(h.Cfg.Auth.CookieDomain); domain != "" {
		cookie.Domain = domain
	}
	c.Cookie(cookie)
	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *AuthHandler) upsertUser(address string) (*models.User, error) {
	var user models.User
	err := h.DB.Where("eoa_address = ?", address).First(&user).Error
	if err == nil {
		return &user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	user = models.User{
		EOAAddress: address,
	}
	if err := h.DB.Create(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (h *AuthHandler) issueAuthCookie(c *fiber.Ctx, user *models.User, wallet string) error {
	now := time.Now().UTC()
	exp := now.Add(time.Duration(h.Cfg.Auth.TokenTTLMinutes) * time.Minute)

	claims := middleware.AuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			Issuer:    h.Cfg.Auth.JWTIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Wallet:    wallet,
		TokenType: middleware.AuthTokenTypeSession,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signed, err := token.SignedString([]byte(h.Cfg.Auth.JWTSecret))
	if err != nil {
		return err
	}

	cookie := &fiber.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    signed,
		Expires:  exp,
		Path:     "/",
		HTTPOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		SameSite: normalizeSameSite(h.Cfg.Auth.CookieSameSite),
	}
	if domain := strings.TrimSpace(h.Cfg.Auth.CookieDomain); domain != "" {
		cookie.Domain = domain
	}
	c.Cookie(cookie)
	return nil
}

// SignerAssertion issues a short-lived bearer token for frontend signer minting.
// GET /api/v1/auth/signer-assertion
func (h *AuthHandler) SignerAssertion(c *fiber.Ctx) error {
	if h == nil || h.Cfg == nil || strings.TrimSpace(h.Cfg.Auth.JWTSecret) == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "auth configuration unavailable",
		})
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	wallet, err := middleware.GetWalletAddress(c)
	if err != nil || strings.TrimSpace(wallet) == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "wallet context missing"})
	}

	now := time.Now().UTC()
	exp := now.Add(signerAssertionTTL)
	claims := middleware.AuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strings.TrimSpace(userID),
			Issuer:    h.Cfg.Auth.JWTIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Wallet:    strings.ToLower(strings.TrimSpace(wallet)),
		TokenType: middleware.AuthTokenTypeSignerAssertion,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signed, signErr := token.SignedString([]byte(h.Cfg.Auth.JWTSecret))
	if signErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to issue signer assertion",
		})
	}

	return c.JSON(fiber.Map{
		"token":      signed,
		"expires_at": exp.Unix(),
	})
}

func generateNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func nonceKey(nonce string) string {
	return nonceKeyPrefix + nonce
}

func (h *AuthHandler) consumeNonce(ctx context.Context, key, expected string) (consumed bool, mismatch bool, err error) {
	if h == nil || h.Redis == nil {
		return false, false, fmt.Errorf("auth store unavailable")
	}
	result, err := consumeNonceScript.Run(ctx, h.Redis, []string{key}, expected).Int()
	if err != nil {
		return false, false, err
	}
	switch result {
	case 1:
		return true, false, nil
	case 0:
		return false, false, nil
	case -1:
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unexpected nonce consume result: %d", result)
	}
}

func buildSIWEMessage(domain, address, statement, uri string, chainID int, nonce string, issuedAt time.Time) string {
	lines := []string{
		fmt.Sprintf("%s%s", domain, siweHeaderSuffix),
		address,
		"",
	}
	if statement != "" {
		lines = append(lines, statement, "")
	}
	lines = append(lines,
		fmt.Sprintf("URI: %s", uri),
		"Version: 1",
		fmt.Sprintf("Chain ID: %d", chainID),
		fmt.Sprintf("Nonce: %s", nonce),
		fmt.Sprintf("Issued At: %s", issuedAt.Format(time.RFC3339)),
	)
	return strings.Join(lines, "\n")
}

func parseSIWEMessage(message string) (*siweMessage, error) {
	lines := strings.Split(message, "\n")
	if len(lines) < 6 {
		return nil, fmt.Errorf("Malformed SIWE message")
	}

	header := strings.TrimSpace(lines[0])
	if !strings.HasSuffix(header, siweHeaderSuffix) {
		return nil, fmt.Errorf("Invalid SIWE header")
	}
	domain := strings.TrimSuffix(header, siweHeaderSuffix)
	address := strings.TrimSpace(lines[1])
	if !common.IsHexAddress(address) {
		return nil, fmt.Errorf("Invalid SIWE address")
	}

	fields := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "URI:"):
			fields["uri"] = strings.TrimSpace(strings.TrimPrefix(line, "URI:"))
		case strings.HasPrefix(line, "Version:"):
			fields["version"] = strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
		case strings.HasPrefix(line, "Chain ID:"):
			fields["chain_id"] = strings.TrimSpace(strings.TrimPrefix(line, "Chain ID:"))
		case strings.HasPrefix(line, "Nonce:"):
			fields["nonce"] = strings.TrimSpace(strings.TrimPrefix(line, "Nonce:"))
		case strings.HasPrefix(line, "Issued At:"):
			fields["issued_at"] = strings.TrimSpace(strings.TrimPrefix(line, "Issued At:"))
		}
	}

	if fields["uri"] == "" || fields["version"] == "" || fields["chain_id"] == "" || fields["nonce"] == "" || fields["issued_at"] == "" {
		return nil, fmt.Errorf("Malformed SIWE message")
	}

	chainID, err := strconv.Atoi(fields["chain_id"])
	if err != nil {
		return nil, fmt.Errorf("Invalid SIWE chain id")
	}

	return &siweMessage{
		Domain:   domain,
		Address:  address,
		URI:      fields["uri"],
		Version:  fields["version"],
		ChainID:  chainID,
		Nonce:    fields["nonce"],
		IssuedAt: fields["issued_at"],
	}, nil
}

func verifySignature(message, signature, expectedAddress string) error {
	if signature == "" {
		return fmt.Errorf("Missing signature")
	}
	sig, err := hexutil.Decode(signature)
	if err != nil {
		return fmt.Errorf("Invalid signature encoding")
	}
	if len(sig) != 65 {
		return fmt.Errorf("Invalid signature length")
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	hash := accounts.TextHash([]byte(message))
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return fmt.Errorf("Invalid signature")
	}

	recovered := crypto.PubkeyToAddress(*pubKey).Hex()
	if !strings.EqualFold(recovered, expectedAddress) {
		return fmt.Errorf("Signature mismatch")
	}
	return nil
}

func normalizeSameSite(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case fiber.CookieSameSiteNoneMode:
		return fiber.CookieSameSiteNoneMode
	case fiber.CookieSameSiteStrictMode:
		return fiber.CookieSameSiteStrictMode
	case fiber.CookieSameSiteDisabled:
		return fiber.CookieSameSiteDisabled
	default:
		return fiber.CookieSameSiteLaxMode
	}
}
