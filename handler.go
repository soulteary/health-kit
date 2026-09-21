package health

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
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

// SimpleResponse is the body returned when details are not included.
// Exported for framework adapters -- see the fiberadapter subpackage.
type SimpleResponse struct {
	Status  Status `json:"status"`
	Service string `json:"service"`
}

// Handler returns a standard library HTTP handler for health checks
func Handler(aggregator *Aggregator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decision := Decide(r.Context(), aggregator, RequestSource(r))
		if decision.Forbidden {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(decision.StatusCode)
		_ = json.NewEncoder(w).Encode(decision.Body)
	}
}

// ClientIPSource is the minimal view of a request needed to resolve a client
// IP. Implementing it is all a framework adapter has to do -- see the
// fiberadapter subpackage.
type ClientIPSource interface {
	// RemoteIP is the peer address of the connection, nil when unavailable.
	RemoteIP() net.IP
	// Header returns a request header, or "" when absent.
	Header(name string) string
}

// ClientIP resolves the client IP under this config's trusted-proxy rules:
// X-Forwarded-For then X-Real-IP, but only when the peer is a trusted proxy.
//
// Exported, and taking an interface, so the rule has exactly one
// implementation. It used to have two -- one per framework -- and a
// trusted-proxy rule that disagrees with itself across frameworks is a
// whitelist bypass waiting to happen, not a cosmetic difference.
func (c Config) ClientIP(src ClientIPSource) string {
	remoteIP := src.RemoteIP()
	if remoteIP == nil {
		return ""
	}

	if c.IsTrustedProxy(remoteIP.String()) {
		if xff := parseForwardedIP(src.Header("X-Forwarded-For")); xff != "" {
			return xff
		}

		if xri := strings.TrimSpace(src.Header("X-Real-IP")); xri != "" {
			if ip := parseIPAddress(xri); ip != nil {
				return ip.String()
			}
		}
	}

	return remoteIP.String()
}

// RequestSource adapts an *http.Request to ClientIPSource.
func RequestSource(r *http.Request) ClientIPSource { return stdSource{r: r} }

type stdSource struct{ r *http.Request }

func (s stdSource) RemoteIP() net.IP          { return parseIPAddress(s.r.RemoteAddr) }
func (s stdSource) Header(name string) string { return s.r.Header.Get(name) }

// Decision is what a health endpoint should return, computed without reference
// to any web framework.
type Decision struct {
	// Forbidden reports that the client IP is not on the whitelist.
	//
	// Each adapter renders its own 403 body. They have never agreed -- the
	// net/http handler sends text/plain "Forbidden" while the Fiber one sends
	// {"error":"Forbidden"} -- and this change deliberately does not unify
	// them, so that moving Fiber out stays a move.
	Forbidden bool

	// StatusCode is the HTTP status to send. It is set on every Decision,
	// including a forbidden one, so an adapter that writes it unconditionally
	// cannot end up sending a zero status.
	StatusCode int

	// Body is the value to serialize as JSON.
	Body any
}

// Decide applies the IP whitelist, runs the checks and reduces the outcome to
// what should be sent. src is only consulted when a whitelist is configured.
func Decide(ctx context.Context, aggregator *Aggregator, src ClientIPSource) Decision {
	config := aggregator.Config()

	if len(config.IPWhitelist) > 0 {
		if !config.IsIPAllowed(config.ClientIP(src)) {
			return Decision{Forbidden: true, StatusCode: http.StatusForbidden}
		}
	}

	result := aggregator.Check(ctx)

	if !config.IncludeDetails {
		return Decision{
			StatusCode: HTTPStatusCode(result.Status),
			Body:       SimpleResponse{Status: result.Status, Service: result.Service},
		}
	}

	if !config.IncludeChecks {
		result.Checks = nil
	}

	return Decision{StatusCode: HTTPStatusCode(result.Status), Body: result}
}

// LivenessHandler returns a simple liveness check handler (for Kubernetes)
// Always returns 200 OK if the service is running
func LivenessHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SimpleResponse{
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

func parseForwardedIP(headerValue string) string {
	if headerValue == "" {
		return ""
	}
	// strings.Split always yields at least one part for a non-empty string,
	// which headerValue is by the check above.
	trimmed := strings.TrimSpace(strings.Split(headerValue, ",")[0])
	if trimmed == "" {
		return ""
	}
	if ip := parseIPAddress(trimmed); ip != nil {
		return ip.String()
	}
	return ""
}

func parseIPAddress(value string) net.IP {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}

// SimpleHandler returns a minimal health check handler without aggregator
// Useful for simple services that just need to report they're running
func SimpleHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SimpleResponse{
			Status:  StatusHealthy,
			Service: serviceName,
		})
	}
}
