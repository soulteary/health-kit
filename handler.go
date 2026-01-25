package health

import (
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

// HTTPStatusCode returns the appropriate HTTP status code for a health status
func HTTPStatusCode(status Status) int {
	switch status {
	case StatusHealthy:
		return http.StatusOK
	case StatusDegraded:
		return http.StatusOK // Degraded is still functional
	case StatusUnhealthy:
		return http.StatusServiceUnavailable
	default:
		return http.StatusServiceUnavailable
	}
}

// simpleResponse is used when details are not included
type simpleResponse struct {
	Status  Status `json:"status"`
	Service string `json:"service"`
}

// Handler returns a standard library HTTP handler for health checks
func Handler(aggregator *Aggregator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config := aggregator.Config()

		// Check IP whitelist
		if len(config.IPWhitelist) > 0 {
			clientIP := getClientIPFromRequest(r)
			if !config.IsIPAllowed(clientIP) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		// Perform health check
		result := aggregator.Check(r.Context())

		// Build response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(HTTPStatusCode(result.Status))

		if !config.IncludeDetails {
			_ = json.NewEncoder(w).Encode(simpleResponse{
				Status:  result.Status,
				Service: result.Service,
			})
			return
		}

		if !config.IncludeChecks {
			result.Checks = nil
		}

		_ = json.NewEncoder(w).Encode(result)
	}
}

// LivenessHandler returns a simple liveness check handler (for Kubernetes)
// Always returns 200 OK if the service is running
func LivenessHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(simpleResponse{
			Status:  StatusHealthy,
			Service: serviceName,
		})
	}
}

// ReadinessHandler returns a readiness check handler (for Kubernetes)
// Returns 200 OK only if all critical checks pass
func ReadinessHandler(aggregator *Aggregator) http.HandlerFunc {
	return Handler(aggregator)
}

// FiberHandler returns a Fiber handler for health checks
func FiberHandler(aggregator *Aggregator) fiber.Handler {
	return func(c *fiber.Ctx) error {
		config := aggregator.Config()

		// Check IP whitelist
		if len(config.IPWhitelist) > 0 {
			clientIP := c.IP()
			if !config.IsIPAllowed(clientIP) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Forbidden",
				})
			}
		}

		// Perform health check
		result := aggregator.Check(c.Context())

		if !config.IncludeDetails {
			return c.Status(HTTPStatusCode(result.Status)).JSON(simpleResponse{
				Status:  result.Status,
				Service: result.Service,
			})
		}

		if !config.IncludeChecks {
			result.Checks = nil
		}

		return c.Status(HTTPStatusCode(result.Status)).JSON(result)
	}
}

// FiberLivenessHandler returns a simple Fiber liveness check handler
func FiberLivenessHandler(serviceName string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(simpleResponse{
			Status:  StatusHealthy,
			Service: serviceName,
		})
	}
}

// FiberReadinessHandler returns a Fiber readiness check handler
func FiberReadinessHandler(aggregator *Aggregator) fiber.Handler {
	return FiberHandler(aggregator)
}

// getClientIPFromRequest extracts the client IP from an HTTP request
func getClientIPFromRequest(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// SimpleHandler returns a minimal health check handler without aggregator
// Useful for simple services that just need to report they're running
func SimpleHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(simpleResponse{
			Status:  StatusHealthy,
			Service: serviceName,
		})
	}
}

// SimpleFiberHandler returns a minimal Fiber health check handler
func SimpleFiberHandler(serviceName string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(simpleResponse{
			Status:  StatusHealthy,
			Service: serviceName,
		})
	}
}
