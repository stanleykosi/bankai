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
	ID               string      `json:"id"`
	ConditionID      string      `json:"conditionId"`
	QuestionID       string      `json:"questionID"`
	Slug             string      `json:"slug"`
	Question         string      `json:"question"`
	ResolutionSource string      `json:"resolutionSource"`
	EndDate          string      `json:"endDate"` // ISO string
	Outcomes         interface{} `json:"outcomes"` // usually []string or stringified JSON
	OutcomePrices    interface{} `json:"outcomePrices"`
	Volume           interface{} `json:"volume"`
	Liquidity        interface{} `json:"liquidity"`
	Active           bool        `json:"active"`
	Closed           bool        `json:"closed"`
	GroupItemTitle   string      `json:"groupItemTitle"`
	ClobTokenIds     string      `json:"clobTokenIds"` // JSON string "[\"token1\", \"token2\"]"
}

// GammaTag represents a tag object
type GammaTag struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Slug  string `json:"slug"`
}

// ToDBModel converts a GammaMarket to our internal DB model
func (gm *GammaMarket) ToDBModel() *models.Market {
	// Parse timestamps
	var endDate *time.Time
	if t, err := time.Parse(time.RFC3339, gm.EndDate); err == nil {
		endDate = &t
	}

	// Parse volume/liquidity safely
	vol := parseFloatSafe(gm.Volume)
	liq := parseFloatSafe(gm.Liquidity)

	return &models.Market{
		ConditionID:     gm.ConditionID,
		QuestionID:      gm.QuestionID,
		Slug:            gm.Slug,
		Title:           gm.Question, // Market question is usually the title
		Description:     gm.ResolutionSource,
		ResolutionRules: gm.ResolutionSource, // Using source as rules proxy for now
		Active:          gm.Active,
		Closed:          gm.Closed,
		Archived:        false, // Will be set from event if needed
		Volume24h:       vol,
		Liquidity:       liq,
		EndDate:         endDate,
	}
}

func parseFloatSafe(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
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

