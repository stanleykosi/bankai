/**
 * @description
 * Signature Verification Service.
 * Responsible for verifying that the signer of an order matches the authenticated user.
 *
 * @notes
 * - Currently performs a logical check: Does Order.Signer == User.EOA?
 * - In a high-security environment, this would also recover the address from the EIP-712 signature/hash.
 *   However, since the frontend performs the signing and the Backend acts as a relay with
 *   authentication via Clerk, ensuring the Clerk User owns the Signer Address is the primary defense.
 */

package services

import (
	"fmt"
	"strings"

	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
)

type SignatureVerifier struct{}

func NewSignatureVerifier() *SignatureVerifier {
	return &SignatureVerifier{}
}

// VerifyOrderOwnership ensures the authenticated user controls both the signer (EOA)
// and the maker (vault) specified in the order payload.
func (v *SignatureVerifier) VerifyOrderOwnership(user *models.User, order *clob.Order) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	if order == nil {
		return fmt.Errorf("order is nil")
	}

	// Normalize addresses
	userEOA := strings.ToLower(strings.TrimSpace(user.EOAAddress))
	orderSigner := strings.ToLower(strings.TrimSpace(order.Signer))

	if userEOA == "" {
		return fmt.Errorf("user has no connected wallet (EOA)")
	}

	if orderSigner == "" {
		return fmt.Errorf("order signer is missing")
	}

	if userEOA != orderSigner {
		return fmt.Errorf("signer mismatch: order signed by %s but authenticated as %s", orderSigner, userEOA)
	}

	userVault := strings.ToLower(strings.TrimSpace(user.VaultAddress))
	orderMaker := strings.ToLower(strings.TrimSpace(order.Maker))

	if userVault == "" {
		return fmt.Errorf("user has no deployed vault on file")
	}

	if orderMaker == "" {
		return fmt.Errorf("order maker is missing")
	}

	if userVault != orderMaker {
		return fmt.Errorf("maker mismatch: order targets %s but your vault is %s", order.Maker, user.VaultAddress)
	}

	// Validate signature type loosely against wallet type.
	// Polymarket accepts raw EOA signatures (type 0) even when the maker is a Proxy or Safe vault.
	if user.WalletType != nil {
		allowed := map[int]struct{}{0: {}} // always allow raw EOA signature
		switch *user.WalletType {
		case models.WalletTypeProxy:
			allowed[1] = struct{}{}
		case models.WalletTypeSafe:
			allowed[2] = struct{}{}
		}

		if _, ok := allowed[order.SignatureType]; !ok {
			return fmt.Errorf(
				"signature type %d is not valid for wallet type %s (allowed: %v)",
				order.SignatureType,
				*user.WalletType,
				keys(allowed),
			)
		}
	}

	// TODO: For strict cryptographic verification, we would:
	// 1. Reconstruct the EIP-712 typed data hash from the Order struct fields
	// 2. ecrecover(hash, order.Signature)
	// 3. Verify recovered address == order.Signer
	//
	// Since we rely on the CLOB to reject invalid signatures, and we rely on Clerk
	// to authenticate the user, checking ownership of both the signer and maker
	// prevents cross-user submission attacks and ensures trades are limited to the
	// caller's vault.

	return nil
}

// keys returns the keys of the map as a slice for logging.
func keys(m map[int]struct{}) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
