/**
 * @description
 * Lightweight OpenAI Chat Completions client.
 * Used by the Oracle service to synthesize market probability estimates.
 *
 * @dependencies
 * - net/http
 * - encoding/json
 * - backend/internal/config
 */

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
)

const (
	DefaultBaseURL   = "https://openrouter.ai/api/v1/chat/completions"
	DefaultModel     = "google/gemini-3-pro-preview"
	requestTimeout   = 120 * time.Second
	defaultMaxTokens = 10000 // High limit for comprehensive analysis with full context
	maxAnalyzeTries  = 3
	retryBaseDelay   = 400 * time.Millisecond
)

var (
	errOpenAIResponseRead   = errors.New("openai response read failed")
	errOpenAIResponseDecode = errors.New("openai response decode failed")
	errOpenAIRetryable      = errors.New("openai api retryable error")
)

type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	model      string
}

type ChatRequest struct {
	Model          string                 `json:"model"`
	Messages       []Message              `json:"messages"`
	Temperature    float64                `json:"temperature"`
	MaxTokens      int                    `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat        `json:"response_format,omitempty"`
	Reasoning      *ReasoningConfig       `json:"reasoning,omitempty"`
}

type ResponseFormat struct {
	Type string `json:"type"`
}

type ReasoningConfig struct {
	Exclude bool `json:"exclude,omitempty"` // Exclude reasoning from content, but allow unlimited reasoning internally
}

type Message struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	Reasoning        string            `json:"reasoning,omitempty"`
	ReasoningDetails []ReasoningDetail `json:"reasoning_details,omitempty"`
}

type ReasoningDetail struct {
	Text string `json:"text,omitempty"`
	// Some providers include format/index; omit for brevity
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewClient(cfg *config.Config) *Client {
	baseURL := strings.TrimSpace(cfg.Services.OpenAIBaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := strings.TrimSpace(cfg.Services.OpenAIModel)
	if model == "" {
		model = DefaultModel
	}

	return &Client{
		apiKey:  cfg.Services.OpenAIAPIKey,
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// Analyze sends a chat completion request and returns the first choice content.
func (c *Client) Analyze(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("openai api key is not configured")
	}

	systemPrompt = strings.TrimSpace(systemPrompt)
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return "", fmt.Errorf("user prompt is required")
	}

	payload := ChatRequest{
		Model: c.model,
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
		MaxTokens:   defaultMaxTokens,
		// Enforce JSON output format
		ResponseFormat: &ResponseFormat{
			Type: "json_object",
		},
		// Exclude reasoning tokens from content field, but allow unlimited reasoning internally
		// The model can reason as much as it wants, but only the final JSON output appears in content
		Reasoning: &ReasoningConfig{
			Exclude: true, // Reasoning happens internally but is excluded from the content field
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	var lastErr error
	for attempt := 1; attempt <= maxAnalyzeTries; attempt++ {
		content, err := c.analyzeOnce(ctx, bodyBytes)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if attempt >= maxAnalyzeTries || !isRetryableOpenAIError(err) {
			return "", err
		}
		logger.Info("Retrying OpenAI request after error (attempt %d/%d): %v", attempt, maxAnalyzeTries, err)
		time.Sleep(retryBaseDelay * time.Duration(attempt))
	}

	return "", lastErr
}

// Model returns the model name being used by this client
func (c *Client) Model() string {
	return c.model
}

func (c *Client) analyzeOnce(ctx context.Context, bodyBytes []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		logger.Error("Failed to read OpenAI response body: %v | partial: %s", readErr, truncateForLog(string(respBody), 1000))
		return "", fmt.Errorf("%w: %v", errOpenAIResponseRead, readErr)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Error("OpenAI API error: %d - %s", resp.StatusCode, string(respBody))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			return "", fmt.Errorf("%w: status %d", errOpenAIRetryable, resp.StatusCode)
		}
		return "", fmt.Errorf("openai api returned status %d", resp.StatusCode)
	}

	var result ChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		logger.Error("Failed to decode OpenAI response: %v | raw: %s", err, string(respBody))
		return "", fmt.Errorf("%w: %v", errOpenAIResponseDecode, err)
	}

	if len(result.Choices) == 0 {
		logger.Error("OpenAI response had no choices | raw: %s", string(respBody))
		return "", fmt.Errorf("no choices returned from openai")
	}

	// Extract ONLY the content field - never use reasoning tokens as content
	// Reasoning tokens are separate and should not interfere with JSON parsing
	content := strings.TrimSpace(result.Choices[0].Message.Content)

	// Log reasoning separately for debugging (if present) but don't use it as content
	if reasoning := strings.TrimSpace(result.Choices[0].Message.Reasoning); reasoning != "" {
		logger.Info("OpenRouter reasoning tokens received (excluded from content): %s", truncateForLog(reasoning, 200))
	}
	if len(result.Choices[0].Message.ReasoningDetails) > 0 {
		for i, detail := range result.Choices[0].Message.ReasoningDetails {
			if i >= 3 { // Limit logging
				break
			}
			if text := strings.TrimSpace(detail.Text); text != "" {
				logger.Info("OpenRouter reasoning detail %d (excluded from content): %s", i+1, truncateForLog(text, 200))
			}
		}
	}

	if content == "" {
		finishReason := result.Choices[0].FinishReason
		logger.Error("OpenAI response missing content field | finish_reason=%s | response: %s | raw: %s",
			finishReason, toJSON(result), truncateForLog(string(respBody), 1000))

		// If truncated due to length, the model used all tokens for reasoning without generating content
		if finishReason == "length" {
			return "", fmt.Errorf("openai response truncated: model consumed all %d tokens in reasoning without generating content. Consider increasing max_tokens further", defaultMaxTokens)
		}
		return "", fmt.Errorf("openai response missing content (finish_reason: %s)", finishReason)
	}

	return content, nil
}

func isRetryableOpenAIError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errOpenAIResponseRead) || errors.Is(err, errOpenAIResponseDecode) || errors.Is(err, errOpenAIRetryable) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
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
