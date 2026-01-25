package health

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, "service", config.ServiceName)
	assert.Equal(t, 5*time.Second, config.Timeout)
	assert.True(t, config.IncludeDetails)
	assert.True(t, config.IncludeChecks)
	assert.Nil(t, config.IPWhitelist)
	assert.Nil(t, config.CriticalChecks)
}

func TestConfigBuilders(t *testing.T) {
	t.Run("WithServiceName", func(t *testing.T) {
		config := DefaultConfig().WithServiceName("myservice")
		assert.Equal(t, "myservice", config.ServiceName)
	})

	t.Run("WithTimeout", func(t *testing.T) {
		config := DefaultConfig().WithTimeout(10 * time.Second)
		assert.Equal(t, 10*time.Second, config.Timeout)
	})

	t.Run("WithIPWhitelist", func(t *testing.T) {
		config := DefaultConfig().WithIPWhitelist([]string{"192.168.1.1", "10.0.0.0/8"})
		assert.Len(t, config.IPWhitelist, 2)
		assert.Len(t, config.parsedIPs, 1)
		assert.Len(t, config.parsedCIDRs, 1)
	})

	t.Run("WithDetails", func(t *testing.T) {
		config := DefaultConfig().WithDetails(false)
		assert.False(t, config.IncludeDetails)
	})

	t.Run("WithChecks", func(t *testing.T) {
		config := DefaultConfig().WithChecks(false)
		assert.False(t, config.IncludeChecks)
	})

	t.Run("WithCriticalChecks", func(t *testing.T) {
		config := DefaultConfig().WithCriticalChecks([]string{"redis", "database"})
		assert.Len(t, config.CriticalChecks, 2)
	})

	t.Run("chained builders", func(t *testing.T) {
		config := DefaultConfig().
			WithServiceName("herald").
			WithTimeout(3 * time.Second).
			WithDetails(true).
			WithCriticalChecks([]string{"redis"})

		assert.Equal(t, "herald", config.ServiceName)
		assert.Equal(t, 3*time.Second, config.Timeout)
		assert.True(t, config.IncludeDetails)
		assert.Len(t, config.CriticalChecks, 1)
	})
}

func TestIsIPAllowed(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		testIP    string
		expected  bool
	}{
		{
			name:      "empty whitelist allows all",
			whitelist: nil,
			testIP:    "192.168.1.1",
			expected:  true,
		},
		{
			name:      "exact IP match",
			whitelist: []string{"192.168.1.1"},
			testIP:    "192.168.1.1",
			expected:  true,
		},
		{
			name:      "IP not in whitelist",
			whitelist: []string{"192.168.1.1"},
			testIP:    "192.168.1.2",
			expected:  false,
		},
		{
			name:      "CIDR match",
			whitelist: []string{"10.0.0.0/8"},
			testIP:    "10.1.2.3",
			expected:  true,
		},
		{
			name:      "CIDR no match",
			whitelist: []string{"10.0.0.0/8"},
			testIP:    "192.168.1.1",
			expected:  false,
		},
		{
			name:      "mixed IP and CIDR",
			whitelist: []string{"192.168.1.1", "10.0.0.0/8"},
			testIP:    "10.5.6.7",
			expected:  true,
		},
		{
			name:      "localhost IPv4",
			whitelist: []string{"127.0.0.1"},
			testIP:    "127.0.0.1",
			expected:  true,
		},
		{
			name:      "localhost IPv6",
			whitelist: []string{"::1"},
			testIP:    "::1",
			expected:  true,
		},
		{
			name:      "IP with port",
			whitelist: []string{"192.168.1.1"},
			testIP:    "192.168.1.1:8080",
			expected:  true,
		},
		{
			name:      "invalid IP",
			whitelist: []string{"192.168.1.1"},
			testIP:    "invalid",
			expected:  false,
		},
		{
			name:      "empty IP",
			whitelist: []string{"192.168.1.1"},
			testIP:    "",
			expected:  false,
		},
		{
			name:      "whitelist with empty string",
			whitelist: []string{"", "192.168.1.1"},
			testIP:    "192.168.1.1",
			expected:  true,
		},
		{
			name:      "whitelist with whitespace",
			whitelist: []string{"  192.168.1.1  "},
			testIP:    "192.168.1.1",
			expected:  true,
		},
		{
			name:      "invalid host with port format",
			whitelist: []string{"192.168.1.1"},
			testIP:    "invalid-host:8080",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig().WithIPWhitelist(tt.whitelist)
			assert.Equal(t, tt.expected, config.IsIPAllowed(tt.testIP))
		})
	}
}

func TestIsCritical(t *testing.T) {
	t.Run("empty critical list means all critical", func(t *testing.T) {
		config := DefaultConfig()
		assert.True(t, config.IsCritical("redis"))
		assert.True(t, config.IsCritical("database"))
		assert.True(t, config.IsCritical("anything"))
	})

	t.Run("specific critical checks", func(t *testing.T) {
		config := DefaultConfig().WithCriticalChecks([]string{"redis", "database"})
		assert.True(t, config.IsCritical("redis"))
		assert.True(t, config.IsCritical("database"))
		assert.False(t, config.IsCritical("cache"))
		assert.False(t, config.IsCritical("external-api"))
	})
}

func TestParseIPWhitelist(t *testing.T) {
	t.Run("lazy parsing on first use", func(t *testing.T) {
		config := Config{
			IPWhitelist: []string{"192.168.1.1", "10.0.0.0/8"},
		}

		// parsedIPs and parsedCIDRs should be empty initially
		assert.Nil(t, config.parsedIPs)
		assert.Nil(t, config.parsedCIDRs)

		// After IsIPAllowed call, they should be populated
		config.IsIPAllowed("192.168.1.1")
		assert.NotNil(t, config.parsedIPs)
		assert.NotNil(t, config.parsedCIDRs)
	})

	t.Run("invalid CIDR is skipped", func(t *testing.T) {
		config := DefaultConfig().WithIPWhitelist([]string{"invalid/cidr", "192.168.1.1"})
		assert.Len(t, config.parsedIPs, 1)
		assert.Len(t, config.parsedCIDRs, 0)
	})
}
