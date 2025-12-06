package main

import (
	"sync"
	"time"
)

// ServiceState represents the persistent state across restarts (minimal)
type ServiceState struct {
	// Service lifecycle
	ServiceStartTime       time.Time `json:"service_start_time"`
	LastShutdownTime       time.Time `json:"last_shutdown_time"`
	LastSaveTime           time.Time `json:"last_save_time"`
	
	// Restart tracking (essential for cooldown/retry logic)
	LastRestartAttempt     time.Time `json:"last_restart_attempt"`
	LastSuccessfulRestart  time.Time `json:"last_successful_restart"`
	ConsecutiveRestartFails int       `json:"consecutive_restart_fails"`
	TotalRestarts          int       `json:"total_restarts"`
	RestartHistory         []RestartRecord `json:"restart_history"` // Last 10-20 restarts
	
	// Power loss detection
	PowerLossSuspected     bool      `json:"power_loss_suspected"`
	PowerLossDetectedAt    time.Time `json:"power_loss_detected_at"`
	
	// Site monitoring state (minimal - only current status, not statistics)
	SiteStates             map[string]*SiteState `json:"site_states"` // key: site address
	
	// Cluster state
	ClusterLock            *ClusterLock `json:"cluster_lock"`
	
	mu                     sync.RWMutex `json:"-"`
}

// SiteState tracks the minimal state of a monitored site (only what's needed across restarts)
type SiteState struct {
	Name                string        `json:"name"`
	Address             string        `json:"address"`
	IsDown              bool          `json:"is_down"`              // Current up/down status
	DownSince           time.Time     `json:"down_since"`           // When it went down (for tracking)
	LastCheckTime       time.Time     `json:"last_check_time"`      // When last checked (to detect staleness)
}

// SiteStats tracks runtime statistics (in-memory only, NOT persisted)
type SiteStats struct {
	TotalChecks         int64
	SuccessfulChecks    int64
	FailedChecks        int64
	LastLatency         float64
	AverageLatency      float64
	MinLatency          float64
	MaxLatency          float64
	mu                  sync.RWMutex
}

// EventRecord tracks a monitoring event (logged to systemd, not persisted)
type EventRecord struct {
	Timestamp   time.Time     `json:"timestamp"`
	EventType   string        `json:"event_type"` // "site_down", "site_up", "restart_started", "restart_completed", "restart_failed"
	SiteAddress string        `json:"site_address"`
	Message     string        `json:"message"`
	Latency     float64       `json:"latency_ms,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
}

// RestartRecord tracks a router restart attempt (persisted for cooldown logic)
type RestartRecord struct {
	Timestamp       time.Time     `json:"timestamp"`
	Reason          string        `json:"reason"` // "sites_down", "verification_failed", "retry"
	DownSites       []string      `json:"down_sites"`
	Success         bool          `json:"success"`
	Error           string        `json:"error,omitempty"`
	Duration        time.Duration `json:"duration"`
	VerificationOK  bool          `json:"verification_ok"`
	NodeID          string        `json:"node_id"` // Which cluster node performed the restart
}

// ClusterLock represents a distributed lock for restart coordination
type ClusterLock struct {
	IsLocked       bool      `json:"is_locked"`
	LockedBy       string    `json:"locked_by"`   // Node ID
	LockedAt       time.Time `json:"locked_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	LockReason     string    `json:"lock_reason"`
	RestartInProgress bool   `json:"restart_in_progress"`
}

// ClusterNode represents another node in the cluster
type ClusterNode struct {
	NodeID          string    `json:"node_id"`
	URL             string    `json:"url"`
	LastHealthCheck time.Time `json:"last_health_check"`
	IsHealthy       bool      `json:"is_healthy"`
	IsActive        bool      `json:"is_active"`
}

// ClusterStatus represents the current cluster status
type ClusterStatus struct {
	LocalNode       string                   `json:"local_node"`
	Nodes           map[string]*ClusterNode  `json:"nodes"` // key: node ID
	Lock            *ClusterLock             `json:"lock"`
	LastUpdated     time.Time                `json:"last_updated"`
	mu              sync.RWMutex             `json:"-"`
}

// PingResult represents the result of a ping operation
type PingResult struct {
	Success     bool
	Latency     float64 // in milliseconds
	PacketLoss  int     // percentage
	Error       error
}

// VerificationResult represents the result of verifying connectivity
type VerificationResult struct {
	TotalSites      int
	SitesChecked    int
	SitesDown       int
	SitesUp         int
	DownSites       []string
	AllDown         bool
	Error           error
}

// RestartResult represents the result of a restart operation
type RestartResult struct {
	Success         bool
	Started         time.Time
	Completed       time.Time
	Duration        time.Duration
	Error           error
	VerificationOK  bool
	Message         string
}

// APIResponse is a generic API response structure
type APIResponse struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// LockRequest represents a request to acquire the cluster lock
type LockRequest struct {
	NodeID     string   `json:"node_id"`
	Reason     string   `json:"reason"`
	DownSites  []string `json:"down_sites"`
	Timestamp  time.Time `json:"timestamp"`
}

// LockResponse represents a response to a lock request
type LockResponse struct {
	Success    bool      `json:"success"`
	Locked     bool      `json:"locked"`
	LockedBy   string    `json:"locked_by"`
	ExpiresAt  time.Time `json:"expires_at"`
	Message    string    `json:"message"`
}

// HealthCheckResponse represents a health check response
type HealthCheckResponse struct {
	NodeID         string    `json:"node_id"`
	Healthy        bool      `json:"healthy"`
	Uptime         float64   `json:"uptime_seconds"`
	RestartActive  bool      `json:"restart_active"`
	LockHeld       bool      `json:"lock_held"`
	Timestamp      time.Time `json:"timestamp"`
}

// StatusResponse represents a status API response
type StatusResponse struct {
	NodeID              string                   `json:"node_id"`
	ServiceUptime       float64                  `json:"service_uptime_seconds"`
	TotalRestarts       int                      `json:"total_restarts"`
	LastRestartTime     time.Time                `json:"last_restart_time"`
	PowerLossSuspected  bool                     `json:"power_loss_suspected"`
	Sites               map[string]*SiteState    `json:"sites"`
	ClusterEnabled      bool                     `json:"cluster_enabled"`
	ClusterNodes        map[string]*ClusterNode  `json:"cluster_nodes,omitempty"`
	ClusterLock         *ClusterLock             `json:"cluster_lock,omitempty"`
	RestartHistory      []RestartRecord          `json:"restart_history"`
}

// Logger levels
const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// Event types
const (
	EventSiteDown          = "site_down"
	EventSiteUp            = "site_up"
	EventRestartStarted    = "restart_started"
	EventRestartCompleted  = "restart_completed"
	EventRestartFailed     = "restart_failed"
	EventPowerLossDetected = "power_loss_detected"
	EventClusterLockAcquired = "cluster_lock_acquired"
	EventClusterLockReleased = "cluster_lock_released"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	CircuitClosed CircuitBreakerState = iota // Normal operation
	CircuitOpen                              // Failing, reject requests
	CircuitHalfOpen                          // Testing if service recovered
)

// CircuitBreaker implements the circuit breaker pattern for router HTTP calls
type CircuitBreaker struct {
	state            CircuitBreakerState
	failures         int
	successes        int
	lastFailure      time.Time
	lastStateChange  time.Time
	
	// Configuration
	maxFailures      int           // Failures before opening circuit
	resetTimeout     time.Duration // Time before attempting half-open
	halfOpenSuccesses int          // Successes needed to close circuit
	
	mu               sync.RWMutex
}

// DowntimeSignature represents a unique identifier for a connectivity failure event
// Used for deduplication of restart attempts
type DowntimeSignature struct {
	DetectedAt    time.Time // When the outage was first detected (rounded to minute)
	DownSitesHash string    // Hash of down sites for deduplication
}

// RestartCoordinator manages restart synchronization across goroutines
type RestartCoordinator struct {
	// Mutex for single restart handler execution
	handlerMu        sync.Mutex
	
	// Global restart-in-progress flag
	restartActive    bool
	restartStartedAt time.Time
	restartMu        sync.RWMutex
	
	// Deduplication tracking
	lastHandledSignature *DowntimeSignature
	signatureMu          sync.Mutex
	
	// Retry scheduling flag - prevents scheduling duplicate retries
	retryScheduled      bool
	retryScheduledAt    time.Time
	retryMu             sync.Mutex
	
	// Note: Exponential backoff uses state.ConsecutiveRestartFails for persistence
}
