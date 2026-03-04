package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const requestIDHeader = "X-Request-Id"

func TracingMiddleware() fiber.Handler {
	tracer := otel.Tracer("bankai.api")

	return func(c *fiber.Ctx) error {
		requestID := strings.TrimSpace(c.Get(requestIDHeader))
		if requestID == "" {
			requestID = newRequestID()
		}
		c.Set(requestIDHeader, requestID)
		c.Locals("request_id", requestID)

		ctx := c.UserContext()
		spanName := c.Method() + " " + c.Path()
		if route := c.Route(); route != nil && strings.TrimSpace(route.Path) != "" {
			spanName = c.Method() + " " + route.Path
		}

		ctx, span := tracer.Start(ctx, spanName,
			trace.WithAttributes(
				attribute.String("http.method", c.Method()),
				attribute.String("http.route", c.Path()),
				attribute.String("request.id", requestID),
			),
		)
		c.SetUserContext(ctx)

		start := time.Now()
		err := c.Next()
		duration := time.Since(start)

		status := c.Response().StatusCode()
		span.SetAttributes(
			attribute.Int("http.status_code", status),
			attribute.String("http.client_ip", c.IP()),
			attribute.String("http.user_agent", c.Get("User-Agent")),
			attribute.Int64("http.duration_ms", duration.Milliseconds()),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if status >= fiber.StatusInternalServerError {
			span.SetStatus(codes.Error, "server_error")
		} else {
			span.SetStatus(codes.Ok, "ok")
		}
		span.End()

		return err
	}
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	}
	return hex.EncodeToString(bytes[:])
}
