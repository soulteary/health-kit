// Package fiberadapter serves health-kit's checks over Fiber v3.
//
// It lives in its own package so that importing the root package does not drag
// Fiber -- and with it fasthttp -- into binaries that never use it. A service
// on net/http, Echo, Gin or chi pays nothing for Fiber support existing; only
// importing this package links it in.
//
// Everything here is a translation layer: the IP whitelist, the trusted-proxy
// rule, which checks run and what status they map to all live in the root
// package and are read from there.
package fiberadapter

import (
	"net"

	"github.com/gofiber/fiber/v3"

	health "github.com/soulteary/health-kit/v2"
)

// Source adapts a fiber.Ctx to health.ClientIPSource.
type Source struct{ C fiber.Ctx }

// RemoteIP is the peer address of the connection.
func (s Source) RemoteIP() net.IP { return s.C.RequestCtx().RemoteIP() }

// Header returns a request header, or "" when absent.
func (s Source) Header(name string) string { return s.C.Get(name) }

// Handler returns a Fiber handler for health checks.
// It is the Fiber counterpart of health.Handler.
func Handler(aggregator *health.Aggregator) fiber.Handler {
	return func(c fiber.Ctx) error {
		decision := health.Decide(c, aggregator, Source{C: c})
		if decision.Forbidden {
			// Kept as JSON to match what this handler has always returned;
			// health.Handler answers text/plain here. See health.Decision.
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Forbidden",
			})
		}
		return c.Status(decision.StatusCode).JSON(decision.Body)
	}
}

// LivenessHandler returns a simple Fiber liveness check handler.
// It is the Fiber counterpart of health.LivenessHandler.
func LivenessHandler(serviceName string) fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(health.SimpleResponse{
			Status:  health.StatusHealthy,
			Service: serviceName,
		})
	}
}

// ReadinessHandler returns a Fiber readiness check handler.
// It is the Fiber counterpart of health.ReadinessHandler.
func ReadinessHandler(aggregator *health.Aggregator) fiber.Handler {
	return Handler(aggregator)
}

// SimpleHandler returns a minimal Fiber health check handler, with no
// aggregator. It is the Fiber counterpart of health.SimpleHandler.
func SimpleHandler(serviceName string) fiber.Handler {
	return LivenessHandler(serviceName)
}
