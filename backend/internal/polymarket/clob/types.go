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
)

// Order represents the signed EIP-712 order structure
type Order struct {
	Salt          string    `json:"salt"`
	Maker         string    `json:"maker"`
	Signer        string    `json:"signer"`
	Taker         string    `json:"taker"`
	TokenID       string    `json:"tokenId"`
	MakerAmount   string    `json:"makerAmount"`
	TakerAmount   string    `json:"takerAmount"`
	Expiration    string    `json:"expiration"`
	Nonce         string    `json:"nonce"`
	FeeRateBps    string    `json:"feeRateBps"`
	Side          OrderSide `json:"side"`
	SignatureType int       `json:"signatureType"` // 0=EOA, 1=PolyProxy, 2=GnosisSafe
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

	normalizedSide := OrderSide(strings.ToUpper(string(o.Side)))
	if _, ok := validOrderSides[normalizedSide]; !ok {
		return fmt.Errorf("order.side %q is invalid", o.Side)
	}
	o.Side = normalizedSide

	if o.SignatureType < 0 {
		return fmt.Errorf("order.signatureType %d is invalid", o.SignatureType)
	}

	return nil
}

// PostOrderRequest represents the payload for POST /order
type PostOrderRequest struct {
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
