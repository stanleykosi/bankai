package allora

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bankai-project/backend/internal/config"
)

func TestGetPriceInferenceParsesScaledFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("unexpected x-api-key: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"request_id":"abc-123",
			"status":true,
			"data":{
				"signature":"0xdeadbeef",
				"inference_data":{
					"network_inference":"3365485208027959000000",
					"confidence_interval_percentiles":["2280000000000000000","50000000000000000000","97720000000000000000"],
					"confidence_interval_values":["3016256807053656000000","3049738780726754000000","3278333171848616500000"],
					"topic_id":"14",
					"timestamp":"1719866777"
				}
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			AlloraAPIKey:          "test-key",
			AlloraBaseURL:         server.URL,
			AlloraSignatureFormat: "ethereum-11155111",
		},
	}
	client := NewClient(cfg)

	out, err := client.GetPriceInference(context.Background(), "btc", Timeframe5m)
	if err != nil {
		t.Fatalf("GetPriceInference() error = %v", err)
	}

	if out.Asset != "BTC" {
		t.Fatalf("expected asset BTC, got %s", out.Asset)
	}
	if out.TopicID != "14" {
		t.Fatalf("expected topic 14, got %s", out.TopicID)
	}
	if out.NetworkInference < 3365 || out.NetworkInference > 3366 {
		t.Fatalf("unexpected network inference %.6f", out.NetworkInference)
	}
	if len(out.ConfidenceIntervalValues) != 3 {
		t.Fatalf("expected 3 confidence values, got %d", len(out.ConfidenceIntervalValues))
	}
	if out.Timestamp.IsZero() || out.Timestamp.Location() != time.UTC {
		t.Fatalf("expected UTC timestamp, got %v", out.Timestamp)
	}
}

func TestGetPriceInferenceRejectsUnsupportedTimeframe(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			AlloraAPIKey: "test-key",
		},
	}
	client := NewClient(cfg)
	_, err := client.GetPriceInference(context.Background(), "BTC", PriceInferenceTimeframe("15m"))
	if err == nil {
		t.Fatalf("expected unsupported timeframe error")
	}
}

func TestGetPriceInferenceRetriesRetryableStatus(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if call < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":false}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"request_id":"retry-ok",
			"status":true,
			"data":{
				"signature":"0xbeef",
				"inference_data":{
					"network_inference":"3000000000000000000000",
					"confidence_interval_percentiles":["50000000000000000000"],
					"confidence_interval_values":["3000000000000000000000"],
					"topic_id":"14",
					"timestamp":"1719866777"
				}
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			AlloraAPIKey:          "test-key",
			AlloraBaseURL:         server.URL,
			AlloraSignatureFormat: "ethereum-11155111",
		},
	}
	client := NewClient(cfg)

	out, err := client.GetPriceInference(context.Background(), "BTC", Timeframe5m)
	if err != nil {
		t.Fatalf("GetPriceInference() error = %v", err)
	}
	if out == nil || out.TopicID != "14" {
		t.Fatalf("expected successful decoded inference after retries")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestGetPriceInferenceParsesNumericTimestamp(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"request_id":"numeric-ts",
			"status":true,
			"data":{
				"signature":"0xbeef",
				"inference_data":{
					"network_inference":"3000000000000000000000",
					"confidence_interval_percentiles":["50000000000000000000"],
					"confidence_interval_values":["3000000000000000000000"],
					"topic_id":"14",
					"timestamp":1719866777
				}
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			AlloraAPIKey:          "test-key",
			AlloraBaseURL:         server.URL,
			AlloraSignatureFormat: "ethereum-11155111",
		},
	}
	client := NewClient(cfg)

	out, err := client.GetPriceInference(context.Background(), "BTC", Timeframe5m)
	if err != nil {
		t.Fatalf("GetPriceInference() error = %v", err)
	}
	if out.Timestamp.Unix() != 1719866777 {
		t.Fatalf("expected timestamp 1719866777, got %d", out.Timestamp.Unix())
	}
}
