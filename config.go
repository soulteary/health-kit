package health

import (
	"net"
	"strings"
	"time"
)

// Config holds the configuration for health check handlers
type Config struct {
	// ServiceName is the name of the service for identification
	ServiceName string

	// Timeout is the default timeout for health checks
	Timeout time.Duration

	// IPWhitelist is a list of IP addresses/CIDRs allowed to access health endpoints
	// If empty, all IPs are allowed
	IPWhitelist []string

	// TrustedProxies is a list of proxy IPs/CIDRs that are allowed to supply
	// X-Forwarded-For or X-Real-IP headers.
	TrustedProxies []string

	// IncludeDetails controls whether to include detailed check results in response
	// Set to false in production to hide internal details
	IncludeDetails bool

	// IncludeChecks controls whether to include individual check results
	IncludeChecks bool

	// CriticalChecks is a list of check names that are critical
	// If any critical check fails, the overall status is unhealthy
	// Non-critical check failures result in degraded status
	CriticalChecks []string

	// parsedCIDRs caches parsed CIDR networks for IP whitelist
	parsedCIDRs []*net.IPNet

	// parsedIPs caches parsed IPs for IP whitelist
	parsedIPs []net.IP

	// parsedTrustedCIDRs caches parsed CIDR networks for trusted proxies
	parsedTrustedCIDRs []*net.IPNet

	// parsedTrustedIPs caches parsed IPs for trusted proxies
	parsedTrustedIPs []net.IP
}

// DefaultConfig returns a Config with sensible defaults
// DefaultConfig returns a configuration safe to expose publicly.
//
// IncludeDetails defaults to false. It used to default to true with no IP
// whitelist, so an unauthenticated /health response carried each probe's raw
// err.Error() -- which for the built-in probes means database DSNs, internal
// hostnames and filesystem paths. Detail is valuable, but it has to be opted
// into alongside a decision about who can see it: use DefaultInternalConfig,
// or set IncludeDetails with an IPWhitelist.
func DefaultConfig() Config {
	return Config{
		ServiceName:    "service",
		Timeout:        5 * time.Second,
		IncludeDetails: false,
		IncludeChecks:  false,
		IPWhitelist:    nil,
		TrustedProxies: nil,
		CriticalChecks: nil,
	}
}

// DefaultInternalConfig returns a configuration that includes per-check detail,
// for an endpoint reachable only from inside the deployment.
//
// Probe errors are included verbatim, so do not expose an aggregator built
// from this to the public internet without an IPWhitelist.
func DefaultInternalConfig() Config {
	c := DefaultConfig()
	c.IncludeDetails = true
	c.IncludeChecks = true
	return c
}

// WithServiceName sets the service name
func (c Config) WithServiceName(name string) Config {
	c.ServiceName = name
	return c
}

// WithTimeout sets the timeout
func (c Config) WithTimeout(timeout time.Duration) Config {
	c.Timeout = timeout
	return c
}

// WithIPWhitelist sets the IP whitelist
func (c Config) WithIPWhitelist(ips []string) Config {
	c.IPWhitelist = ips
	c.parseIPWhitelist()
	return c
}

// WithTrustedProxies sets the trusted proxies list
func (c Config) WithTrustedProxies(ips []string) Config {
	c.TrustedProxies = ips
	c.parseTrustedProxies()
	return c
}

// WithDetails sets whether to include details
func (c Config) WithDetails(include bool) Config {
	c.IncludeDetails = include
	return c
}

// WithChecks sets whether to include individual checks
func (c Config) WithChecks(include bool) Config {
	c.IncludeChecks = include
	return c
}

// WithCriticalChecks sets the list of critical checks
func (c Config) WithCriticalChecks(checks []string) Config {
	c.CriticalChecks = checks
	return c
}

// parseIPWhitelist parses the IP whitelist into networks and IPs
func (c *Config) parseIPWhitelist() {
	c.parsedIPs, c.parsedCIDRs = parseIPEntries(c.IPWhitelist)
}

// parseTrustedProxies parses the trusted proxies into networks and IPs
func (c *Config) parseTrustedProxies() {
	c.parsedTrustedIPs, c.parsedTrustedCIDRs = parseIPEntries(c.TrustedProxies)
}

func parseIPEntries(entries []string) ([]net.IP, []*net.IPNet) {
	var ips []net.IP
	var cidrs []*net.IPNet

	for _, ipStr := range entries {
		ipStr = strings.TrimSpace(ipStr)
		if ipStr == "" {
			continue
		}

		// Try parsing as CIDR first
		if strings.Contains(ipStr, "/") {
			_, network, err := net.ParseCIDR(ipStr)
			if err == nil {
				cidrs = append(cidrs, network)
				continue
			}
		}

		// Try parsing as plain IP
		ip := net.ParseIP(ipStr)
		if ip != nil {
			ips = append(ips, ip)
		}
	}

	return ips, cidrs
}

// IsIPAllowed checks if the given IP is allowed by the whitelist
// Returns true if whitelist is empty (all IPs allowed)
func (c *Config) IsIPAllowed(ipStr string) bool {
	// If no whitelist, allow all
	if len(c.IPWhitelist) == 0 {
		return true
	}

	// Parse on first use if not already parsed
	if len(c.parsedCIDRs) == 0 && len(c.parsedIPs) == 0 && len(c.IPWhitelist) > 0 {
		c.parseIPWhitelist()
	}

	// Parse the input IP
	ip := net.ParseIP(ipStr)
	if ip == nil {
		// Try extracting IP from host:port format
		host, _, err := net.SplitHostPort(ipStr)
		if err != nil {
			return false
		}
		ip = net.ParseIP(host)
		if ip == nil {
			return false
		}
	}

	// Check against parsed IPs
	for _, allowedIP := range c.parsedIPs {
		if allowedIP.Equal(ip) {
			return true
		}
	}

	// Check against parsed CIDRs
	for _, network := range c.parsedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// IsTrustedProxy checks if the given IP belongs to a trusted proxy list
func (c *Config) IsTrustedProxy(ipStr string) bool {
	if len(c.TrustedProxies) == 0 {
		return false
	}

	if len(c.parsedTrustedCIDRs) == 0 && len(c.parsedTrustedIPs) == 0 && len(c.TrustedProxies) > 0 {
		c.parseTrustedProxies()
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		host, _, err := net.SplitHostPort(ipStr)
		if err != nil {
			return false
		}
		ip = net.ParseIP(host)
		if ip == nil {
			return false
		}
	}

	for _, allowedIP := range c.parsedTrustedIPs {
		if allowedIP.Equal(ip) {
			return true
		}
	}

	for _, network := range c.parsedTrustedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// IsCritical checks if a check name is in the critical list
func (c *Config) IsCritical(name string) bool {
	// If no critical checks defined, all checks are critical
	if len(c.CriticalChecks) == 0 {
		return true
	}

	for _, critical := range c.CriticalChecks {
		if critical == name {
			return true
		}
	}

	return false
}
