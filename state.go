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
		// Backup corrupted file
		backupPath := filepath + ".corrupted"
		if backupErr := os.Rename(filepath, backupPath); backupErr == nil {
			LogWarn("Corrupted state file backed up to: %s", backupPath)
		}
		return nil, fmt.Errorf("failed to parse state file (corrupted): %v", err)
	}
	
	LogInfo("State loaded from %s (last save: %s)", filepath, formatTimestamp(state.LastSaveTime))
	
	return &state, nil
}

// DetectPowerLoss detects if a power loss occurred based on state
func DetectPowerLoss(state *ServiceState, gracePeriodMinutes int) bool {
	if state == nil {
		return false
	}
	
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	// If this is a fresh start (no previous shutdown time), no power loss
	if isZeroTime(state.LastShutdownTime) {
		return false
	}
	
	// Calculate time since last shutdown
	timeSinceShutdown := time.Since(state.LastShutdownTime)
	
	// If shutdown was recent (within grace period), consider it a normal restart
	gracePeriod := time.Duration(gracePeriodMinutes) * time.Minute
	if timeSinceShutdown < gracePeriod {
		LogDebug("Recent shutdown detected (%.1f minutes ago), not a power loss", timeSinceShutdown.Minutes())
		return false
	}
	
	// If there's a significant gap, it might be a power loss
	// But we need to be conservative - could also be a long maintenance window
	// Check if there was an ongoing restart that was interrupted
	if !isZeroTime(state.LastRestartAttempt) {
		timeSinceRestartAttempt := time.Since(state.LastRestartAttempt)
		
		// If a restart was attempted recently and we're now starting up,
		// it could be because the router restart cut our connection/power
		if timeSinceRestartAttempt < 30*time.Minute {
			LogWarn("Restart attempt detected %.1f minutes ago - possible power loss during restart", 
				timeSinceRestartAttempt.Minutes())
			return true
		}
	}
	
	// Long shutdown period - likely maintenance or intentional shutdown
	LogInfo("Long shutdown period detected (%.1f hours) - treating as maintenance", timeSinceShutdown.Hours())
	return false
}

// RecordRestartAttempt records a restart attempt in state
func RecordRestartAttempt(state *ServiceState, record RestartRecord) {
	state.mu.Lock()
	defer state.mu.Unlock()
	
	state.LastRestartAttempt = record.Timestamp
	if record.Success {
		state.LastSuccessfulRestart = record.Timestamp
		state.ConsecutiveRestartFails = 0
	} else {
		state.ConsecutiveRestartFails++
	}
	
	state.TotalRestarts++
	state.RestartHistory = append(state.RestartHistory, record)
	
	// Keep only last 50 restart records
	if len(state.RestartHistory) > 50 {
		state.RestartHistory = state.RestartHistory[len(state.RestartHistory)-50:]
	}
}

// RecordEvent records an event in state
func RecordEvent(state *ServiceState, event EventRecord) {
	state.mu.Lock()
	defer state.mu.Unlock()
	
	// Add to site-specific events if site address is provided
	if event.SiteAddress != "" {
		siteState, exists := state.SiteStates[event.SiteAddress]
		if exists {
			siteState.RecentEvents = append(siteState.RecentEvents, event)
			
			// Keep only last 100 events per site
			if len(siteState.RecentEvents) > 100 {
				siteState.RecentEvents = siteState.RecentEvents[len(siteState.RecentEvents)-100:]
			}
		}
	}
}

// UpdateSiteState updates the state for a specific site
func UpdateSiteState(state *ServiceState, site Site, pingResult PingResult) {
	state.mu.Lock()
	defer state.mu.Unlock()
	
	siteState, exists := state.SiteStates[site.Address]
	if !exists {
		siteState = &SiteState{
			Name:           site.Name,
			Address:        site.Address,
			MinLatency:     -1,
			RecentEvents:   make([]EventRecord, 0),
		}
		state.SiteStates[site.Address] = siteState
	}
	
	now := time.Now()
	siteState.LastCheckTime = now
	siteState.TotalChecks++
	
	if pingResult.Success {
		siteState.SuccessfulChecks++
		
		// Update latency stats
		if pingResult.Latency > 0 {
			siteState.LastLatency = pingResult.Latency
			
			// Update min/max
			if siteState.MinLatency < 0 || pingResult.Latency < siteState.MinLatency {
				siteState.MinLatency = pingResult.Latency
			}
			if pingResult.Latency > siteState.MaxLatency {
				siteState.MaxLatency = pingResult.Latency
			}
			
			// Update average (rolling average)
			if siteState.AverageLatency <= 0 {
				siteState.AverageLatency = pingResult.Latency
			} else {
				// Weighted average: 90% old + 10% new
				siteState.AverageLatency = siteState.AverageLatency*0.9 + pingResult.Latency*0.1
			}
		}
		
		// If site was down, record recovery
		if siteState.IsDown {
			downtime := time.Since(siteState.DownSince)
			siteState.TotalDowntime += downtime
			siteState.IsDown = false
			siteState.LastUpTime = now
			
			event := EventRecord{
				Timestamp:   now,
				EventType:   EventSiteUp,
				SiteAddress: site.Address,
				Message:     fmt.Sprintf("Site recovered after %s", formatDuration(downtime)),
				Duration:    downtime,
				Latency:     pingResult.Latency,
			}
			siteState.RecentEvents = append(siteState.RecentEvents, event)
			
			LogInfo("✅ Site %s recovered (downtime: %s)", getSiteDisplayName(site), formatDuration(downtime))
		}
	} else {
		siteState.FailedChecks++
		
		// If site just went down, record it
		if !siteState.IsDown {
			siteState.IsDown = true
			siteState.DownSince = now
			
			event := EventRecord{
				Timestamp:   now,
				EventType:   EventSiteDown,
				SiteAddress: site.Address,
				Message:     fmt.Sprintf("Site went down (packet loss: %d%%)", pingResult.PacketLoss),
			}
			siteState.RecentEvents = append(siteState.RecentEvents, event)
			
			LogWarn("⚠️  Site %s went down", getSiteDisplayName(site))
		}
	}
	
	// Keep only last 100 events
	if len(siteState.RecentEvents) > 100 {
		siteState.RecentEvents = siteState.RecentEvents[len(siteState.RecentEvents)-100:]
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
	
	// Check cooldown period
	if !isZeroTime(state.LastRestartAttempt) {
		timeSinceLastAttempt := time.Since(state.LastRestartAttempt)
		cooldown := time.Duration(cooldownMinutes) * time.Minute
		
		if timeSinceLastAttempt < cooldown {
			remaining := cooldown - timeSinceLastAttempt
			return false, fmt.Sprintf("restart cooldown active (%.1f minutes remaining)", remaining.Minutes())
		}
	}
	
	return true, ""
}

// GetRecentRestarts returns recent restart attempts
func GetRecentRestarts(state *ServiceState, count int) []RestartRecord {
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	if len(state.RestartHistory) == 0 {
		return []RestartRecord{}
	}
	
	startIdx := len(state.RestartHistory) - count
	if startIdx < 0 {
		startIdx = 0
	}
	
	// Return a copy
	records := make([]RestartRecord, len(state.RestartHistory[startIdx:]))
	copy(records, state.RestartHistory[startIdx:])
	
	return records
}

