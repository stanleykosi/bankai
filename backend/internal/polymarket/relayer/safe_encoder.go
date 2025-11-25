/**
 * @description
 * ABI encoding utilities for Gnosis Safe deployment.
 * Handles encoding of Safe setup data and Proxy Factory calls.
 *
 * @dependencies
 * - github.com/ethereum/go-ethereum
 * - github.com/ethereum/go-ethereum/accounts/abi
 * - github.com/ethereum/go-ethereum/common
 */

package relayer

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// Gnosis Safe Proxy Factory Address (Polygon Mainnet)
	// From Polymarket docs: https://polygonscan.com/address/0xaacfeea03eb1561c4e67d661e40682bd20e3541b
	// Confirmed in polymarket_documentation.md line 26712
	GnosisSafeProxyFactoryAddress = "0xaacfeea03eb1561c4e67d661e40682bd20e3541b"

	// Gnosis Safe Master Copy Address (Polygon Mainnet - Safe 1.3.0)
	// This is the standard Safe 1.3.0 implementation on Polygon
	// NOTE: Verify this address matches the actual Safe version used by Polymarket
	GnosisSafeMasterCopyAddress = "0x3E5c63644E683549055b9Be8653de26E0B4CD36E"

	// Gnosis Safe Fallback Handler (Compatibility Fallback Handler 1.3.0)
	// Used for Safe setup - standard address for Safe 1.3.0 on Polygon
	// NOTE: Verify this address matches the actual fallback handler used by Polymarket
	GnosisSafeFallbackHandlerAddress = "0xf48f2B2d2a534e402487b3ee7C18c33Aec0Fe5e4"

	// Zero address for payment token and receiver
	ZeroAddress = "0x0000000000000000000000000000000000000000"
)

// encodeSafeSetup encodes the Safe setup function call
// This creates the initializer data for the Safe proxy
func encodeSafeSetup(ownerAddress string, threshold int) ([]byte, error) {
	// Safe setup function signature:
	// setup(address[] _owners, uint256 _threshold, address to, bytes data, address fallbackHandler, address paymentToken, uint256 payment, address paymentReceiver)
	safeSetupABI := `[{
		"inputs": [
			{"internalType": "address[]", "name": "_owners", "type": "address[]"},
			{"internalType": "uint256", "name": "_threshold", "type": "uint256"},
			{"internalType": "address", "name": "to", "type": "address"},
			{"internalType": "bytes", "name": "data", "type": "bytes"},
			{"internalType": "address", "name": "fallbackHandler", "type": "address"},
			{"internalType": "address", "name": "paymentToken", "type": "address"},
			{"internalType": "uint256", "name": "payment", "type": "uint256"},
			{"internalType": "address", "name": "paymentReceiver", "type": "address"}
		],
		"name": "setup",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	}]`

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(safeSetupABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Safe setup ABI: %w", err)
	}

	// Validate owner address
	ownerAddr := common.HexToAddress(ownerAddress)
	if ownerAddr == (common.Address{}) {
		return nil, fmt.Errorf("invalid owner address: %s", ownerAddress)
	}

	// Prepare setup parameters
	owners := []common.Address{ownerAddr}
	thresholdBig := big.NewInt(int64(threshold))
	to := common.HexToAddress(ZeroAddress)
	data := []byte{}
	fallbackHandler := common.HexToAddress(GnosisSafeFallbackHandlerAddress)
	paymentToken := common.HexToAddress(ZeroAddress)
	payment := big.NewInt(0)
	paymentReceiver := common.HexToAddress(ZeroAddress)

	// Encode the setup function call
	encoded, err := parsedABI.Pack("setup",
		owners,
		thresholdBig,
		to,
		data,
		fallbackHandler,
		paymentToken,
		payment,
		paymentReceiver,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to encode Safe setup: %w", err)
	}

	return encoded, nil
}

// encodeCreateProxyWithNonce encodes the Proxy Factory createProxyWithNonce call
// This is the actual transaction data sent to the factory
func encodeCreateProxyWithNonce(ownerAddress string, saltNonce *big.Int) (string, error) {
	// Proxy Factory createProxyWithNonce function signature:
	// createProxyWithNonce(address _singleton, bytes initializer, uint256 saltNonce)
	proxyFactoryABI := `[{
		"inputs": [
			{"internalType": "address", "name": "_singleton", "type": "address"},
			{"internalType": "bytes", "name": "initializer", "type": "bytes"},
			{"internalType": "uint256", "name": "saltNonce", "type": "uint256"}
		],
		"name": "createProxyWithNonce",
		"outputs": [
			{"internalType": "contract GnosisSafeProxy", "name": "proxy", "type": "address"}
		],
		"stateMutability": "nonpayable",
		"type": "function"
	}]`

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(proxyFactoryABI)))
	if err != nil {
		return "", fmt.Errorf("failed to parse Proxy Factory ABI: %w", err)
	}

	// Encode the Safe setup initializer
	initializer, err := encodeSafeSetup(ownerAddress, 1)
	if err != nil {
		return "", fmt.Errorf("failed to encode Safe setup: %w", err)
	}

	// Prepare parameters
	singleton := common.HexToAddress(GnosisSafeMasterCopyAddress)
	if saltNonce == nil {
		saltNonce = big.NewInt(0) // Default to 0 if not provided
	}

	// Encode the createProxyWithNonce function call
	encoded, err := parsedABI.Pack("createProxyWithNonce",
		singleton,
		initializer,
		saltNonce,
	)
	if err != nil {
		return "", fmt.Errorf("failed to encode createProxyWithNonce: %w", err)
	}

	// Return as hex string with 0x prefix
	return "0x" + hex.EncodeToString(encoded), nil
}

// generateSaltNonce generates a deterministic salt nonce based on the owner address
// This ensures the same owner always gets the same Safe address (if not already deployed)
func generateSaltNonce(ownerAddress string) *big.Int {
	// Use a simple hash of the owner address as the salt nonce
	// In production, you might want to use a more sophisticated approach
	hash := crypto.Keccak256Hash([]byte(ownerAddress))
	return new(big.Int).SetBytes(hash.Bytes())
}

