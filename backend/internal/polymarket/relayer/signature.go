package relayer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// buildBuilderSignature replicates the Builder Signing SDK logic documented by Polymarket.
// signature = base64url( HMAC_SHA256( base64Decode(secret), timestamp + method + path + body ) )
// We keep trailing '=' padding per docs.
func buildBuilderSignature(secret string, timestamp int64, method, requestPath string, body []byte) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("builder secret missing")
	}

	normalizedSecret := strings.TrimSpace(secret)

	var decodedSecret []byte
	var err error

	// Builder docs note the secret is base64; the dashboard currently emits URL-safe (+/- replaced).
	decodedSecret, err = base64.RawURLEncoding.DecodeString(normalizedSecret)
	if err != nil {
		decodedSecret, err = base64.URLEncoding.DecodeString(normalizedSecret)
	}
	if err != nil {
		decodedSecret, err = base64.RawStdEncoding.DecodeString(normalizedSecret)
		if err != nil {
			decodedSecret, err = base64.StdEncoding.DecodeString(normalizedSecret)
		}
	}
	if err != nil {
		// As a last resort, treat it as raw bytes.
		decodedSecret = []byte(normalizedSecret)
	}

	payload := fmt.Sprintf("%d%s%s", timestamp, strings.ToUpper(method), requestPath)
	if len(body) > 0 {
		payload += string(body)
	}

	mac := hmac.New(sha256.New, decodedSecret)
	if _, err := mac.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("failed to compute signature: %w", err)
	}

	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	// Make URL-safe while preserving padding
	sig = strings.ReplaceAll(sig, "+", "-")
	sig = strings.ReplaceAll(sig, "/", "_")

	return sig, nil
}
