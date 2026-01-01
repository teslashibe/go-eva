// Package health provides health check functionality
package health

import (
	"sync"
	"time"
)

// Status represents overall system health
type Status struct {
	Status        string            `json:"status"` // ok, degraded, unhealthy
	Version       string            `json:"version"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	Components    map[string]Check  `json:"components"`
}

// Check represents a component health check
type Check struct {
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message,omitempty"`
	LastCheck time.Time `json:"last_check"`
}

// Checker tracks health of system components
type Checker struct {
	mu         sync.RWMutex
	version    string
	startTime  time.Time
	components map[string]Check
}

// NewChecker creates a new health checker
func NewChecker(version string) *Checker {
	return &Checker{
		version:    version,
		startTime:  time.Now(),
		components: make(map[string]Check),
	}
}

// SetComponent updates a component's health status
func (c *Checker) SetComponent(name string, healthy bool, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.components[name] = Check{
		Healthy:   healthy,
		Message:   message,
		LastCheck: time.Now(),
	}
}

// GetStatus returns the overall health status
func (c *Checker) GetStatus() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := "ok"
	for _, check := range c.components {
		if !check.Healthy {
			status = "degraded"
			break
		}
	}

	// Copy components map
	components := make(map[string]Check)
	for k, v := range c.components {
		components[k] = v
	}

	return Status{
		Status:        status,
		Version:       c.version,
		UptimeSeconds: int64(time.Since(c.startTime).Seconds()),
		Components:    components,
	}
}

// IsHealthy returns true if all components are healthy
func (c *Checker) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, check := range c.components {
		if !check.Healthy {
			return false
		}
	}
	return true
}

