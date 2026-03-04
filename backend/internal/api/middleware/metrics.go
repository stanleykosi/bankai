package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	apiRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bankai_api_requests_total",
			Help: "Total number of API requests",
		},
		[]string{"method", "route", "status"},
	)
	apiRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bankai_api_request_duration_seconds",
			Help:    "API request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status"},
	)
)

func init() {
	prometheus.MustRegister(apiRequestsTotal, apiRequestDuration)
}

func MetricsMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		method := c.Method()
		route := "unmatched"
		if c.Route() != nil {
			if resolved := strings.TrimSpace(c.Route().Path); resolved != "" {
				route = resolved
			}
		}
		status := c.Response().StatusCode()
		statusLabel := strconv.Itoa(status)

		apiRequestsTotal.WithLabelValues(method, route, statusLabel).Inc()
		apiRequestDuration.WithLabelValues(method, route, statusLabel).Observe(time.Since(start).Seconds())

		return err
	}
}

func MetricsHandler(metricsToken string) fiber.Handler {
	h := adaptor.HTTPHandler(promhttp.Handler())
	token := strings.TrimSpace(metricsToken)
	if token == "" {
		return func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "metrics endpoint disabled"})
		}
	}

	return func(c *fiber.Ctx) error {
		auth := strings.TrimSpace(c.Get("Authorization"))
		if strings.HasPrefix(auth, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == token {
			return h(c)
		}
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
}
