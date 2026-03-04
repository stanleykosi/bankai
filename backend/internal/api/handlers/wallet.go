/**
 * @description
 * HTTP Handlers for Wallet management.
 * Exposes endpoints to get wallet status and trigger deployment.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 * - backend/internal/api/middleware
 * - backend/internal/models
 */

package handlers

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/relayer"
	"github.com/bankai-project/backend/internal/services"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gofiber/fiber/v2"
)

type WalletHandler struct {
	Manager           *services.WalletManager
	Blockchain        *services.BlockchainService
	CollateralAssetID string
}

type DeployWalletRequest struct {
	Signature string `json:"signature"`
	Metadata  string `json:"metadata"`
}

func NewWalletHandler(manager *services.WalletManager, blockchain *services.BlockchainService, collateralAssetID string) *WalletHandler {
	return &WalletHandler{
		Manager:           manager,
		Blockchain:        blockchain,
		CollateralAssetID: strings.TrimSpace(collateralAssetID),
	}
}

// GetWallet returns the wallet status for the authenticated user
// GET /api/v1/wallet
func (h *WalletHandler) GetWallet(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	// We use EnsureWallet here to opportunistically check/deploy if missing.
	// This effectively "Auto-Onboards" the user when they visit the app.
	user, err := h.Manager.EnsureWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Wallet check failed: " + err.Error(),
		})
	}

	return c.JSON(user)
}

// GetDeployTypedData returns the EIP-712 payload the frontend must sign to request a Safe deployment.
func (h *WalletHandler) GetDeployTypedData(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to load user: " + err.Error(),
		})
	}

	if user.EOAAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Connect a wallet before requesting deployment"})
	}

	typed := relayer.BuildSafeCreateTypedData()
	return c.JSON(fiber.Map{
		"owner":      user.EOAAddress,
		"typed_data": typed,
	})
}

// DeployWallet consumes a signed SAFE-CREATE request from the frontend and submits it to the relayer.
func (h *WalletHandler) DeployWallet(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req DeployWalletRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}

	if req.Signature == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "signature is required"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to load user: " + err.Error(),
		})
	}

	if user.EOAAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Connect a wallet before requesting deployment"})
	}

	// Idempotency: if Safe already exists (relayer or stored), short-circuit without re-signing.
	derivedSafe, derr := relayer.DeriveSafeAddress(user.EOAAddress)
	if derr == nil && derivedSafe != "" {
		if deployed, checkErr := h.Manager.Relayer.GetDeployed(c.Context(), derivedSafe); checkErr == nil && deployed {
			wType := models.WalletTypeSafe
			if err := h.Manager.UpdateVaultAddress(c.Context(), userID, derivedSafe, &wType); err != nil {
				logger.Error("Failed to persist existing safe address for user %s: %v", userID, err)
			}
			return c.JSON(fiber.Map{
				"task_id":          "",
				"state":            "ALREADY_DEPLOYED",
				"transaction_hash": "",
				"proxy_address":    derivedSafe,
			})
		}
	}

	txReq, err := relayer.BuildSafeCreateRequest(user.EOAAddress, req.Signature, req.Metadata)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	resp, err := h.Manager.Relayer.DeploySafe(c.Context(), txReq)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Relayer deployment failed: " + err.Error()})
	}

	if resp.ProxyAddress != "" {
		wType := models.WalletTypeSafe
		if err := h.Manager.UpdateVaultAddress(c.Context(), userID, resp.ProxyAddress, &wType); err != nil {
			logger.Error("Failed to persist deployed safe address for user %s: %v", userID, err)
		}
	}

	return c.JSON(fiber.Map{
		"task_id":          resp.TaskID,
		"state":            resp.State,
		"transaction_hash": resp.TransactionHash,
		"proxy_address":    resp.ProxyAddress,
	})
}

// UpdateWallet allows the frontend to report a discovered wallet address
// (Useful if the frontend detects the proxy via other means/libraries)
// POST /api/v1/wallet/update
type UpdateWalletRequest struct {
	VaultAddress string `json:"vault_address"`
	WalletType   string `json:"wallet_type"` // "PROXY" or "SAFE"
}

func (h *WalletHandler) UpdateWallet(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req UpdateWalletRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}

	if req.VaultAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "vault_address is required"})
	}

	var wType *models.WalletType
	if req.WalletType == "PROXY" {
		wt := models.WalletTypeProxy
		wType = &wt
	} else if req.WalletType == "SAFE" {
		wt := models.WalletTypeSafe
		wType = &wt
	}

	if err := h.Manager.UpdateVaultAddress(c.Context(), userID, req.VaultAddress, wType); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update wallet: " + err.Error()})
	}

	return c.JSON(fiber.Map{"status": "success"})
}

// GetDepositAddress returns the vault address for deposits
// GET /api/v1/wallet/deposit
func (h *WalletHandler) GetDepositAddress(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get user wallet: " + err.Error(),
		})
	}

	if user.VaultAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Vault address not found. Please connect a wallet first.",
		})
	}

	tokenAddress := h.CollateralAssetID
	if tokenAddress == "" && h.Blockchain != nil {
		tokenAddress = h.Blockchain.USDCAddress()
	}

	return c.JSON(fiber.Map{
		"vault_address": user.VaultAddress,
		"network":       "polygon",
		"token":         "USDC",
		"token_address": tokenAddress,
	})
}

// GetBalance returns the USDC balance of the user's vault
// GET /api/v1/wallet/balance
func (h *WalletHandler) GetBalance(c *fiber.Ctx) error {
	if h.Blockchain == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Blockchain service unavailable",
		})
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get user wallet: " + err.Error(),
		})
	}

	if user.VaultAddress == "" {
		return c.JSON(fiber.Map{
			"balance":           "0",
			"balance_formatted": "0.00",
			"vault_address":     "",
		})
	}

	fresh := false
	if value := strings.TrimSpace(c.Query("fresh")); value != "" {
		switch strings.ToLower(value) {
		case "1", "true", "yes":
			fresh = true
		}
	}

	var balance *big.Int
	if fresh {
		balance, err = h.Blockchain.GetUSDCBalanceFresh(c.Context(), user.VaultAddress)
	} else {
		balance, err = h.Blockchain.GetUSDCBalance(c.Context(), user.VaultAddress)
	}
	if err != nil {
		logger.Error("Failed to fetch USDC balance for %s: %v", user.VaultAddress, err)
		if cached := h.Blockchain.GetCachedUSDCBalance(user.VaultAddress, true); cached != nil {
			return c.JSON(fiber.Map{
				"balance":           cached.String(),
				"balance_formatted": h.Blockchain.FormatUSDCBalance(cached),
				"vault_address":     user.VaultAddress,
				"token":             "USDC",
				"token_address":     h.Blockchain.USDCAddress(),
				"balance_stale":     true,
			})
		}
		return c.JSON(fiber.Map{
			"balance":             "0",
			"balance_formatted":   "0.00",
			"vault_address":       user.VaultAddress,
			"token":               "USDC",
			"token_address":       h.Blockchain.USDCAddress(),
			"balance_unavailable": true,
		})
	}

	return c.JSON(fiber.Map{
		"balance":           balance.String(),
		"balance_formatted": h.Blockchain.FormatUSDCBalance(balance),
		"vault_address":     user.VaultAddress,
		"token":             "USDC",
		"token_address":     h.Blockchain.USDCAddress(),
		"balance_fresh":     fresh,
	})
}

// GetWithdrawNonce returns the next SAFE nonce from the relayer.
// GET /api/v1/wallet/withdraw/nonce
func (h *WalletHandler) GetWithdrawNonce(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get user wallet: " + err.Error(),
		})
	}
	if user.EOAAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "EOA address not found"})
	}
	if h.Manager == nil || h.Manager.Relayer == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Relayer service unavailable",
		})
	}

	nonce, err := h.Manager.Relayer.GetNonce(c.Context(), user.EOAAddress, relayer.TransactionTypeSafe)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "Failed to fetch relayer nonce: " + err.Error(),
		})
	}
	return c.JSON(fiber.Map{"nonce": nonce})
}

// WithdrawRequest represents a withdrawal request
type WithdrawRequest struct {
	ToAddress       string `json:"to_address"`       // Destination address (EOA)
	Amount          string `json:"amount"`           // Amount in USDC atomic units (metadata only)
	SafeTxTo        string `json:"safe_tx_to"`       // Transaction "to" for SAFE (defaults to collateral token)
	SafeTxData      string `json:"safe_tx_data"`     // ABI-encoded Safe transaction payload
	Nonce           string `json:"nonce"`            // Relayer nonce for SAFE tx
	Signature       string `json:"signature"`        // Packed SAFE signature
	Operation       string `json:"operation"`        // SAFE operation (0=CALL, 1=DELEGATE_CALL)
	SafeTxnGas      string `json:"safe_txn_gas"`     // SAFE tx gas
	BaseGas         string `json:"base_gas"`         // SAFE base gas
	GasPrice        string `json:"gas_price"`        // SAFE gas price
	GasToken        string `json:"gas_token"`        // SAFE gas token
	RefundReceiver  string `json:"refund_receiver"`  // SAFE refund receiver
	PaymentToken    string `json:"payment_token"`    // Optional SAFE-CREATE compatibility
	Payment         string `json:"payment"`          // Optional SAFE-CREATE compatibility
	PaymentReceiver string `json:"payment_receiver"` // Optional SAFE-CREATE compatibility
	Metadata        string `json:"metadata"`         // Optional metadata/audit tag
}

var erc20TransferMethodID = []byte{0xa9, 0x05, 0x9c, 0xbb}

func decodeERC20TransferCall(raw string) (common.Address, *big.Int, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "0x"))
	if trimmed == "" {
		return common.Address{}, nil, fmt.Errorf("calldata is empty")
	}
	data, err := hex.DecodeString(trimmed)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("calldata is not valid hex: %w", err)
	}
	if len(data) != 4+32+32 {
		return common.Address{}, nil, fmt.Errorf("expected ERC20 transfer calldata length %d, got %d", 68, len(data))
	}
	if !bytes.Equal(data[:4], erc20TransferMethodID) {
		return common.Address{}, nil, fmt.Errorf("calldata method selector is not transfer(address,uint256)")
	}

	to := common.BytesToAddress(data[4+12 : 4+32])
	amount := new(big.Int).SetBytes(data[4+32 : 4+64])
	return to, amount, nil
}

// Withdraw submits a signed SAFE transfer transaction via the relayer.
// POST /api/v1/wallet/withdraw
func (h *WalletHandler) Withdraw(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req WithdrawRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}

	if req.ToAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "to_address is required"})
	}

	if req.Amount == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "amount is required"})
	}
	if !common.IsHexAddress(req.ToAddress) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "to_address must be a valid hex address"})
	}
	if strings.TrimSpace(req.SafeTxData) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "safe_tx_data is required"})
	}
	if strings.TrimSpace(req.Nonce) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "nonce is required"})
	}
	if strings.TrimSpace(req.Signature) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "signature is required"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get user wallet: " + err.Error(),
		})
	}

	if user.VaultAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Vault address not found. Please connect a wallet first.",
		})
	}
	if !common.IsHexAddress(user.VaultAddress) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Vault address is invalid",
		})
	}
	if h.Manager == nil || h.Manager.Relayer == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Relayer service unavailable",
		})
	}

	operation := strings.TrimSpace(req.Operation)
	if operation == "" {
		operation = "0"
	}
	safeTxnGas := strings.TrimSpace(req.SafeTxnGas)
	if safeTxnGas == "" {
		safeTxnGas = "0"
	}
	baseGas := strings.TrimSpace(req.BaseGas)
	if baseGas == "" {
		baseGas = "0"
	}
	gasPrice := strings.TrimSpace(req.GasPrice)
	if gasPrice == "" {
		gasPrice = "0"
	}
	gasToken := strings.TrimSpace(req.GasToken)
	if gasToken == "" {
		gasToken = relayer.ZeroAddress
	}
	refundReceiver := strings.TrimSpace(req.RefundReceiver)
	if refundReceiver == "" {
		refundReceiver = relayer.ZeroAddress
	}
	safeTxTo := strings.TrimSpace(req.SafeTxTo)
	if safeTxTo == "" {
		safeTxTo = h.CollateralAssetID
	}
	if safeTxTo == "" && h.Blockchain != nil {
		safeTxTo = h.Blockchain.USDCAddress()
	}
	if !common.IsHexAddress(safeTxTo) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "safe_tx_to must be a valid hex address",
		})
	}

	expectedToken := strings.TrimSpace(h.CollateralAssetID)
	if expectedToken == "" && h.Blockchain != nil {
		expectedToken = strings.TrimSpace(h.Blockchain.USDCAddress())
	}
	if expectedToken != "" && !strings.EqualFold(safeTxTo, expectedToken) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "safe_tx_to must match configured collateral token",
		})
	}

	amountValue, ok := new(big.Int).SetString(strings.TrimSpace(req.Amount), 10)
	if !ok || amountValue.Sign() <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "amount must be a positive integer string"})
	}
	calldataTo, calldataAmount, err := decodeERC20TransferCall(req.SafeTxData)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "safe_tx_data must be ERC20 transfer(address,uint256) calldata",
		})
	}
	if !strings.EqualFold(calldataTo.Hex(), req.ToAddress) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "safe_tx_data destination does not match to_address",
		})
	}
	if calldataAmount.Cmp(amountValue) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "safe_tx_data amount does not match amount",
		})
	}

	txReq := &relayer.TransactionRequest{
		Type:        relayer.TransactionTypeSafe,
		From:        user.EOAAddress,
		To:          safeTxTo,
		ProxyWallet: user.VaultAddress,
		Data:        req.SafeTxData,
		Nonce:       req.Nonce,
		Signature:   req.Signature,
		SignatureParams: relayer.SignatureParams{
			Operation:      operation,
			SafeTxnGas:     safeTxnGas,
			BaseGas:        baseGas,
			GasPrice:       gasPrice,
			GasToken:       gasToken,
			RefundReceiver: refundReceiver,
		},
		Metadata: req.Metadata,
	}

	resp, err := h.Manager.Relayer.SubmitSafeTransaction(c.Context(), txReq)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "Failed to submit withdrawal transaction: " + err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"task_id":          resp.TaskID,
		"state":            resp.State,
		"transaction_hash": resp.TransactionHash,
		"proxy_address":    resp.ProxyAddress,
	})
}
