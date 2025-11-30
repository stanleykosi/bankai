/**
 * @description
 * Blockchain Service for interacting with Polygon network.
 * Handles balance checks for USDC and other ERC20 tokens.
 *
 * @dependencies
 * - github.com/ethereum/go-ethereum
 * - backend/internal/config
 * - backend/internal/logger
 */

package services

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	// Native USDC on Polygon (Current standard - USDC.e has been deprecated)
	// Address: 0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359
	// Note: USDC.e (0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174) has been deprecated
	// Polymarket now uses native USDC on Polygon as of 2024/2025
	USDCAddressPolygon = "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359"

	// Default Polygon RPC endpoint (can be overridden via POLYGON_RPC_URL)
	DefaultPolygonRPCEndpoint = "https://polygon-rpc.com"
)

// ERC20 ABI for balanceOf function
const erc20BalanceOfABI = `[{"constant":true,"inputs":[{"name":"_owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"balance","type":"uint256"}],"type":"function"}]`

type BlockchainService struct {
	client      *ethclient.Client
	usdcAddress common.Address
}

func NewBlockchainService(cfg *config.Config) (*BlockchainService, error) {
	// Use Polygon RPC from config or default
	rpcURL := strings.TrimSpace(cfg.Services.PolygonRPCURL)
	if rpcURL == "" {
		rpcURL = DefaultPolygonRPCEndpoint
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Polygon RPC: %w", err)
	}

	return &BlockchainService{
		client:      client,
		usdcAddress: common.HexToAddress(USDCAddressPolygon),
	}, nil
}

// GetUSDCBalance returns the USDC balance for a given address
func (s *BlockchainService) GetUSDCBalance(ctx context.Context, address string) (*big.Int, error) {
	addr := common.HexToAddress(address)
	if addr == (common.Address{}) {
		return nil, fmt.Errorf("invalid address: %s", address)
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(erc20BalanceOfABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC20 ABI: %w", err)
	}

	// Encode the balanceOf call
	data, err := parsedABI.Pack("balanceOf", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf call: %w", err)
	}

	// Call the contract
	callMsg := ethereum.CallMsg{
		To:   &s.usdcAddress,
		Data: data,
	}

	result, err := s.client.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call contract: %w", err)
	}

	// Unpack the result - balanceOf returns a single uint256
	var results []interface{}
	err = parsedABI.UnpackIntoInterface(&results, "balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack balance result: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned from balanceOf call")
	}

	balance, ok := results[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("failed to decode balance as *big.Int")
	}

	return balance, nil
}

// FormatUSDCBalance formats a USDC balance (6 decimals) to a human-readable string
func (s *BlockchainService) FormatUSDCBalance(balance *big.Int) string {
	if balance == nil {
		return "0.00"
	}

	// USDC has 6 decimals
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil)
	quotient := new(big.Int).Div(balance, divisor)
	remainder := new(big.Int).Mod(balance, divisor)

	// Format with 2 decimal places
	remainderStr := remainder.String()
	for len(remainderStr) < 6 {
		remainderStr = "0" + remainderStr
	}

	// Take first 2 decimal places
	if len(remainderStr) > 2 {
		remainderStr = remainderStr[:2]
	}

	return fmt.Sprintf("%s.%s", quotient.String(), remainderStr)
}

// Close closes the Ethereum client connection
func (s *BlockchainService) Close() {
	if s.client != nil {
		s.client.Close()
	}
}
