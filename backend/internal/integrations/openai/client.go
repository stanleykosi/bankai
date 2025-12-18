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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
)

const (
	DefaultBaseURL   = "https://openrouter.ai/api/v1/chat/completions"
	DefaultModel     = "google/gemini-3-pro-preview"
	requestTimeout   = 60 * time.Second
	defaultMaxTokens = 320
)

type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	model      string
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
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
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

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

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.Error("OpenAI API error: %d - %s", resp.StatusCode, string(respBody))
		return "", fmt.Errorf("openai api returned status %d", resp.StatusCode)
	}

	var result ChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		logger.Error("Failed to decode OpenAI response: %v | raw: %s", err, string(respBody))
		return "", fmt.Errorf("failed to decode openai response: %w", err)
	}

	if len(result.Choices) == 0 {
		logger.Error("OpenAI response had no choices | raw: %s", string(respBody))
		return "", fmt.Errorf("no choices returned from openai")
	}

	content := strings.TrimSpace(result.Choices[0].Message.Content)
	if content == "" {
		if alt := strings.TrimSpace(result.Choices[0].Message.Reasoning); alt != "" {
			content = alt
		} else if len(result.Choices[0].Message.ReasoningDetails) > 0 {
			content = strings.TrimSpace(result.Choices[0].Message.ReasoningDetails[0].Text)
		}
	}

	if content == "" {
		logger.Error("OpenAI response missing content | response: %s | raw: %s", toJSON(result), string(respBody))
		return "", fmt.Errorf("openai response missing content")
	}

	return content, nil
}

// Model returns the model name being used by this client
func (c *Client) Model() string {
	return c.model
}

func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
