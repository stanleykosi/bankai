/**
 * @description
 * Service for managing trade execution.
 * Bridges the API layer with the CLOB client and Database persistence.
 *
 * @dependencies
 * - backend/internal/polymarket/clob
 * - backend/internal/models
 * - gorm.io/gorm
 */

package services

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TradeService struct {
	DB   *gorm.DB
	Clob *clob.Client
}

func NewTradeService(db *gorm.DB, clobClient *clob.Client) *TradeService {
	return &TradeService{
		DB:   db,
		Clob: clobClient,
	}
}

// RelayTrade derives user API credentials on-demand, relays the order with both user (L2) and builder headers,
// and persists the order record to the database.
func (s *TradeService) RelayTrade(ctx context.Context, user *models.User, req *clob.PostOrderRequest, auth *clob.ClobAuthProof) (*clob.PostOrderResponse, error) {
	if req == nil {
		return nil, errors.New("post order request is required")
	}
	if user == nil {
		return nil, errors.New("user context is required")
	}
	if auth == nil {
		return nil, errors.New("auth proof is required")
	}
	if strings.TrimSpace(user.VaultAddress) == "" {
		return nil, fmt.Errorf("user has no deployed vault on file")
	}

	if !strings.EqualFold(strings.TrimSpace(req.Order.Maker), strings.TrimSpace(user.VaultAddress)) {
		return nil, fmt.Errorf("order maker %s does not match vault %s", req.Order.Maker, user.VaultAddress)
	}

	// 0. Basic numeric validations against market metadata (tick size, min size).
	if err := s.validateOrderAmounts(ctx, &req.Order); err != nil {
		return nil, fmt.Errorf("invalid order payload (amounts): %w", err)
	}

	// 1. Derive/obtain the user's API credentials (do not persist).
	creds, err := s.Clob.DeriveAPIKey(ctx, auth)
	if err != nil {
		return nil, fmt.Errorf("failed to derive user api key: %w", err)
	}

	// Recover signer locally to pinpoint signature mismatches before hitting CLOB
	if recov, digest, err := recoverOrderSigner(&req.Order); err != nil {
		logger.Info("Order signature recovery failed: %v", err)
	} else {
		logger.Info("Order signature recovery: signer=%s recovered=%s digest=%s", req.Order.Signer, recov.Hex(), digest.Hex())
	}

	// Inject owner with the user API key as required by CLOB
	req.Owner = creds.Key
	// Explicitly ensure deferExec is set (default immediate execution path)
	req.DeferExec = false

	// Validate after owner is injected
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid order payload (client): %w", err)
	}

	// Log the outbound payload for debugging against the TS client
	if b, merr := json.Marshal(req); merr == nil {
		logger.Info("Posting order payload: %s", string(b))
		logger.Info("Order debug: signatureType=%d side=%s expiration=%s tokenId=%s owner=%s", req.Order.SignatureType, req.Order.Side, req.Order.Expiration, req.Order.TokenID, req.Owner)
	}

	// 2. Post to CLOB with both user (L2) and builder headers.
	resp, err := s.Clob.PostOrder(ctx, req, creds)
	if err != nil {
		return nil, fmt.Errorf("failed to relay trade: %w", err)
	}

	if err := s.persistOrder(ctx, user, req, resp); err != nil {
		logger.Error("Failed to persist order for user %s: %v", user.ID, err)
	}

	if !resp.Success {
		return resp, fmt.Errorf("clob rejected order: %s", resp.ErrorMsg)
	}
	return resp, nil
}

func (s *TradeService) persistOrder(ctx context.Context, user *models.User, req *clob.PostOrderRequest, resp *clob.PostOrderResponse) error {
	if resp == nil {
		return errors.New("clob response cannot be nil")
	}

	orderID := resp.OrderID
	if orderID == "" && len(resp.OrderIDs) == 1 {
		orderID = resp.OrderIDs[0]
	}

	// Parse numeric fields
	makerAmt, err := strconv.ParseFloat(req.Order.MakerAmount, 64)
	if err != nil {
		return fmt.Errorf("invalid makerAmount: %w", err)
	}
	takerAmt, err := strconv.ParseFloat(req.Order.TakerAmount, 64)
	if err != nil {
		return fmt.Errorf("invalid takerAmount: %w", err)
	}

	// Try to find market by token ID to determine outcome label
	var market models.Market
	var marketFound bool
	err = s.DB.WithContext(ctx).
		Where("token_id_yes = ? OR token_id_no = ?", req.Order.TokenID, req.Order.TokenID).
		First(&market).Error
	if err == nil {
		marketFound = true
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.Error("Failed to lookup market for token %s: %v", req.Order.TokenID, err)
	}

	outcomeLabel := deriveOutcomeLabel(func() *models.Market {
		if marketFound {
			return &market
		}
		return nil
	}(), req.Order.TokenID, req.Order.Side)

	// Calculate price and size from atomic units
	var dbPrice, dbSize float64
	if req.Order.Side == clob.BUY {
		if takerAmt > 0 {
			dbPrice = makerAmt / takerAmt
		}
		dbSize = takerAmt / 1e6
	} else {
		if makerAmt > 0 {
			dbPrice = takerAmt / makerAmt
		}
		dbSize = makerAmt / 1e6
	}

	order := models.Order{
		UserID:         user.ID,
		CLOBOrderID:    orderID,
		Side:           models.OrderSide(req.Order.Side),
		Outcome:        outcomeLabel,
		OutcomeTokenID: req.Order.TokenID,
		Price:          dbPrice,
		Size:           dbSize,
		OrderType:      string(req.OrderType),
		Status:         mapClobStatus(resp, req.OrderType),
		StatusDetail:   strings.ToLower(resp.Status),
		OrderHashes:    models.StringArray(resp.OrderHashes),
		ErrorMessage:   resp.ErrorMsg,
	}

	if marketFound {
		order.MarketID = market.ConditionID
	}

	return s.DB.WithContext(ctx).Create(&order).Error
}

// validateOrderAmounts checks price/size alignment to market tick & min-size rules to catch payload issues pre-flight.
func (s *TradeService) validateOrderAmounts(ctx context.Context, order *clob.Order) error {
	if order == nil {
		return errors.New("order is nil")
	}

	// Fetch minimal market metadata for tick/min size and neg-risk context.
	var market models.Market
	var hasMarket bool
	if err := s.DB.WithContext(ctx).
		Where("token_id_yes = ? OR token_id_no = ?", order.TokenID, order.TokenID).
		First(&market).Error; err == nil {
		hasMarket = true
	}

	// Parse amounts (all are integer strings representing 1e6 base units).
	makerAmt, err := strconv.ParseFloat(order.MakerAmount, 64)
	if err != nil {
		return fmt.Errorf("makerAmount parse: %w", err)
	}
	takerAmt, err := strconv.ParseFloat(order.TakerAmount, 64)
	if err != nil {
		return fmt.Errorf("takerAmount parse: %w", err)
	}
	if makerAmt <= 0 || takerAmt <= 0 {
		return fmt.Errorf("maker/taker amounts must be > 0")
	}

	// Compute price from amounts (shares priced in USDC, 1e6 decimals).
	var price float64
	switch order.Side {
	case clob.BUY:
		price = makerAmt / takerAmt
	case clob.SELL:
		price = takerAmt / makerAmt
	default:
		return fmt.Errorf("unsupported side %s", order.Side)
	}

	// Default tick sizes and min-size if we lack metadata.
	tickSize := 0.01
	minSize := 1.0
	if hasMarket {
		if market.OrderPriceMinTickSize > 0 {
			tickSize = market.OrderPriceMinTickSize
		}
		if market.OrderMinSize > 0 {
			minSize = market.OrderMinSize
		}
	}

	// Validate min size (shares are taker for BUY, maker for SELL, measured in full units not atomic).
	shares := takerAmt / 1e6
	if order.Side == clob.SELL {
		shares = makerAmt / 1e6
	}
	if shares < minSize {
		return fmt.Errorf("size %.4f below min size %.4f", shares, minSize)
	}

	// Validate price range and tick.
	if price <= 0 || price >= 1 {
		return fmt.Errorf("price %.6f out of bounds (0,1)", price)
	}
	if !isOnTick(price, tickSize) {
		return fmt.Errorf("price %.6f not aligned to tick %.4f", price, tickSize)
	}

	return nil
}

// isOnTick returns true if value is within a tiny epsilon of tick grid.
func isOnTick(val, tick float64) bool {
	if tick <= 0 {
		return true
	}
	steps := val / tick
	nearest := math.Round(steps)
	return math.Abs(steps-nearest) < 1e-6
}

// recoverOrderSigner computes the EIP-712 order digest and recovers the signer address.
// This helps pinpoint signature mismatches before posting to CLOB (which only returns a generic 400).
func recoverOrderSigner(order *clob.Order) (common.Address, common.Hash, error) {
	if order == nil {
		return common.Address{}, common.Hash{}, errors.New("order is nil")
	}

	// Constants aligned with frontend signing (Polymarket CTF Exchange on Polygon mainnet).
	const domainName = "Polymarket CTF Exchange"
	const domainVersion = "1"
	const chainID = 137
	const verifyingContract = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"

	// Type hashes
	typeHashDomain := crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	typeHashOrder := crypto.Keccak256Hash([]byte("Order(uint256 salt,address maker,address signer,address taker,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint256 expiration,uint256 nonce,uint256 feeRateBps,uint8 side)"))

	// Domain separator
	domainSeparator := crypto.Keccak256Hash(
		typeHashDomain.Bytes(),
		crypto.Keccak256Hash([]byte(domainName)).Bytes(),
		crypto.Keccak256Hash([]byte(domainVersion)).Bytes(),
		padUint256(chainID),
		padAddress(common.HexToAddress(verifyingContract)),
	)

	// Side as uint8
	side := uint8(0)
	switch strings.ToUpper(strings.TrimSpace(string(order.Side))) {
	case "SELL", "1":
		side = 1
	}

	structHash := crypto.Keccak256Hash(
		typeHashOrder.Bytes(),
		padBig(order.Salt),
		padAddress(common.HexToAddress(order.Maker)),
		padAddress(common.HexToAddress(order.Signer)),
		padAddress(common.HexToAddress(order.Taker)),
		padBig(order.TokenID),
		padBig(order.MakerAmount),
		padBig(order.TakerAmount),
		padBig(order.Expiration),
		padBig(order.Nonce),
		padBig(order.FeeRateBps),
		padUint8(side),
	)

	digest := crypto.Keccak256Hash(
		[]byte{0x19, 0x01},
		domainSeparator.Bytes(),
		structHash.Bytes(),
	)

	sigBytes, err := hexToBytes(order.Signature)
	if err != nil {
		return common.Address{}, digest, fmt.Errorf("invalid signature hex: %w", err)
	}
	if len(sigBytes) != 65 {
		return common.Address{}, digest, fmt.Errorf("invalid signature length: %d (expected 65)", len(sigBytes))
	}
	vRaw := sigBytes[64]
	// go-ethereum SigToPub expects V as 27/28; ethers often returns 0/1
	if sigBytes[64] == 0 || sigBytes[64] == 1 {
		sigBytes[64] += 27
	}
	vNorm := sigBytes[64]
	if vNorm != 27 && vNorm != 28 {
		return common.Address{}, digest, fmt.Errorf("invalid recovery id (v): %d (raw=%d)", vNorm, vRaw)
	}

	// Extra diagnostics: check r/s and v against validation rules
	r := new(big.Int).SetBytes(sigBytes[0:32])
	s := new(big.Int).SetBytes(sigBytes[32:64])
	vCheck := byte(0)
	if vNorm >= 27 {
		vCheck = vNorm - 27 // ValidateSignatureValues expects 0/1
	}
	valid := crypto.ValidateSignatureValues(vCheck, r, s, true)
	if !valid {
		return common.Address{}, digest, fmt.Errorf("signature components invalid: v(raw=%d norm=%d check=%d) r=%s s=%s", vRaw, vNorm, vCheck, r.Text(16), s.Text(16))
	}

	pubKey, err := crypto.SigToPub(digest.Bytes(), sigBytes)
	if err != nil {
		return common.Address{}, digest, fmt.Errorf(
			"recover failed: %w (len=%d vRaw=%d vNorm=%d r=%s s=%s digest=%s)",
			err, len(sigBytes), vRaw, vNorm, r.Text(16), s.Text(16), digest.Hex(),
		)
	}
	return crypto.PubkeyToAddress(*pubKey), digest, nil
}

func padBig(val string) []byte {
	bi := new(big.Int)
	bi.SetString(strings.TrimSpace(val), 10)
	return padUint256Big(bi)
}

func padUint256(v int) []byte {
	return padUint256Big(big.NewInt(int64(v)))
}

func padUint8(v uint8) []byte {
	return padUint256Big(big.NewInt(int64(v)))
}

func padUint256Big(bi *big.Int) []byte {
	if bi == nil {
		bi = big.NewInt(0)
	}
	return common.LeftPadBytes(bi.Bytes(), 32)
}

func padAddress(addr common.Address) []byte {
	return common.LeftPadBytes(addr.Bytes(), 32)
}

func hexToBytes(sig string) ([]byte, error) {
	s := strings.TrimPrefix(sig, "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}

func mapClobStatus(resp *clob.PostOrderResponse, orderType clob.OrderType) models.OrderStatus {
	if resp == nil {
		return models.OrderStatusFailed
	}
	if !resp.Success {
		return models.OrderStatusFailed
	}

	switch strings.ToLower(resp.Status) {
	case "matched":
		return models.OrderStatusFilled
	case "live":
		return models.OrderStatusOpen
	case "delayed":
		return models.OrderStatusPending
	case "unmatched":
		return models.OrderStatusOpen
	}

	// FOK orders that succeed without status default to filled, FAK to open
	if orderType == clob.OrderTypeFOK {
		return models.OrderStatusFilled
	}
	return models.OrderStatusOpen
}

type outcomeMeta struct {
	Index   int
	Label   string
	TokenID string
}

func deriveOutcomeLabel(market *models.Market, tokenID string, side clob.OrderSide) string {
	rawTokenID := strings.TrimSpace(tokenID)
	tokenID = strings.ToLower(rawTokenID)

	var metas []outcomeMeta
	if market != nil {
		metas = parseMarketOutcomeMetadata(market.Outcomes)

		// Direct tokenID match from metadata
		for _, meta := range metas {
			if meta.TokenID != "" && tokenID != "" && strings.EqualFold(meta.TokenID, tokenID) {
				if lbl := meta.LabelOrFallback(); lbl != "" {
					return lbl
				}
			}
		}

		if tokenID != "" {
			if market.TokenIDYes != "" && strings.EqualFold(strings.ToLower(market.TokenIDYes), tokenID) {
				if lbl := labelForOutcomeIndex(metas, 0); lbl != "" {
					return lbl
				}
				return "YES"
			}
			if market.TokenIDNo != "" && strings.EqualFold(strings.ToLower(market.TokenIDNo), tokenID) {
				if lbl := labelForOutcomeIndex(metas, 1); lbl != "" {
					return lbl
				}
				return "NO"
			}
		}
	}

	if rawTokenID != "" {
		return rawTokenID
	}
	if side != "" {
		return strings.ToUpper(string(side))
	}
	return "UNKNOWN"
}

func parseMarketOutcomeMetadata(raw string) []outcomeMeta {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}

	metas := make([]outcomeMeta, 0, len(entries))
	for idx, entry := range entries {
		var str string
		if err := json.Unmarshal(entry, &str); err == nil {
			if trimmed := strings.TrimSpace(str); trimmed != "" {
				metas = append(metas, outcomeMeta{
					Index: idx,
					Label: trimmed,
				})
				continue
			}
		}

		var obj map[string]interface{}
		if err := json.Unmarshal(entry, &obj); err == nil {
			meta := outcomeMeta{
				Index: idx,
				TokenID: firstNonEmptyString(
					interfaceToString(obj["tokenId"]),
					interfaceToString(obj["tokenID"]),
					interfaceToString(obj["token_id"]),
					interfaceToString(obj["id"]),
				),
				Label: firstNonEmptyString(
					interfaceToString(obj["label"]),
					interfaceToString(obj["name"]),
					interfaceToString(obj["shortName"]),
					interfaceToString(obj["title"]),
					interfaceToString(obj["outcome"]),
					interfaceToString(obj["value"]),
				),
			}
			if meta.Label == "" && meta.TokenID != "" {
				meta.Label = meta.TokenID
			}
			if meta.Label != "" || meta.TokenID != "" {
				metas = append(metas, meta)
			}
		}
	}

	return metas
}

func (o outcomeMeta) LabelOrFallback() string {
	if o.Label != "" {
		return o.Label
	}
	return o.TokenID
}

func labelForOutcomeIndex(metas []outcomeMeta, idx int) string {
	for _, meta := range metas {
		if meta.Index == idx && meta.Label != "" {
			return meta.Label
		}
	}
	return ""
}

func interfaceToString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	case int:
		return strings.TrimSpace(strconv.Itoa(int(v)))
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// RelayBatchTrades iterates through the provided PostOrderRequests and relays each one sequentially.
// Using single-order submission ensures we receive deterministic order IDs for persistence.
func (s *TradeService) RelayBatchTrades(ctx context.Context, user *models.User, requests []*clob.PostOrderRequest, auth *clob.ClobAuthProof) ([]*clob.PostOrderResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	if auth == nil {
		return nil, errors.New("auth proof is required")
	}
	if len(requests) == 0 {
		return nil, errors.New("at least one order is required")
	}

	creds, err := s.Clob.DeriveAPIKey(ctx, auth)
	if err != nil {
		return nil, fmt.Errorf("failed to derive user api key: %w", err)
	}

	responses := make([]*clob.PostOrderResponse, 0, len(requests))
	for idx, req := range requests {
		req.Owner = creds.Key
		if err := req.Validate(); err != nil {
			return nil, fmt.Errorf("order %d invalid: %w", idx, err)
		}
		resp, err := s.Clob.PostOrder(ctx, req, creds)
		if err != nil {
			return nil, fmt.Errorf("batch order %d failed: %w", idx, err)
		}
		responses = append(responses, resp)
	}

	return responses, nil
}

// CancelOrder cancels an existing order on the CLOB and updates persistence.
func (s *TradeService) CancelOrder(ctx context.Context, user *models.User, orderID string) (*clob.CancelResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, errors.New("orderID is required")
	}

	var order models.Order
	if err := s.DB.WithContext(ctx).
		Where("user_id = ? AND clob_order_id = ?", user.ID, orderID).
		First(&order).Error; err != nil {
		return nil, fmt.Errorf("order not found or not owned by user: %w", err)
	}

	resp, err := s.Clob.CancelOrder(ctx, &clob.CancelOrderRequest{OrderID: orderID}, nil)
	if err != nil {
		return nil, err
	}

	if len(resp.Canceled) > 0 {
		if err := s.DB.WithContext(ctx).Model(&order).Updates(map[string]interface{}{
			"status":        models.OrderStatusCanceled,
			"status_detail": "canceled",
		}).Error; err != nil {
			logger.Error("Failed to update order %s cancellation status: %v", orderID, err)
		}
	}

	return resp, nil
}

// CancelOrders cancels multiple orders for the user and updates their status.
func (s *TradeService) CancelOrders(ctx context.Context, user *models.User, orderIDs []string) (*clob.CancelResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	if len(orderIDs) == 0 {
		return nil, errors.New("at least one orderID is required")
	}

	// Ensure all orders belong to the user
	var count int64
	if err := s.DB.WithContext(ctx).Model(&models.Order{}).
		Where("user_id = ? AND clob_order_id IN ?", user.ID, orderIDs).
		Count(&count).Error; err != nil {
		return nil, fmt.Errorf("failed to verify order ownership: %w", err)
	}
	if count != int64(len(orderIDs)) {
		return nil, fmt.Errorf("one or more orders do not belong to the user")
	}

	resp, err := s.Clob.CancelOrders(ctx, &clob.CancelOrdersRequest{OrderIDs: orderIDs}, nil)
	if err != nil {
		return nil, err
	}

	if len(resp.Canceled) > 0 {
		if err := s.DB.WithContext(ctx).Model(&models.Order{}).
			Where("user_id = ? AND clob_order_id IN ?", user.ID, resp.Canceled).
			Updates(map[string]interface{}{
				"status":        models.OrderStatusCanceled,
				"status_detail": "canceled",
			}).Error; err != nil {
			logger.Error("Failed to update canceled orders for user %s: %v", user.ID, err)
		}
	}

	return resp, nil
}

// ListOrders returns paginated order history for a user.
func (s *TradeService) ListOrders(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.Order, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	query := s.DB.WithContext(ctx).Model(&models.Order{}).
		Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count orders: %w", err)
	}

	var orders []models.Order
	if err := query.Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&orders).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to fetch orders: %w", err)
	}

	return orders, total, nil
}
