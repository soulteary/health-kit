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
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() Config {
	return Config{
		ServiceName:    "service",
		Timeout:        5 * time.Second,
		IncludeDetails: true,
		IncludeChecks:  true,
		IPWhitelist:    nil,
		CriticalChecks: nil,
	}
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
	c.parsedCIDRs = nil
	c.parsedIPs = nil

	for _, ipStr := range c.IPWhitelist {
		ipStr = strings.TrimSpace(ipStr)
		if ipStr == "" {
			continue
		}

		// Try parsing as CIDR first
		if strings.Contains(ipStr, "/") {
			_, network, err := net.ParseCIDR(ipStr)
			if err == nil {
				c.parsedCIDRs = append(c.parsedCIDRs, network)
				continue
			}
		}

		// Try parsing as plain IP
		ip := net.ParseIP(ipStr)
		if ip != nil {
			c.parsedIPs = append(c.parsedIPs, ip)
		}
	}
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
