/**
 * @description
 * HTTP handlers for the Alpha Hub (/analysis) endpoints.
 *
 * @dependencies
 * - backend/internal/services
 * - github.com/gofiber/fiber/v2
 */

package handlers

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type AnalysisHandler struct {
	Service *services.AlphaHubService
}

func NewAnalysisHandler(service *services.AlphaHubService) *AnalysisHandler {
	return &AnalysisHandler{Service: service}
}

// GetSmartMoney returns the smart-money + whale signals for the last hour (default) or provided window.
// GET /api/v1/analysis/smart-money?window=60
func (h *AnalysisHandler) GetSmartMoney(c *fiber.Ctx) error {
	window := time.Duration(1440) * time.Minute // default 24h lookback
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		if mins, err := strconv.Atoi(raw); err == nil && mins > 0 && mins <= 1440 {
			window = time.Duration(mins) * time.Minute
		}
	}

	resp, err := h.Service.GetSmartMoneySignals(c.Context(), window)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

// GetAIPicks runs the Alpha Hub LLM against the latest smart-money payload.
// GET /api/v1/analysis/ai-picks?window=60
func (h *AnalysisHandler) GetAIPicks(c *fiber.Ctx) error {
	window := time.Duration(1440) * time.Minute // default 24h lookback
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		if mins, err := strconv.Atoi(raw); err == nil && mins > 0 && mins <= 1440 {
			window = time.Duration(mins) * time.Minute
		}
	}

	force := strings.TrimSpace(c.Query("force")) == "1"
	snapshot, err := h.Service.GetDailySnapshot(c.Context(), window, force)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if snapshot == nil {
		return c.JSON(&services.AIResponse{Picks: []services.AIPick{}})
	}
	resp := snapshot.AI
	resp.GeneratedAt = snapshot.GeneratedAt
	resp.ExpiresAt = snapshot.ExpiresAt
	resp.Stale = snapshot.Stale
	resp.Source = snapshot.Source
	resp.WindowSeconds = snapshot.WindowSeconds
	resp.TokenEstimate = snapshot.TokenEstimate
	resp.ChunkStats = snapshot.AI.ChunkStats
	if len(resp.Picks) == 0 && strings.TrimSpace(resp.RawContent) != "" {
		if picks, err := services.ParseAIPicksFromContent(resp.RawContent); err == nil && len(picks) > 0 {
			resp.Picks = picks
			resp.RawContent = ""
			if resp.CompletionNote == "" {
				resp.CompletionNote = "recovered_from_cached_raw_content"
			}
		}
	}
	return c.JSON(resp)
}

// GetSnapshot returns the cached daily snapshot without triggering a rebuild.
// GET /api/v1/analysis/snapshot?window=1440
func (h *AnalysisHandler) GetSnapshot(c *fiber.Ctx) error {
	window := time.Duration(1440) * time.Minute
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		if mins, err := strconv.Atoi(raw); err == nil && mins > 0 && mins <= 1440 {
			window = time.Duration(mins) * time.Minute
		}
	}

	snap, err := h.Service.GetCachedDailySnapshot(c.Context(), window)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if snap == nil {
		now := time.Now().UTC()
		return c.JSON(&services.AlphaSnapshot{
			WindowSeconds: int(window.Seconds()),
			GeneratedAt:   now,
			ExpiresAt:     now.Add(24 * time.Hour),
			SmartMoney: services.SmartMoneyResponse{
				WindowSeconds: int(window.Seconds()),
				Markets:       []services.MarketSignal{},
				Whales:        []services.WhaleEvent{},
				GeneratedAt:   now,
			},
			AI: services.AIResponse{
				Picks: []services.AIPick{},
			},
			Source:    "cache_miss",
			Stale:     true,
			LastError: "snapshot_missing",
		})
	}
	return c.JSON(snap)
}

// StreamWhaleUpdates streams live whale-sized trades over SSE.
// GET /api/v1/analysis/whales/stream
func (h *AnalysisHandler) StreamWhaleUpdates(c *fiber.Ctx) error {
	streamHub := h.Service.WhaleStreamHub()
	if streamHub == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "whale stream is not available",
		})
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	requestCtx := c.Context()
	ctx, cancel := context.WithCancel(context.Background())
	msgCh, unsubscribe := streamHub.Subscribe()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer func() {
			cancel()
			unsubscribe()
		}()

		requestDone := requestCtx.Done()
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-requestDone:
				return
			case <-ctx.Done():
				return
			case <-keepalive.C:
				fmt.Fprint(w, ": ping\n\n")
				if err := w.Flush(); err != nil {
					return
				}
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

// GetRecentWhales returns the latest live whales buffered in Redis.
// GET /api/v1/analysis/whales/recent?limit=15
func (h *AnalysisHandler) GetRecentWhales(c *fiber.Ctx) error {
	limit := 15
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	whales, err := h.Service.GetRecentWhales(c.Context(), limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(whales)
}

// CancelAIPicks requests cancellation of the current AI picks run.
// POST /api/v1/analysis/ai-picks/cancel?label=daily_snapshot
func (h *AnalysisHandler) CancelAIPicks(c *fiber.Ctx) error {
	label := strings.TrimSpace(c.Query("label"))
	if label == "" {
		label = "daily_snapshot"
	}
	if err := h.Service.CancelAIPicks(c.Context(), label); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "cancel_requested", "label": label})
}

// ResumeAIPicks clears a cancellation flag for AI picks runs.
// POST /api/v1/analysis/ai-picks/resume?label=daily_snapshot
func (h *AnalysisHandler) ResumeAIPicks(c *fiber.Ctx) error {
	label := strings.TrimSpace(c.Query("label"))
	if label == "" {
		label = "daily_snapshot"
	}
	if err := h.Service.ResumeAIPicks(c.Context(), label); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "cancel_cleared", "label": label})
}
