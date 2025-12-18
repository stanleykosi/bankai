/**
 * @description
 * Oracle Service.
 * Implements the RAG (Retrieval-Augmented Generation) pipeline:
 * 1. Fetch Market Context (DB)
 * 2. Fetch External News (Tavily)
 * 3. Synthesize & Predict (OpenAI)
 *
 * @dependencies
 * - backend/internal/integrations/tavily
 * - backend/internal/integrations/openai
 * - backend/internal/services (MarketService)
 */

package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/integrations/openai"
	"github.com/bankai-project/backend/internal/integrations/tavily"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
)

const (
	maxSourceEntries       = 5  // Use all 5 Tavily results for maximum context
	maxSourceContentLength = 0  // 0 = no truncation, send full content
	maxRulesLength         = 0  // 0 = no truncation, send full resolution rules
	maxDescriptionLength   = 0  // 0 = no truncation, send full description
)

var ErrMarketNotFound = errors.New("market not found")

type OracleService struct {
	MarketService *MarketService
	Tavily        *tavily.Client
	OpenAI        *openai.Client
}

type MarketAnalysis struct {
	MarketID    string   `json:"market_id"`
	Question    string   `json:"question"`
	Probability float64  `json:"probability"` // 0.0 to 1.0
	Sentiment   string   `json:"sentiment"`   // "Bullish", "Bearish", "Neutral", "Uncertain"
	Reasoning   string   `json:"reasoning"`   // Concise explanation
	Sources     []Source `json:"sources"`     // Citations
	LastUpdated string   `json:"last_updated"`
}

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

func NewOracleService(ms *MarketService, tavilyClient *tavily.Client, openaiClient *openai.Client) *OracleService {
	return &OracleService{
		MarketService: ms,
		Tavily:        tavilyClient,
		OpenAI:        openaiClient,
	}
}

// AnalyzeMarket performs a full RAG analysis on a market
func (s *OracleService) AnalyzeMarket(ctx context.Context, conditionID string) (*MarketAnalysis, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil, fmt.Errorf("condition_id is required")
	}

	// 1. Fetch Market Metadata
	market, err := s.MarketService.GetMarketByConditionID(ctx, conditionID)
	if err != nil {
		return nil, err
	}
	if market == nil {
		return nil, ErrMarketNotFound
	}

	logger.Info("Oracle analyzing market: %s", market.Title)

	// 2. Search for Information
	query := fmt.Sprintf("%s latest news analysis", market.Title)
	searchResults, err := s.Tavily.Search(ctx, query)
	if err != nil {
		logger.Error("Oracle search failed for %s: %v", conditionID, err)
		searchResults = nil
	}
	logger.Info("Oracle search completed for %s | query=%q | results=%d", conditionID, query, len(searchResults))

	// 3. Construct LLM Context
	// Note: Market data sources:
	// - DB fields: title, description, rules, dates, volume, liquidity, price changes, BestBid/BestAsk (overall market)
	// - Redis fields (via attachRealtimePrices): YesPrice, NoPrice, YesBestBid/Ask, NoBestBid/Ask (token-specific)
	// - TrendingScore: NOT available for single market fetches (only computed for market lanes)
	contextBuilder := strings.Builder{}
	contextBuilder.WriteString("=== MARKET INFORMATION ===\n")
	contextBuilder.WriteString(fmt.Sprintf("Title: %s\n", market.Title))
	
	if desc := strings.TrimSpace(market.Description); desc != "" {
		// No truncation - send full description for maximum context
		contextBuilder.WriteString(fmt.Sprintf("Description: %s\n", desc))
	}
	
	if rules := strings.TrimSpace(market.ResolutionRules); rules != "" {
		// No truncation - send full resolution rules for maximum context
		contextBuilder.WriteString(fmt.Sprintf("Resolution Rules: %s\n", rules))
	}
	
	if market.Category != "" {
		contextBuilder.WriteString(fmt.Sprintf("Category: %s\n", market.Category))
	}
	
	if len(market.Tags) > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(market.Tags, ", ")))
	}
	
	if market.Outcomes != "" {
		contextBuilder.WriteString(fmt.Sprintf("Outcomes: %s\n", market.Outcomes))
	}
	
	// Date information
	if market.StartDate != nil {
		contextBuilder.WriteString(fmt.Sprintf("Start Date: %s\n", market.StartDate.Format(time.RFC3339)))
	}
	if market.EndDate != nil {
		contextBuilder.WriteString(fmt.Sprintf("End Date: %s\n", market.EndDate.Format(time.RFC3339)))
	}
	if market.EventStartTime != nil {
		contextBuilder.WriteString(fmt.Sprintf("Event Start Time: %s\n", market.EventStartTime.Format(time.RFC3339)))
	}
	
	// Market status
	statusParts := []string{}
	if market.Closed {
		statusParts = append(statusParts, "Closed")
	}
	if market.Archived {
		statusParts = append(statusParts, "Archived")
	}
	if market.Active {
		statusParts = append(statusParts, "Active")
	}
	if market.Funded {
		statusParts = append(statusParts, "Funded")
	}
	if market.Ready {
		statusParts = append(statusParts, "Ready")
	}
	if len(statusParts) > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Status: %s\n", strings.Join(statusParts, ", ")))
	}
	
	// Price Information
	contextBuilder.WriteString("\n=== PRICING DATA ===\n")
	if market.YesPrice > 0 {
		contextBuilder.WriteString(fmt.Sprintf("YES Price: %.4f (%.2f%%)\n", market.YesPrice, market.YesPrice*100))
	}
	if market.NoPrice > 0 {
		contextBuilder.WriteString(fmt.Sprintf("NO Price: %.4f (%.2f%%)\n", market.NoPrice, market.NoPrice*100))
	}
	if market.LastTradePrice > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Last Trade Price: %.4f\n", market.LastTradePrice))
	}
	if market.BestBid > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Best Bid: %.4f\n", market.BestBid))
	}
	if market.BestAsk > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Best Ask: %.4f\n", market.BestAsk))
	}
	if market.Spread > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Spread: %.4f\n", market.Spread))
	}
	if market.YesBestBid > 0 {
		contextBuilder.WriteString(fmt.Sprintf("YES Best Bid: %.4f\n", market.YesBestBid))
	}
	if market.YesBestAsk > 0 {
		contextBuilder.WriteString(fmt.Sprintf("YES Best Ask: %.4f\n", market.YesBestAsk))
	}
	if market.NoBestBid > 0 {
		contextBuilder.WriteString(fmt.Sprintf("NO Best Bid: %.4f\n", market.NoBestBid))
	}
	if market.NoBestAsk > 0 {
		contextBuilder.WriteString(fmt.Sprintf("NO Best Ask: %.4f\n", market.NoBestAsk))
	}
	
	// Price Changes (momentum indicators)
	priceChangeParts := []string{}
	if market.OneHourPriceChange != 0 {
		priceChangeParts = append(priceChangeParts, fmt.Sprintf("1h: %.2f%%", market.OneHourPriceChange*100))
	}
	if market.OneDayPriceChange != 0 {
		priceChangeParts = append(priceChangeParts, fmt.Sprintf("24h: %.2f%%", market.OneDayPriceChange*100))
	}
	if market.OneWeekPriceChange != 0 {
		priceChangeParts = append(priceChangeParts, fmt.Sprintf("7d: %.2f%%", market.OneWeekPriceChange*100))
	}
	if market.OneMonthPriceChange != 0 {
		priceChangeParts = append(priceChangeParts, fmt.Sprintf("30d: %.2f%%", market.OneMonthPriceChange*100))
	}
	if len(priceChangeParts) > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Price Changes: %s\n", strings.Join(priceChangeParts, ", ")))
	}
	
	// Volume Metrics
	contextBuilder.WriteString("\n=== VOLUME METRICS ===\n")
	if market.Volume24h > 0 {
		contextBuilder.WriteString(fmt.Sprintf("24h Volume: $%.2f\n", market.Volume24h))
	}
	if market.Volume1Week > 0 {
		contextBuilder.WriteString(fmt.Sprintf("7d Volume: $%.2f\n", market.Volume1Week))
	}
	if market.Volume1Month > 0 {
		contextBuilder.WriteString(fmt.Sprintf("30d Volume: $%.2f\n", market.Volume1Month))
	}
	if market.VolumeAllTime > 0 {
		contextBuilder.WriteString(fmt.Sprintf("All-Time Volume: $%.2f\n", market.VolumeAllTime))
	}
	
	// Liquidity Metrics
	if market.Liquidity > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Liquidity: $%.2f\n", market.Liquidity))
	}
	if market.LiquidityClob > 0 {
		contextBuilder.WriteString(fmt.Sprintf("CLOB Liquidity: $%.2f\n", market.LiquidityClob))
	}
	
	// Outcome Prices (if available)
	if market.OutcomePrices != "" {
		contextBuilder.WriteString(fmt.Sprintf("Outcome Prices: %s\n", market.OutcomePrices))
	}
	
	// Market Quality Indicators
	if market.Competitive > 0 {
		contextBuilder.WriteString(fmt.Sprintf("Competitive Score: %.2f\n", market.Competitive))
	}
	// Note: TrendingScore is only computed for market lanes, not available for single market fetches
	
	// Market Timestamps
	if market.MarketCreatedAt != nil {
		contextBuilder.WriteString(fmt.Sprintf("Market Created: %s\n", market.MarketCreatedAt.Format(time.RFC3339)))
	}
	if market.MarketUpdatedAt != nil {
		contextBuilder.WriteString(fmt.Sprintf("Last Updated: %s\n", market.MarketUpdatedAt.Format(time.RFC3339)))
	}
	
	contextBuilder.WriteString("\n=== RECENT SEARCH RESULTS ===\n")

	var sources []Source
	if len(searchResults) == 0 {
		contextBuilder.WriteString("No external sources available from search.\n")
	} else {
		for i, res := range searchResults {
			if i >= maxSourceEntries {
				break
			}
			// No truncation - send full content for maximum context
			content := strings.TrimSpace(res.Content)
			if content != "" {
				contextBuilder.WriteString(fmt.Sprintf("[%d] %s: %s\n", i+1, res.Title, content))
			} else {
				contextBuilder.WriteString(fmt.Sprintf("[%d] %s\n", i+1, res.Title))
			}
			sources = append(sources, Source{Title: res.Title, URL: res.URL})
		}
	}

	// 4. Prompt Engineering
	systemPrompt := `You are Bankai Oracle, a precision prediction market analysis engine. Your sole purpose is to calculate the exact probability of a "YES" outcome for the given market using all available data.

ANALYSIS METHODOLOGY:

1. MARKET FUNDAMENTALS:
   - Parse the full description and resolution rules meticulously. These define what "YES" means and how the market resolves.
   - Identify the specific event, deadline, and resolution criteria. Any ambiguity in rules increases uncertainty.
   - Consider category and tags for context about market type and domain expertise required.

2. TEMPORAL ANALYSIS:
   - Compare current time against Start Date, End Date, and Event Start Time.
   - Markets closer to resolution dates have less time for conditions to change.
   - Recent market creation suggests less historical data; older markets may have more established patterns.

3. MARKET SENTIMENT (PRICING DATA):
   - Current YES/NO prices reflect aggregate market belief. YES price = implied probability.
   - Compare YES price to your calculated probability. Significant divergence indicates either market inefficiency or your analysis gap.
   - Best Bid/Ask spreads indicate liquidity depth. Wide spreads suggest uncertainty or low confidence.
   - Last Trade Price shows recent market activity and direction.

4. MOMENTUM INDICATORS:
   - Price changes (1h, 24h, 7d, 30d) reveal trend direction and velocity.
   - Positive momentum in YES price suggests increasing confidence in YES outcome.
   - Negative momentum suggests deteriorating YES prospects.
   - Consider both short-term (1h, 24h) and medium-term (7d, 30d) trends for context.

5. MARKET DEPTH & ACTIVITY:
   - Volume metrics indicate market interest and information flow. High volume suggests active information discovery.
   - 24h volume vs historical volume shows if interest is accelerating or declining.
   - Liquidity depth determines how easily positions can be entered/exited. Low liquidity may indicate low confidence or niche market.
   - Competitive score reflects market quality and participant engagement.

6. EXTERNAL INTELLIGENCE (Tavily Search Results):
   - Analyze all 5 search results for concrete, current information relevant to the market question.
   - Prioritize recent developments, official announcements, regulatory changes, and factual events.
   - Cross-reference news with market description and resolution rules. Does the news support or contradict YES outcome?
   - Ignore speculation and opinion; focus on verifiable facts and events.
   - If news is contradictory or insufficient, factor this into uncertainty.

7. SYNTHESIS & PROBABILITY CALCULATION:
   - Weight all factors: fundamentals (40%), market sentiment/pricing (30%), external news (20%), momentum/volume (10%).
   - If market price strongly diverges from fundamentals, question why. Market may know something you don't, or vice versa.
   - If resolution rules are ambiguous or external information is contradictory, increase uncertainty.
   - If all signals align (fundamentals + pricing + news + momentum), confidence should be high.
   - Output a precise probability (e.g., 0.734, not 0.7 or 0.75). Use decimal precision to reflect confidence level.

8. SENTIMENT CLASSIFICATION:
   - "Bullish": Strong positive signals across multiple dimensions, probability > 0.65
   - "Bearish": Strong negative signals, probability < 0.35
   - "Neutral": Mixed signals or balanced evidence, probability 0.35-0.65
   - "Uncertain": Insufficient information, contradictory signals, or ambiguous resolution criteria

OUTPUT REQUIREMENTS:
- Respond with ONLY a valid JSON object. No markdown, no code fences, no prose, no reasoning tokens outside JSON.
- If you cannot produce valid JSON, return: {"probability":0,"sentiment":"Uncertain","reasoning":"Unable to comply"}

Required JSON format:
{
  "probability": number,  // 0.0 to 1.0, precise decimal (e.g., 0.734 for 73.4%)
  "sentiment": string,    // Exactly one of: "Bullish", "Bearish", "Neutral", "Uncertain"
  "reasoning": string     // 3-5 sentences: Concise synthesis of key factors that led to your probability estimate. Reference specific data points (prices, volume, news, dates) that influenced your decision. Be direct and factual.
}`

	userPrompt := fmt.Sprintf(`Analyze this prediction market and calculate the probability of a YES outcome.

You have been provided with:
- Complete market description and resolution rules (read carefully - these define what YES means)
- Current market pricing data (YES/NO prices, bids/asks, spreads, last trade)
- Price momentum indicators (1h, 24h, 7d, 30d changes)
- Trading volume and liquidity metrics (24h, 7d, 30d, all-time)
- Market status, dates, and metadata
- 5 external news sources with full content (analyze for relevant, current information)

SYNTHESIZE ALL DATA:
1. What do the fundamentals (description + rules) tell you about the likelihood of YES?
2. What does current market pricing imply about collective belief?
3. What do price momentum trends indicate about direction?
4. What do volume and liquidity metrics reveal about market confidence?
5. What concrete information do the external news sources provide?
6. How do all these signals align or conflict?

Calculate a precise probability (0.0-1.0) that reflects your synthesis of ALL available data.
Classify sentiment based on the strength and direction of signals.
Provide reasoning that references specific data points that influenced your calculation.

Return ONLY the JSON object with your analysis.

%s`, contextBuilder.String())

	// 5. Call LLM
	logger.Info("Oracle calling LLM for market %s | model=%s | context_len=%d", market.ConditionID, s.OpenAI.Model(), len(userPrompt))
	rawResponse, err := s.OpenAI.Analyze(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("analysis generation failed: %w", err)
	}
	if strings.TrimSpace(rawResponse) == "" {
		logger.Error("LLM response was empty for market %s", market.ConditionID)
		return nil, fmt.Errorf("analysis result was empty")
	}
	logger.Info("Oracle raw LLM response for %s (truncated): %s", market.ConditionID, truncateForLog(rawResponse, 2000))

	// 6. Parse Response
	// First, try to extract JSON object directly (handles cases with reasoning text before/after)
	cleanedResponse := extractJSONObject(rawResponse)
	
	// If no JSON found, try cleaning markdown fences
	if !strings.Contains(cleanedResponse, "{") {
		cleanedResponse = cleanJSONFence(rawResponse)
		cleanedResponse = extractJSONObject(cleanedResponse)
	}
	
	cleanedResponse = strings.TrimSpace(cleanedResponse)
	if cleanedResponse == "" {
		logger.Error("Cleaned LLM response empty for market %s | raw: %q", market.ConditionID, truncateForLog(rawResponse, 500))
		return fallbackAnalysis(market, sources, "empty LLM response after cleaning")
	}
	logger.Info("Oracle cleaned JSON candidate for %s (truncated): %s", market.ConditionID, truncateForLog(cleanedResponse, 1000))

	var llmResult struct {
		Probability float64 `json:"probability"`
		Sentiment   string  `json:"sentiment"`
		Reasoning   string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(cleanedResponse), &llmResult); err != nil {
		logger.Error("Failed to parse LLM response as JSON: %v | cleaned: %s | raw: %s", err, truncateForLog(cleanedResponse, 500), truncateForLog(rawResponse, 500))
		if coerced := coerceAnalysisFromText(rawResponse); coerced != nil {
			logger.Info("Oracle coerced analysis from non-JSON response for %s", market.ConditionID)
			coerced.MarketID = market.ConditionID
			coerced.Question = market.Title
			coerced.Sources = sources
			coerced.LastUpdated = time.Now().UTC().Format(time.RFC3339)
			return coerced, nil
		}
		return fallbackAnalysis(market, sources, cleanedResponse)
	}

	// Validate parsed fields
	if llmResult.Reasoning == "" {
		logger.Info("Oracle parsed result missing reasoning field for %s, using default", market.ConditionID)
		llmResult.Reasoning = "Analysis completed but no reasoning provided."
	}

	logger.Info("Oracle parsed result for %s | prob=%.3f | sentiment=%s | reasoning_len=%d", 
		market.ConditionID, llmResult.Probability, llmResult.Sentiment, len(llmResult.Reasoning))

	return &MarketAnalysis{
		MarketID:    market.ConditionID,
		Question:    market.Title,
		Probability: normalizeProbability(llmResult.Probability),
		Sentiment:   normalizeSentiment(llmResult.Sentiment),
		Reasoning:   strings.TrimSpace(llmResult.Reasoning),
		Sources:     sources,
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func cleanJSONFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// extractJSONObject tries to pull the first top-level JSON object from a string.
// This handles cases where reasoning tokens or other text appear before/after the JSON.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	
	// Find the first '{' character
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return s
	}
	
	// Track depth to find matching closing brace
	depth := 0
	inString := false
	escapeNext := false
	
	for i := start; i < len(s); i++ {
		char := s[i]
		
		// Handle escape sequences in strings
		if escapeNext {
			escapeNext = false
			continue
		}
		if char == '\\' {
			escapeNext = true
			continue
		}
		
		// Track string boundaries
		if char == '"' && !escapeNext {
			inString = !inString
			continue
		}
		
		// Only count braces when not inside a string
		if !inString {
			switch char {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					// Found complete JSON object
					return strings.TrimSpace(s[start : i+1])
				}
			}
		}
	}
	
	// If we didn't find a complete object, return what we have
	return strings.TrimSpace(s[start:])
}

func truncateForLog(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "...(truncated)"
}

func fallbackAnalysis(market *models.Market, sources []Source, reason string) (*MarketAnalysis, error) {
	return &MarketAnalysis{
		MarketID:    market.ConditionID,
		Question:    market.Title,
		Probability: 0,
		Sentiment:   "Uncertain",
		Reasoning:   fmt.Sprintf("LLM response unparsable: %s", truncateForLog(reason, 200)),
		Sources:     sources,
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// coerceAnalysisFromText tries to extract probability/sentiment/reasoning from free-form text.
func coerceAnalysisFromText(raw string) *MarketAnalysis {
	txt := strings.TrimSpace(raw)
	if txt == "" {
		return nil
	}

	// Extract first probability-like number
	var prob float64
	probFound := false

	// Match percentages like 65% or 65.5%
	rePct := regexp.MustCompile(`(?i)(\d{1,3}(\.\d+)?)[ ]*%`)
	if m := rePct.FindStringSubmatch(txt); len(m) > 0 {
		if p, err := strconv.ParseFloat(m[1], 64); err == nil {
			prob = p / 100.0
			probFound = true
		}
	}

	// Match decimal probabilities 0.xx if not already found
	if !probFound {
		reDec := regexp.MustCompile(`(?m)\b0\.\d+\b`)
		if m := reDec.FindString(txt); m != "" {
			if p, err := strconv.ParseFloat(m, 64); err == nil {
				prob = p
				probFound = true
			}
		}
	}

	sentiment := "Uncertain"
	lower := strings.ToLower(txt)
	if strings.Contains(lower, "bullish") {
		sentiment = "Bullish"
	} else if strings.Contains(lower, "bearish") {
		sentiment = "Bearish"
	} else if strings.Contains(lower, "neutral") {
		sentiment = "Neutral"
	}

	reasoning := truncateForLog(txt, 400)

	if !probFound {
		prob = 0
	}

	return &MarketAnalysis{
		Probability: normalizeProbability(prob),
		Sentiment:   sentiment,
		Reasoning:   reasoning,
	}
}

func truncateText(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}

func normalizeProbability(p float64) float64 {
	if p > 1 && p <= 100 {
		p = p / 100
	}
	switch {
	case p < 0:
		return 0
	case p > 1:
		return 1
	default:
		return p
	}
}

func normalizeSentiment(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "bullish":
		return "Bullish"
	case "bearish":
		return "Bearish"
	case "neutral":
		return "Neutral"
	case "uncertain", "unknown", "":
		return "Uncertain"
	default:
		return s
	}
}
