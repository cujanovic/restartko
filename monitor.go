package main

import (
	"fmt"
	"net"
	"sort"
	"time"
)

// Monitor handles the main monitoring and restart logic
type Monitor struct {
	config         Config
	state          *ServiceState
	clusterManager *ClusterManager
	dnsCache       *DNSCache
	stopChan       chan struct{}
}

// NewMonitor creates a new monitor instance
func NewMonitor(config Config, state *ServiceState) *Monitor {
	// Set default DNS cache TTL
	dnsCacheTTL := time.Duration(config.DNSCacheTTLMinutes) * time.Minute
	if dnsCacheTTL == 0 {
		dnsCacheTTL = 5 * time.Minute
	}
	
	monitor := &Monitor{
		config:   config,
		state:    state,
		dnsCache: NewDNSCache(dnsCacheTTL),
		stopChan: make(chan struct{}),
	}

	// Initialize cluster manager if enabled
	if config.ClusterEnabled {
		monitor.clusterManager = NewClusterManager(config, state)
	}

	return monitor
}

// Start starts the monitoring service
func (m *Monitor) Start() error {
	LogInfo("🎯 Starting RestartKO Monitor Service")
	LogInfo("   • Monitoring Interval: %d seconds", m.config.PingIntervalSeconds)
	LogInfo("   • Monitored Sites: %d", len(m.config.Sites))
	LogInfo("   • Router: %s (%s)", m.config.Router.Address, m.config.Router.Type)
	
	if m.config.ClusterEnabled {
		LogInfo("   • Cluster Mode: ENABLED (Node: %s)", m.config.NodeID)
	} else {
		LogInfo("   • Cluster Mode: DISABLED")
	}

	// Start cluster manager if enabled
	if m.clusterManager != nil {
		if err := m.clusterManager.Start(); err != nil {
			return fmt.Errorf("failed to start cluster manager: %v", err)
		}
	}

	// Check for power loss
	if DetectPowerLoss(m.state, m.config.PowerLossGracePeriodMinutes) {
		LogWarn("⚡ Power loss detected! Waiting %d minutes before resuming normal operations...", 
			m.config.PowerLossRestartDelayMinutes)
		
		m.state.mu.Lock()
		m.state.PowerLossSuspected = true
		m.state.PowerLossDetectedAt = time.Now()
		m.state.mu.Unlock()

		// Wait for power loss delay
		if m.config.PowerLossRestartDelayMinutes > 0 {
			time.Sleep(time.Duration(m.config.PowerLossRestartDelayMinutes) * time.Minute)
		}

		LogInfo("Power loss grace period completed, resuming normal operations")
	}

	// Start state saver goroutine
	go m.stateSaverLoop()

	// Get primary monitoring sites (non-verification-only)
	primarySites := m.getPrimarySites()
	if len(primarySites) == 0 {
		return fmt.Errorf("no primary monitoring sites configured")
	}

	// Get verification sites (sorted by priority)
	verificationSites := m.getVerificationSites()

	// Start DNS cache cleanup goroutine
	go m.dnsCacheCleanupLoop()
	
	// Pre-resolve DNS for all hostnames
	m.preResolveDNS()

	LogInfo("✅ Monitor service started successfully")
	LogInfo("   • Primary monitoring sites: %d", len(primarySites))
	LogInfo("   • Verification sites: %d", len(verificationSites))
	LogInfo("   • DNS Cache TTL: %d minutes", m.config.DNSCacheTTLMinutes)

	// Start monitoring loops for each primary site
	for _, site := range primarySites {
		go m.monitorSiteLoop(site, verificationSites)
	}

	// Wait for stop signal
	<-m.stopChan

	return nil
}

// Stop stops the monitoring service
func (m *Monitor) Stop() error {
	LogInfo("Stopping monitor service...")

	// Stop cluster manager
	if m.clusterManager != nil {
		m.clusterManager.Stop()
	}

	// Save final state
	if err := SaveState(m.state, m.config.StateFilePath); err != nil {
		LogError("Failed to save final state: %v", err)
	}

	close(m.stopChan)
	return nil
}

// monitorSiteLoop monitors a single site continuously
func (m *Monitor) monitorSiteLoop(site Site, verificationSites []Site) {
	// Apply cluster stagger delay if configured
	staggerDelay := m.calculateClusterStagger()
	if staggerDelay > 0 {
		LogInfo("Applying cluster stagger delay of %d seconds for site: %s", staggerDelay, getSiteDisplayName(site))
		time.Sleep(time.Duration(staggerDelay) * time.Second)
	}
	
	ticker := time.NewTicker(time.Duration(m.config.PingIntervalSeconds) * time.Second)
	defer ticker.Stop()

	LogInfo("Started monitoring loop for site: %s", getSiteDisplayName(site))
	
	// Perform initial check immediately after stagger delay
	m.checkSite(site, verificationSites)

	for {
		select {
		case <-ticker.C:
			m.checkSite(site, verificationSites)
		case <-m.stopChan:
			return
		}
	}
}

// calculateClusterStagger calculates the stagger delay for this node
// Returns the number of seconds to delay before starting monitoring
func (m *Monitor) calculateClusterStagger() int {
	// If stagger is disabled or not in cluster mode, return 0
	if m.config.ClusterStaggerSeconds <= 0 || !m.config.ClusterEnabled {
		return 0
	}
	
	// If no other nodes configured, no need to stagger
	if len(m.config.ClusterNodes) == 0 {
		return 0
	}
	
	// Create a sorted list of all node IDs
	var allNodeIDs []string
	
	// If ClusterAllNodeIDs is provided, use it (recommended)
	if len(m.config.ClusterAllNodeIDs) > 0 {
		allNodeIDs = make([]string, len(m.config.ClusterAllNodeIDs))
		copy(allNodeIDs, m.config.ClusterAllNodeIDs)
	} else {
		// Fallback: build list from this node + cluster nodes
		// This assumes cluster_nodes contains node IDs, not URLs
		allNodeIDs = make([]string, 0, len(m.config.ClusterNodes)+1)
		allNodeIDs = append(allNodeIDs, m.config.NodeID)
		allNodeIDs = append(allNodeIDs, m.config.ClusterNodes...)
	}
	
	// Sort to ensure consistent ordering across all nodes
	// Simple bubble sort for deterministic ordering
	for i := 0; i < len(allNodeIDs); i++ {
		for j := i + 1; j < len(allNodeIDs); j++ {
			if allNodeIDs[i] > allNodeIDs[j] {
				allNodeIDs[i], allNodeIDs[j] = allNodeIDs[j], allNodeIDs[i]
			}
		}
	}
	
	// Find this node's position
	nodePosition := -1
	for i, nodeID := range allNodeIDs {
		if nodeID == m.config.NodeID {
			nodePosition = i
			break
		}
	}
	
	// If node not found (shouldn't happen), default to position 0
	if nodePosition < 0 {
		LogWarn("Node ID '%s' not found in cluster list, using position 0", m.config.NodeID)
		nodePosition = 0
	}
	
	// Calculate stagger: distribute evenly across the stagger window
	totalNodes := len(allNodeIDs)
	staggerPerNode := m.config.ClusterStaggerSeconds / totalNodes
	
	offset := nodePosition * staggerPerNode
	
	LogInfo("Cluster stagger: node='%s' position=%d/%d offset=%ds (all nodes: %v)", 
		m.config.NodeID, nodePosition+1, totalNodes, offset, allNodeIDs)
	
	return offset
}

// checkSite checks a single site and handles failures
func (m *Monitor) checkSite(site Site, verificationSites []Site) {
	LogDebug("Checking site: %s", getSiteDisplayName(site))

	// Ping the site with DNS cache
	result := PingSiteWithDNS(site, m.config.PingCount, m.config.PingTimeoutSeconds, m.dnsCache, m.config.UseRawSockets)

	// Update state (this will log the ping result to systemd)
	UpdateSiteState(m.state, site, result)

	// If site is up, nothing to do
	if result.Success && result.PacketLoss < 50 {
		return
	}

	// Site is down - verify with other sites
	LogWarn("⚠️  Site %s appears to be DOWN, verifying with other sites...", getSiteDisplayName(site))

	// Verify connectivity with other sites
	verifyResult := VerifyConnectivityWithDNS(verificationSites, m.config.VerificationSiteCount, m.config, m.dnsCache)

	if !verifyResult.AllDown {
		// Other sites are reachable, so it's just this specific site that's down
		LogInfo("Other sites are reachable - issue is with %s specifically, not internet", getSiteDisplayName(site))
		return
	}

	// All verification sites are down - internet connectivity issue
	LogError("🔴 Multiple sites are down - internet connectivity problem detected!")
	LogError("   Down sites: %v", verifyResult.DownSites)

	// Try to restart router
	m.handleConnectivityFailure(verifyResult.DownSites)
}

// handleConnectivityFailure handles a detected connectivity failure
func (m *Monitor) handleConnectivityFailure(downSites []string) {
	LogInfo("🔧 Handling connectivity failure...")

	// Apply grace period to allow router self-recovery
	if m.config.RestartGracePeriodSeconds > 0 {
		LogInfo("⏳ Applying grace period (%d seconds) to allow router self-recovery...", 
			m.config.RestartGracePeriodSeconds)
		time.Sleep(time.Duration(m.config.RestartGracePeriodSeconds) * time.Second)
		
		// Re-verify connectivity after grace period
		LogInfo("🔍 Re-verifying connectivity after grace period...")
		allSites := m.getAllSites()
		verifyResult := VerifyConnectivityWithDNS(allSites, m.config.VerificationSiteCount, m.config, m.dnsCache)
		
		if !verifyResult.AllDown {
			LogInfo("✅ Connectivity restored during grace period - router recovered on its own!")
			return
		}
		
		LogWarn("⚠️  Connectivity still down after grace period - proceeding with restart logic")
	}

	// Check router uptime for power outage detection
	if m.config.Router.UptimeCheckEnabled {
		routerUptime, powerOutageSuspected, err := GetRouterUptime(m.config.Router)
		if err != nil {
			LogWarn("Failed to check router uptime: %v", err)
		} else if powerOutageSuspected {
			LogWarn("⚡ Power outage detected! Router uptime: %s", formatDuration(routerUptime))
			LogWarn("Applying power loss delay (%d minutes) before attempting restart", 
				m.config.PowerLossRestartDelayMinutes)
			
			// Wait for the power loss delay
			time.Sleep(time.Duration(m.config.PowerLossRestartDelayMinutes) * time.Minute)
			
			// Verify connectivity again after delay
			LogInfo("Re-verifying connectivity after power loss delay...")
			allSites := m.getAllSites()
			verifyResult := VerifyConnectivityWithDNS(allSites, m.config.VerificationSiteCount, m.config, m.dnsCache)
			
			if !verifyResult.AllDown {
				LogInfo("✅ Connectivity restored after power loss - no restart needed")
				return
			}
			
			LogWarn("Connectivity still down after power loss delay - proceeding with restart")
		}
	}

	// Check if we can attempt a restart
	canRestart, reason := CanAttemptRestart(m.state, m.config.RestartCooldownMinutes, m.config.RestartMaxRetries)
	if !canRestart {
		LogWarn("Cannot attempt restart: %s", reason)
		return
	}

	// Try to acquire cluster lock if cluster mode is enabled
	if m.clusterManager != nil {
		acquired, err := m.clusterManager.TryAcquireLock("sites_down", downSites)
		if err != nil {
			LogError("Failed to acquire cluster lock: %v", err)
			return
		}
		if !acquired {
			LogWarn("Could not acquire cluster lock - another node is handling the restart")
			return
		}
		defer m.clusterManager.ReleaseLock()
	}

	// Perform restart
	m.performRestart("sites_down", downSites)
}

// performRestart performs the router restart and verification
func (m *Monitor) performRestart(reason string, downSites []string) {
	LogInfo("🔄 Initiating router restart (reason: %s)", reason)

	// Create restart record
	record := RestartRecord{
		Timestamp: time.Now(),
		Reason:    reason,
		DownSites: downSites,
		NodeID:    m.config.NodeID,
	}

	// Record event
	event := EventRecord{
		Timestamp: time.Now(),
		EventType: EventRestartStarted,
		Message:   fmt.Sprintf("Router restart initiated: %s", reason),
	}
	RecordEvent(m.state, event)

	// Perform the restart
	restartResult := RestartRouter(m.config.Router)
	record.Success = restartResult.Success
	record.Duration = restartResult.Duration

	if !restartResult.Success {
		record.Error = restartResult.Error.Error()
		RecordRestartAttempt(m.state, record)

		LogError("❌ Router restart failed: %v", restartResult.Error)

		event := EventRecord{
			Timestamp: time.Now(),
			EventType: EventRestartFailed,
			Message:   fmt.Sprintf("Router restart failed: %v", restartResult.Error),
		}
		RecordEvent(m.state, event)

		// Check if we should retry
		m.scheduleRetryIfNeeded()
		return
	}

	LogInfo("✅ Router restart command sent successfully")

	// Verify restart success
	allSites := m.getAllSites()
	verifySuccess := VerifyRestartSuccess(allSites, m.config, m.dnsCache)

	record.VerificationOK = verifySuccess
	RecordRestartAttempt(m.state, record)

	if verifySuccess {
		LogInfo("🎉 Router restart completed successfully and verified!")

		event := EventRecord{
			Timestamp: time.Now(),
			EventType: EventRestartCompleted,
			Message:   "Router restart completed and verified successfully",
			Duration:  restartResult.Duration,
		}
		RecordEvent(m.state, event)
	} else {
		LogWarn("⚠️  Router restart completed but verification failed")

		event := EventRecord{
			Timestamp: time.Now(),
			EventType: EventRestartFailed,
			Message:   "Router restart completed but connectivity not restored",
		}
		RecordEvent(m.state, event)

		// Schedule retry
		m.scheduleRetryIfNeeded()
	}

	// Save state after restart attempt
	if err := SaveState(m.state, m.config.StateFilePath); err != nil {
		LogError("Failed to save state after restart: %v", err)
	}
}

// scheduleRetryIfNeeded schedules a restart retry if within limits
func (m *Monitor) scheduleRetryIfNeeded() {
	m.state.mu.RLock()
	consecutiveFails := m.state.ConsecutiveRestartFails
	m.state.mu.RUnlock()

	if consecutiveFails >= m.config.RestartMaxRetries {
		LogError("🛑 Maximum restart retries reached (%d). Manual intervention required.", m.config.RestartMaxRetries)
		return
	}

	retryDelay := time.Duration(m.config.RestartRetryDelayMinutes) * time.Minute
	LogInfo("⏳ Scheduling restart retry in %s (attempt %d/%d)",
		formatDuration(retryDelay), consecutiveFails+1, m.config.RestartMaxRetries)

	// Schedule retry
	go func() {
		time.Sleep(retryDelay)

		// Re-check if we still need to restart
		allSites := m.getAllSites()
		verifyResult := VerifyConnectivityWithDNS(allSites, m.config.VerificationSiteCount, m.config, m.dnsCache)

		if verifyResult.AllDown {
			LogInfo("Connectivity still down - attempting restart retry")
			m.handleConnectivityFailure(verifyResult.DownSites)
		} else {
			LogInfo("✅ Connectivity restored - canceling scheduled retry")
			// Reset consecutive fails since connectivity is restored
			m.state.mu.Lock()
			m.state.ConsecutiveRestartFails = 0
			m.state.mu.Unlock()
		}
	}()
}

// stateSaverLoop periodically saves state
func (m *Monitor) stateSaverLoop() {
	ticker := time.NewTicker(time.Duration(m.config.StateSaveIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := SaveState(m.state, m.config.StateFilePath); err != nil {
				LogError("Failed to save state: %v", err)
			} else {
				LogDebug("State saved successfully")
			}
		case <-m.stopChan:
			return
		}
	}
}

// Helper methods

func (m *Monitor) getPrimarySites() []Site {
	primary := make([]Site, 0)
	for _, site := range m.config.Sites {
		if !site.VerificationOnly {
			primary = append(primary, site)
		}
	}
	return primary
}

func (m *Monitor) getVerificationSites() []Site {
	sites := make([]Site, len(m.config.Sites))
	copy(sites, m.config.Sites)

	// Sort by priority (higher priority first)
	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Priority > sites[j].Priority
	})

	return sites
}

func (m *Monitor) getAllSites() []Site {
	return m.config.Sites
}

// dnsCacheCleanupLoop periodically cleans up expired DNS cache entries
func (m *Monitor) dnsCacheCleanupLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.dnsCache.CleanupExpired()
		case <-m.stopChan:
			return
		}
	}
}

// preResolveDNS pre-resolves DNS for all sites on startup
func (m *Monitor) preResolveDNS() {
	dnsHosts := make([]string, 0)
	
	// Collect all hostnames (not IPs)
	for _, site := range m.config.Sites {
		if net.ParseIP(site.Address) == nil {
			dnsHosts = append(dnsHosts, site.Address)
		}
	}
	
	if len(dnsHosts) == 0 {
		return
	}
	
	LogInfo("🔍 Pre-resolving %d DNS hostnames...", len(dnsHosts))
	startTime := time.Now()
	
	for _, hostname := range dnsHosts {
		resolvedIP, _, err := m.dnsCache.Resolve(hostname)
		if err != nil {
			LogWarn("⚠️  Pre-resolution failed for %s: %v", hostname, err)
		} else {
			LogDebug("✓ Pre-resolved %s → %s", hostname, resolvedIP)
		}
	}
	
	duration := time.Since(startTime)
	LogInfo("✅ Pre-resolved %d DNS hostnames in %v", len(dnsHosts), duration)
}

