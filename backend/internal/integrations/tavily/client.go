/**
 * @description
 * Client for Tavily Search API.
 * Optimized for pulling recent news/context to feed LLM prompts.
 *
 * @dependencies
 * - net/http
 * - encoding/json
 * - backend/internal/config
 */

package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
)

const (
	BaseURL        = "https://api.tavily.com/search"
	requestTimeout = 15 * time.Second
)

type Client struct {
	apiKey     string
	httpClient *http.Client
}

type SearchRequest struct {
	APIKey            string   `json:"api_key"`
	Query             string   `json:"query"`
	SearchDepth       string   `json:"search_depth"` // "basic" or "advanced"
	IncludeImages     bool     `json:"include_images"`
	IncludeAnswer     bool     `json:"include_answer"`
	IncludeRawContent bool     `json:"include_raw_content"`
	MaxResults        int      `json:"max_results"`
	IncludeDomains    []string `json:"include_domains,omitempty"`
	ExcludeDomains    []string `json:"exclude_domains,omitempty"`
}

type SearchResponse struct {
	Query   string         `json:"query"`
	Answer  string         `json:"answer"`
	Images  []interface{}  `json:"images"`
	Results []SearchResult `json:"results"`
}

type SearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		apiKey: cfg.Services.TavilyAPIKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// Search performs a query against Tavily API.
func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("tavily api key is not configured")
	}

	payload := SearchRequest{
		APIKey:            c.apiKey,
		Query:             query,
		SearchDepth:       "basic",
		IncludeAnswer:     false,
		IncludeRawContent: true,
		MaxResults:        5,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Error("Tavily API error: %d - %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("tavily api returned status %d", resp.StatusCode)
	}

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode tavily response: %w", err)
	}

	return result.Results, nil
}
