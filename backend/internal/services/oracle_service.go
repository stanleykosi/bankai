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
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/integrations/openai"
	"github.com/bankai-project/backend/internal/integrations/tavily"
	"github.com/bankai-project/backend/internal/logger"
)

const (
	maxSourceEntries       = 4
	maxSourceContentLength = 480
	maxRulesLength         = 600
	maxDescriptionLength   = 600
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
	contextBuilder := strings.Builder{}
	contextBuilder.WriteString(fmt.Sprintf("Market Title: %s\n", market.Title))
	if desc := strings.TrimSpace(market.Description); desc != "" {
		contextBuilder.WriteString(fmt.Sprintf("Description: %s\n", truncateText(desc, maxDescriptionLength)))
	}
	if rules := strings.TrimSpace(market.ResolutionRules); rules != "" {
		contextBuilder.WriteString(fmt.Sprintf("Resolution Rules: %s\n", truncateText(rules, maxRulesLength)))
	}
	contextBuilder.WriteString("\nRecent Search Results:\n")

	var sources []Source
	if len(searchResults) == 0 {
		contextBuilder.WriteString("No external sources available from search.\n")
	} else {
		for i, res := range searchResults {
			if i >= maxSourceEntries {
				break
			}
			snippet := truncateText(strings.TrimSpace(res.Content), maxSourceContentLength)
			contextBuilder.WriteString(fmt.Sprintf("[%d] %s: %s\n", i+1, res.Title, snippet))
			sources = append(sources, Source{Title: res.Title, URL: res.URL})
		}
	}

	// 4. Prompt Engineering
	systemPrompt := `You are Bankai Oracle, an elite prediction market analyst.
Your goal is to estimate the probability (0-100%) of a "YES" outcome for the given market.
You must be objective, identifying key factors, potential blockers, and recent developments.
If the information is insufficient, acknowledge the uncertainty.

Return ONLY a single JSON object. No markdown, no prose, no code fences.
Output JSON format:
{
  "probability": number, // 0.0 to 1.0 (e.g. 0.65)
  "sentiment": string, // "Bullish", "Bearish", "Neutral", "Uncertain"
  "reasoning": string // Concise summary of your analysis (max 3 sentences)
}`

	userPrompt := fmt.Sprintf("Analyze this market based on the context:\n\n%s", contextBuilder.String())

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
	cleanedResponse := cleanJSONFence(rawResponse)
	cleanedResponse = extractJSONObject(cleanedResponse)
	if strings.TrimSpace(cleanedResponse) == "" {
		logger.Error("Cleaned LLM response empty for market %s | raw: %q", market.ConditionID, rawResponse)
		return nil, fmt.Errorf("failed to parse analysis result")
	}
	logger.Info("Oracle cleaned JSON candidate for %s (truncated): %s", market.ConditionID, truncateForLog(cleanedResponse, 1000))

	var llmResult struct {
		Probability float64 `json:"probability"`
		Sentiment   string  `json:"sentiment"`
		Reasoning   string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(cleanedResponse), &llmResult); err != nil {
		logger.Error("Failed to parse LLM response: %s | raw: %s", cleanedResponse, rawResponse)
		return nil, fmt.Errorf("failed to parse analysis result")
	}

	logger.Info("Oracle parsed result for %s | prob=%.3f | sentiment=%s", market.ConditionID, llmResult.Probability, llmResult.Sentiment)

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
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(s[start : i+1])
			}
		}
	}
	return s
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
