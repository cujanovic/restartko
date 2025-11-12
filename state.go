package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// NewServiceState creates a new service state
func NewServiceState() *ServiceState {
	return &ServiceState{
		ServiceStartTime:        time.Now(),
		SiteStates:             make(map[string]*SiteState),
		RestartHistory:         make([]RestartRecord, 0),
		ClusterLock:            nil,
		ConsecutiveRestartFails: 0,
	}
}

// SaveState saves the service state to disk
func SaveState(state *ServiceState, filepath string) error {
	if filepath == "" {
		return fmt.Errorf("state file path is empty")
	}
	
	state.mu.Lock()
	state.LastSaveTime = time.Now()
	state.LastShutdownTime = time.Now() // Update on every save
	state.mu.Unlock()
	
	// Marshal to JSON
	state.mu.RLock()
	data, err := json.MarshalIndent(state, "", "  ")
	state.mu.RUnlock()
	
	if err != nil {
		return fmt.Errorf("failed to marshal state: %v", err)
	}
	
	// Write to temporary file first
	tmpFile := filepath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %v", err)
	}
	
	// Atomic rename
	if err := os.Rename(tmpFile, filepath); err != nil {
		os.Remove(tmpFile) // Cleanup
		return fmt.Errorf("failed to rename state file: %v", err)
	}
	
	LogDebug("State saved to %s", filepath)
	return nil
}

// LoadState loads the service state from disk
func LoadState(filepath string) (*ServiceState, error) {
	if filepath == "" {
		return nil, fmt.Errorf("state file path is empty")
	}
	
	// Check if file exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		LogInfo("No previous state file found, starting fresh")
		return NewServiceState(), nil
	}
	
	// Read state file
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %v", err)
	}
	
	// Check if file is empty
	if len(data) == 0 {
		LogWarn("State file is empty, starting fresh")
		return NewServiceState(), nil
	}
	
	var state ServiceState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupted state - backup and start fresh
		backupPath := filepath + ".corrupted"
		if err := os.Rename(filepath, backupPath); err == nil {
			LogWarn("Corrupted state file backed up to: %s", backupPath)
		}
		return NewServiceState(), nil
	}
	
	LogInfo("State loaded from %s (last save: %s)", filepath, formatTimestamp(state.LastSaveTime))
	return &state, nil
}

// DetectPowerLoss checks if there was a power loss based on state
func DetectPowerLoss(state *ServiceState, config *Config) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	gracePeriodMinutes := config.PowerLossGracePeriodMinutes
	
	// If service just started, check last shutdown time
	serviceRuntime := time.Since(state.ServiceStartTime)
	
	// If we've been running for more than grace period, we're past power loss concerns
	if serviceRuntime > time.Duration(gracePeriodMinutes)*time.Minute {
		return false
	}
	
	// Check if last shutdown was clean (within reasonable time)
	if state.LastShutdownTime.IsZero() {
		// No previous shutdown recorded - could be first run or power loss
		return false
	}
	
	// If service was shutdown recently (within grace period), it's not a power loss
	timeSinceShutdown := time.Since(state.LastShutdownTime)
	if timeSinceShutdown < time.Duration(gracePeriodMinutes)*time.Minute {
		// Clean shutdown within grace period
		return false
	}
	
	// Check router uptime to distinguish between power loss and manual service restart
	// If router has been up longer than the service downtime, it's just a service restart
	routerUptime, _, err := GetRouterUptime(config.Router)
	if err == nil && routerUptime > 0 {
		// Router uptime is available
		if routerUptime > timeSinceShutdown {
			// Router has been up longer than service was down
			// This means only the service was restarted, not the router/power
			LogDebug("Router uptime (%v) > service downtime (%v), not a power loss", 
				routerUptime, timeSinceShutdown)
			return false
		}
		// Router uptime is less than service downtime - likely a power loss
		LogDebug("Router uptime (%v) < service downtime (%v), likely power loss", 
			routerUptime, timeSinceShutdown)
	} else {
		// Could not get router uptime, fall back to time-based detection
		LogDebug("Could not determine router uptime, using time-based power loss detection")
	}
	
	// Likely a power loss if:
	// 1. Service just started (within grace period)
	// 2. Last shutdown was not recent
	// 3. Router uptime (if available) is less than service downtime
	return true
}

// RecordEvent logs a monitoring event to systemd journal (like ping-monitor)
func RecordEvent(state *ServiceState, event EventRecord) {
	// Events are logged to systemd journal, not saved in state (like ping-monitor)
	switch event.EventType {
	case EventSiteDown:
		LogWarn("Event: %s - %s", event.EventType, event.Message)
	case EventSiteUp:
		LogInfo("Event: %s - %s (duration: %s)", event.EventType, event.Message, formatDuration(event.Duration))
	case EventRestartStarted, EventRestartCompleted:
		LogInfo("Event: %s - %s", event.EventType, event.Message)
	case EventRestartFailed:
		LogError("Event: %s - %s", event.EventType, event.Message)
	default:
		LogDebug("Event: %s - %s", event.EventType, event.Message)
	}
}

// UpdateSiteState updates the minimal persistent state for a site (like ping-monitor)
// Statistics are tracked in-memory by the Monitor, not persisted here
func UpdateSiteState(state *ServiceState, site Site, pingResult PingResult) {
	state.mu.Lock()
	defer state.mu.Unlock()
	
	siteState, exists := state.SiteStates[site.Address]
	if !exists {
		siteState = &SiteState{
			Name:    site.Name,
			Address: site.Address,
		}
		state.SiteStates[site.Address] = siteState
	}
	
	now := time.Now()
	siteState.LastCheckTime = now
	
	if pingResult.Success {
		// Log every successful ping to systemd journal (like ping-monitor)
		if pingResult.Latency > 0 {
			LogInfo("✓ %s - latency: %.2fms, packet loss: %d%%", 
				getSiteDisplayName(site), pingResult.Latency, pingResult.PacketLoss)
		} else {
			// Edge case: successful ping but no latency data
			LogInfo("✓ %s - up (no latency data)", getSiteDisplayName(site))
		}
		
		// If site was down, record recovery
		if siteState.IsDown {
			downtime := time.Since(siteState.DownSince)
			siteState.IsDown = false
			
			// Log recovery to systemd journal
			LogInfo("🟢 RECOVERY: %s is now UP (was down for %s, latency: %.2fms)", 
				getSiteDisplayName(site), formatDuration(downtime), pingResult.Latency)
		}
	} else {
		// Log failed ping to systemd journal
		LogWarn("✗ %s - ping failed (packet loss: %d%%)", 
			getSiteDisplayName(site), pingResult.PacketLoss)
		
		// If site just went down, record it
		if !siteState.IsDown {
			siteState.IsDown = true
			siteState.DownSince = now
			
			// Log down alert to systemd journal
			LogError("🔴 ALERT: %s is now DOWN", getSiteDisplayName(site))
		}
	}
}

// CanAttemptRestart checks if we can attempt a restart based on state
func CanAttemptRestart(state *ServiceState, cooldownMinutes int, maxRetries int) (bool, string) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	// Check if we've exceeded max consecutive failures
	if state.ConsecutiveRestartFails >= maxRetries {
		return false, fmt.Sprintf("max consecutive restart failures reached (%d/%d)", 
			state.ConsecutiveRestartFails, maxRetries)
	}
	
	// Check if we're still in cooldown period
	if !state.LastRestartAttempt.IsZero() {
		timeSince := time.Since(state.LastRestartAttempt)
		cooldownDuration := time.Duration(cooldownMinutes) * time.Minute
		
		if timeSince < cooldownDuration {
			return false, fmt.Sprintf("cooldown period active (%.0f/%.0f minutes)", 
				timeSince.Minutes(), cooldownDuration.Minutes())
		}
	}
	
	return true, "restart allowed"
}

// RecordRestartAttempt records a restart attempt in the state
func RecordRestartAttempt(state *ServiceState, record RestartRecord) {
	state.mu.Lock()
	defer state.mu.Unlock()
	
	state.LastRestartAttempt = record.Timestamp
	state.TotalRestarts++
	
	if record.Success {
		state.LastSuccessfulRestart = record.Timestamp
		state.ConsecutiveRestartFails = 0 // Reset on success
	} else {
		state.ConsecutiveRestartFails++
	}
	
	// Add to history
	state.RestartHistory = append(state.RestartHistory, record)
	
	// Keep only last 20 restart records
	if len(state.RestartHistory) > 20 {
		state.RestartHistory = state.RestartHistory[len(state.RestartHistory)-20:]
	}
}

// GetRecentRestarts returns the most recent restart records
func GetRecentRestarts(state *ServiceState, count int) []RestartRecord {
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	if len(state.RestartHistory) == 0 {
		return []RestartRecord{}
	}
	
	if count > len(state.RestartHistory) {
		count = len(state.RestartHistory)
	}
	
	// Return last N records
	start := len(state.RestartHistory) - count
	records := make([]RestartRecord, count)
	copy(records, state.RestartHistory[start:])
	
	return records
}
