/**
 * @description
 * Data structures for the Polymarket CLOB API.
 * Includes Order definitions, Request/Response payloads, and Enums.
 *
 * @notes
 * - Matches the JSON structure expected by POST /order and other CLOB endpoints.
 * - Handles both raw string numbers and float types where appropriate.
 */

package clob

import (
	"errors"
	"fmt"
	"strings"
)

// OrderSide represents the side of the trade (BUY/SELL)
type OrderSide string

const (
	BUY  OrderSide = "BUY"
	SELL OrderSide = "SELL"
)

// OrderType represents the execution strategy
type OrderType string

const (
	OrderTypeGTC OrderType = "GTC" // Good-Til-Cancelled
	OrderTypeGTD OrderType = "GTD" // Good-Til-Date
	OrderTypeFOK OrderType = "FOK" // Fill-Or-Kill
	OrderTypeFAK OrderType = "FAK" // Fill-And-Kill
)

var (
	validOrderTypes = map[OrderType]struct{}{
		OrderTypeGTC: {},
		OrderTypeGTD: {},
		OrderTypeFOK: {},
		OrderTypeFAK: {},
	}
	validOrderSides = map[OrderSide]struct{}{
		BUY:  {},
		SELL: {},
	}
	// Signature types (per Polymarket clob-client):
	// 0 = raw EOA signature (works for most wallets, including Safe owners),
	// 1 = Magic/Proxy,
	// 2 = Browser wallets (Metamask/Coinbase) / Safe.
	validSignatureTypes = map[int]struct{}{
		0: {},
		1: {},
		2: {},
	}
)

// Order represents the signed EIP-712 order structure
// JSON tags use camelCase for unmarshaling from frontend
type Order struct {
	Salt          string    `json:"salt"`
	Maker         string    `json:"maker"`
	Signer        string    `json:"signer"`
	Taker         string    `json:"taker"`
	TokenID       string    `json:"tokenId"`       // camelCase for frontend
	MakerAmount   string    `json:"makerAmount"`   // camelCase for frontend
	TakerAmount   string    `json:"takerAmount"`   // camelCase for frontend
	Expiration    string    `json:"expiration"`
	Nonce         string    `json:"nonce"`
	FeeRateBps    string    `json:"feeRateBps"`   // camelCase for frontend
	Side          OrderSide `json:"side"`
	SignatureType int       `json:"signatureType"` // camelCase for frontend
	Signature     string    `json:"signature"`
}

// Validate ensures the Order struct contains the properties required by the CLOB API.
func (o *Order) Validate() error {
	if o == nil {
		return errors.New("order payload is required")
	}

	requiredFields := map[string]string{
		"salt":        o.Salt,
		"maker":       o.Maker,
		"signer":      o.Signer,
		"taker":       o.Taker,
		"tokenId":     o.TokenID,
		"makerAmount": o.MakerAmount,
		"takerAmount": o.TakerAmount,
		"expiration":  o.Expiration,
		"nonce":       o.Nonce,
		"feeRateBps":  o.FeeRateBps,
		"signature":   o.Signature,
	}

	for field, value := range requiredFields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("order.%s is required", field)
		}
	}

	sideTrim := strings.TrimSpace(string(o.Side))
	switch sideTrim {
	case "0":
		o.Side = OrderSide("0")
	case "1":
		o.Side = OrderSide("1")
	default:
		normalizedSide := OrderSide(strings.ToUpper(sideTrim))
		if _, ok := validOrderSides[normalizedSide]; !ok {
			return fmt.Errorf("order.side %q is invalid", o.Side)
		}
		o.Side = normalizedSide
	}

	if _, ok := validSignatureTypes[o.SignatureType]; !ok {
		return fmt.Errorf("order.signatureType %d is invalid", o.SignatureType)
	}

	return nil
}

// PostOrderRequest represents the payload for POST /order
// Uses camelCase to match CLOB API expectations (consistent with response fields like errorMsg, orderId)
type PostOrderRequest struct {
	DeferExec bool      `json:"deferExec"` // Whether to defer execution (default false for immediate validity)
	Order     Order     `json:"order"`
	Owner     string    `json:"owner"` // The API Key of the order owner (User)
	OrderType OrderType `json:"orderType"`
}

// Validate ensures the request conforms to the CLOB `POST /order` schema.
func (r *PostOrderRequest) Validate() error {
	if r == nil {
		return errors.New("post order request is required")
	}

	if err := r.Order.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(r.Owner) == "" {
		return errors.New("owner (API key) is required")
	}

	normalizedType := OrderType(strings.ToUpper(string(r.OrderType)))
	if _, ok := validOrderTypes[normalizedType]; !ok {
		return fmt.Errorf("orderType %q is invalid", r.OrderType)
	}
	r.OrderType = normalizedType

	return nil
}

// PostOrderResponse represents the success response from CLOB
type PostOrderResponse struct {
	Success     bool     `json:"success"`
	ErrorMsg    string   `json:"errorMsg"`
	OrderID     string   `json:"orderId"`
	OrderIDs    []string `json:"orderIds,omitempty"`
	OrderHashes []string `json:"orderHashes"` // Hashes of settlement transactions if matched
	Status      string   `json:"status,omitempty"`
}

// BookResponse represents the simplified order book snapshot returned by the CLOB API.
type BookResponse struct {
	Bids []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"bids"`
	Asks []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"asks"`
}

// PostOrdersRequest represents the payload for batch orders (POST /orders)
type PostOrdersRequest []PostOrderRequest

// Validate ensures every batched entry is valid.
func (r PostOrdersRequest) Validate() error {
	if len(r) == 0 {
		return errors.New("at least one order is required")
	}
	for idx := range r {
		if err := r[idx].Validate(); err != nil {
			return fmt.Errorf("order %d invalid: %w", idx, err)
		}
	}
	return nil
}

// ErrorResponse represents a generic error from CLOB
type ErrorResponse struct {
	Error string `json:"error"`
}

// CancelOrderRequest represents the payload for DELETE /order
type CancelOrderRequest struct {
	OrderID string `json:"orderID"`
}

// CancelOrdersRequest represents the payload for DELETE /orders
type CancelOrdersRequest struct {
	OrderIDs []string `json:"orderIds"`
}

// CancelResponse represents the response for cancellation endpoints
type CancelResponse struct {
	Canceled    []string          `json:"canceled"`
	NotCanceled map[string]string `json:"not_canceled"`
}
