/**
 * @description
 * Type definitions for the Polymarket Gamma API responses.
 * These structs map to the JSON returned by endpoints like /events and /markets.
 */

package gamma

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/bankai-project/backend/internal/models"
)

// GammaEvent represents an event object from the Gamma API
type GammaEvent struct {
	ID          string        `json:"id"`
	Slug        string        `json:"slug"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	StartDate   string        `json:"startDate"` // Gamma returns ISO strings
	EndDate     string        `json:"endDate"`
	Image       string        `json:"image"`
	Icon        string        `json:"icon"`
	Active      bool          `json:"active"`
	Closed      bool          `json:"closed"`
	Archived    bool          `json:"archived"`
	Markets     []GammaMarket `json:"markets"`
	Tags        []GammaTag    `json:"tags"`
	Volume      interface{}   `json:"volume"`    // Can be string or number
	Liquidity   interface{}   `json:"liquidity"` // Can be string or number
}

// GammaMarket represents a market object from the Gamma API
type GammaMarket struct {
	ID                       string      `json:"id"`
	ConditionID              string      `json:"conditionId"`
	QuestionID               string      `json:"questionID"`
	Slug                     string      `json:"slug"`
	Question                 string      `json:"question"`
	Description              string      `json:"description"`
	ResolutionSource         string      `json:"resolutionSource"`
	StartDate                string      `json:"startDate"`
	EndDate                  string      `json:"endDate"` // ISO string
	EventStartTime           string      `json:"eventStartTime"`
	Image                    string      `json:"image"`
	Icon                     string      `json:"icon"`
	Outcomes                 string      `json:"outcomes"` // JSON encoded array
	OutcomePrices            string      `json:"outcomePrices"`
	Active                   bool        `json:"active"`
	Closed                   bool        `json:"closed"`
	Archived                 bool        `json:"archived"`
	Featured                 bool        `json:"featured"`
	New                      bool        `json:"new"`
	Restricted               bool        `json:"restricted"`
	EnableOrderBook          bool        `json:"enableOrderBook"`
	AutomaticallyActive      bool        `json:"automaticallyActive"`
	ManualActivation         bool        `json:"manualActivation"`
	AcceptingOrders          bool        `json:"acceptingOrders"`
	AcceptingOrdersTimestamp string      `json:"acceptingOrdersTimestamp"`
	Ready                    bool        `json:"ready"`
	Funded                   bool        `json:"funded"`
	PendingDeployment        bool        `json:"pendingDeployment"`
	Deploying                bool        `json:"deploying"`
	RFQEnabled               bool        `json:"rfqEnabled"`
	HoldingRewardsEnabled    bool        `json:"holdingRewardsEnabled"`
	FeesEnabled              bool        `json:"feesEnabled"`
	NegRisk                  bool        `json:"negRisk"`
	NegRiskOther             bool        `json:"negRiskOther"`
	MarketMakerAddress       string      `json:"marketMakerAddress"`
	GroupItemThreshold       interface{} `json:"groupItemThreshold"`
	ClobTokenIds             string      `json:"clobTokenIds"` // JSON string "[\"token1\", \"token2\"]"

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`

	OrderPriceMinTickSize interface{} `json:"orderPriceMinTickSize"`
	OrderMinSize          interface{} `json:"orderMinSize"`

	Volume         interface{} `json:"volume"`
	VolumeAmm      interface{} `json:"volumeAmm"`
	VolumeClob     interface{} `json:"volumeClob"`
	VolumeNum      interface{} `json:"volumeNum"`
	Volume24hr     interface{} `json:"volume24hr"`
	Volume24hrAmm  interface{} `json:"volume24hrAmm"`
	Volume24hrClob interface{} `json:"volume24hrClob"`
	Volume1wk      interface{} `json:"volume1wk"`
	Volume1wkAmm   interface{} `json:"volume1wkAmm"`
	Volume1wkClob  interface{} `json:"volume1wkClob"`
	Volume1mo      interface{} `json:"volume1mo"`
	Volume1moAmm   interface{} `json:"volume1moAmm"`
	Volume1moClob  interface{} `json:"volume1moClob"`
	Volume1yr      interface{} `json:"volume1yr"`
	Volume1yrAmm   interface{} `json:"volume1yrAmm"`
	Volume1yrClob  interface{} `json:"volume1yrClob"`

	Liquidity     interface{} `json:"liquidity"`
	LiquidityAmm  interface{} `json:"liquidityAmm"`
	LiquidityClob interface{} `json:"liquidityClob"`
	LiquidityNum  interface{} `json:"liquidityNum"`

	BestBid             interface{} `json:"bestBid"`
	BestAsk             interface{} `json:"bestAsk"`
	Spread              interface{} `json:"spread"`
	LastTradePrice      interface{} `json:"lastTradePrice"`
	OneHourPriceChange  interface{} `json:"oneHourPriceChange"`
	OneDayPriceChange   interface{} `json:"oneDayPriceChange"`
	OneWeekPriceChange  interface{} `json:"oneWeekPriceChange"`
	OneMonthPriceChange interface{} `json:"oneMonthPriceChange"`
	OneYearPriceChange  interface{} `json:"oneYearPriceChange"`

	RewardsMinSize   interface{} `json:"rewardsMinSize"`
	RewardsMaxSpread interface{} `json:"rewardsMaxSpread"`
	Competitive      interface{} `json:"competitive"`
}

// GammaTag represents a tag object
type GammaTag struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Slug  string `json:"slug"`
}

// Profile represents a Polymarket user profile returned by /public-search
// Includes both the controlling EOA (baseAddress) and the custodial vault/proxy wallet.
type Profile struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	BaseAddress      string `json:"baseAddress"`
	ProxyWallet      string `json:"proxyWallet"`
	Pseudonym        string `json:"pseudonym"`
	DisplayPublic    bool   `json:"displayUsernamePublic"`
	WalletActivated  bool   `json:"walletActivated"`
	User             int    `json:"user"`
	Referral         string `json:"referral"`
	ProfileImage     string `json:"profileImage"`
	Bio              string `json:"bio"`
	IsCloseOnly      bool   `json:"isCloseOnly"`
	IsCertReq        bool   `json:"isCertReq"`
	CertReqDate      string `json:"certReqDate"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

// SearchResponse represents the Gamma /public-search payload
type SearchResponse struct {
	Profiles []Profile `json:"profiles"`
}

// ToDBModel converts a GammaMarket to our internal DB model
func (gm *GammaMarket) ToDBModel() *models.Market {
	startDate := parseTimePtr(gm.StartDate)
	endDate := parseTimePtr(gm.EndDate)
	eventStart := parseTimePtr(gm.EventStartTime)
	createdAt := parseTimePtr(gm.CreatedAt)
	updatedAt := parseTimePtr(gm.UpdatedAt)
	acceptingOrdersAt := parseTimePtr(gm.AcceptingOrdersTimestamp)

	// Parse volume/liquidity safely
	vol := parseFloatSafe(gm.Volume)
	liq := parseFloatSafe(gm.Liquidity)

	resolutionRules := gm.Description
	switch {
	case resolutionRules == "" && gm.ResolutionSource != "":
		resolutionRules = gm.ResolutionSource
	case resolutionRules != "" && gm.ResolutionSource != "":
		resolutionRules = resolutionRules + "\n\nSource: " + gm.ResolutionSource
	}

	return &models.Market{
		ConditionID:           gm.ConditionID,
		GammaMarketID:         gm.ID,
		QuestionID:            gm.QuestionID,
		Slug:                  gm.Slug,
		Title:                 gm.Question, // Market question is usually the title
		Description:           gm.Description,
		ResolutionRules:       resolutionRules,
		ImageURL:              gm.Image,
		IconURL:               gm.Icon,
		StartDate:             startDate,
		Active:                gm.Active,
		Closed:                gm.Closed,
		Archived:              gm.Archived,
		Featured:              gm.Featured,
		IsNew:                 gm.New,
		Restricted:            gm.Restricted,
		EnableOrderBook:       gm.EnableOrderBook,
		MarketMakerAddr:       gm.MarketMakerAddress,
		EventStartTime:        eventStart,
		AcceptingOrders:       gm.AcceptingOrders,
		AcceptingOrdersAt:     acceptingOrdersAt,
		Ready:                 gm.Ready,
		Funded:                gm.Funded,
		PendingDeployment:     gm.PendingDeployment,
		Deploying:             gm.Deploying,
		RFQEnabled:            gm.RFQEnabled,
		HoldingRewardsEnabled: gm.HoldingRewardsEnabled,
		FeesEnabled:           gm.FeesEnabled,
		NegRisk:               gm.NegRisk,
		NegRiskOther:          gm.NegRiskOther,
		AutomaticallyActive:   gm.AutomaticallyActive,
		ManualActivation:      gm.ManualActivation,
		VolumeAllTime:         vol,
		Volume24h:             parseFloatSafe(gm.Volume24hr),
		Volume24hAmm:          parseFloatSafe(gm.Volume24hrAmm),
		Volume24hClob:         parseFloatSafe(gm.Volume24hrClob),
		Volume1Week:           parseFloatSafe(gm.Volume1wk),
		Volume1WeekAmm:        parseFloatSafe(gm.Volume1wkAmm),
		Volume1WeekClob:       parseFloatSafe(gm.Volume1wkClob),
		Volume1Month:          parseFloatSafe(gm.Volume1mo),
		Volume1MonthAmm:       parseFloatSafe(gm.Volume1moAmm),
		Volume1MonthClob:      parseFloatSafe(gm.Volume1moClob),
		Volume1Year:           parseFloatSafe(gm.Volume1yr),
		Volume1YearAmm:        parseFloatSafe(gm.Volume1yrAmm),
		Volume1YearClob:       parseFloatSafe(gm.Volume1yrClob),
		VolumeAmm:             parseFloatSafe(gm.VolumeAmm),
		VolumeClob:            parseFloatSafe(gm.VolumeClob),
		VolumeNum:             parseFloatSafe(gm.VolumeNum),
		Liquidity:             liq,
		LiquidityNum:          parseFloatSafe(gm.LiquidityNum),
		LiquidityClob:         parseFloatSafe(gm.LiquidityClob),
		LiquidityAmm:          parseFloatSafe(gm.LiquidityAmm),
		OrderMinSize:          parseFloatSafe(gm.OrderMinSize),
		OrderPriceMinTickSize: parseFloatSafe(gm.OrderPriceMinTickSize),
		BestBid:               parseFloatSafe(gm.BestBid),
		BestAsk:               parseFloatSafe(gm.BestAsk),
		Spread:                parseFloatSafe(gm.Spread),
		LastTradePrice:        parseFloatSafe(gm.LastTradePrice),
		OneHourPriceChange:    parseFloatSafe(gm.OneHourPriceChange),
		OneDayPriceChange:     parseFloatSafe(gm.OneDayPriceChange),
		OneWeekPriceChange:    parseFloatSafe(gm.OneWeekPriceChange),
		OneMonthPriceChange:   parseFloatSafe(gm.OneMonthPriceChange),
		OneYearPriceChange:    parseFloatSafe(gm.OneYearPriceChange),
		Competitive:           parseFloatSafe(gm.Competitive),
		RewardsMinSize:        parseFloatSafe(gm.RewardsMinSize),
		RewardsMaxSpread:      parseFloatSafe(gm.RewardsMaxSpread),
		Outcomes:              gm.Outcomes,
		OutcomePrices:         gm.OutcomePrices,
		MarketCreatedAt:       createdAt,
		MarketUpdatedAt:       updatedAt,
		EndDate:               endDate,
	}
}

func parseFloatSafe(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case json.Number:
		f, _ := val.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

func parseTimePtr(value string) *time.Time {
	if value == "" {
		return nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return &t
		}
	}
	return nil
}

// ParseTokenIDs parses the clobTokenIds JSON string
// Input: "[\"123...\", \"456...\"]"
// Returns: (tokenIDYes, tokenIDNo)
func ParseTokenIDs(jsonStr string) (string, string) {
	if jsonStr == "" {
		return "", ""
	}

	var tokens []string
	if err := json.Unmarshal([]byte(jsonStr), &tokens); err != nil {
		// If unmarshaling fails, return empty strings
		return "", ""
	}

	if len(tokens) >= 2 {
		// For binary markets, typically index 0 is YES, index 1 is NO
		return tokens[0], tokens[1]
	} else if len(tokens) == 1 {
		// Only one token found, assume it's YES
		return tokens[0], ""
	}

	return "", ""
}
