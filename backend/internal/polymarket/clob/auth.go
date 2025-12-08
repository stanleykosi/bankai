package clob

import (
	"fmt"
	"net/http"
	"time"
)

const (
	authMessage         = "This message attests that I control the given wallet"
	authDomainName      = "ClobAuthDomain"
	authDomainVersion   = "1"
	defaultAuthNonce    = 0
	defaultAuthValidity = 60 // seconds window we consider a timestamp fresh
)

// ClobAuthProof carries the L1 signature required to derive/create a user API key.
// The signature is produced over the EIP-712 ClobAuth struct.
type ClobAuthProof struct {
	Address   string `json:"address"`
	Timestamp string `json:"timestamp"`
	Nonce     int64  `json:"nonce"`
	Signature string `json:"signature"`
}

// APIKeyCredentials represents the user-scoped API key used for POST /order.
type APIKeyCredentials struct {
	Key        string `json:"key"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
	Address    string `json:"address,omitempty"` // convenience for signing headers
}

// BuildAuthPayload constructs a timestamp + nonce for the frontend to sign.
func BuildAuthPayload() (timestamp string, nonce int64) {
	now := time.Now().Unix()
	return fmt.Sprintf("%d", now), defaultAuthNonce
}

// Validate ensures the provided proof has the basics before we forward it.
func (p *ClobAuthProof) Validate() error {
	if p == nil {
		return fmt.Errorf("auth proof is required")
	}
	if p.Address == "" {
		return fmt.Errorf("auth address is required")
	}
	if p.Signature == "" {
		return fmt.Errorf("auth signature is required")
	}
	if p.Timestamp == "" {
		return fmt.Errorf("auth timestamp is required")
	}
	return nil
}

// setL1Headers sets the L1 auth headers required by /auth/api-key and /auth/derive-api-key.
func setL1Headers(req *http.Request, proof *ClobAuthProof) error {
	if proof == nil {
		return fmt.Errorf("auth proof is required")
	}
	if err := proof.Validate(); err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("POLY_ADDRESS", proof.Address)
	req.Header.Set("POLY_SIGNATURE", proof.Signature)
	req.Header.Set("POLY_TIMESTAMP", proof.Timestamp)
	req.Header.Set("POLY_NONCE", fmt.Sprintf("%d", proof.Nonce))
	return nil
}
