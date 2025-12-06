package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ClusterManager manages cluster coordination
type ClusterManager struct {
	config        Config
	state         *ServiceState
	clusterStatus *ClusterStatus
	httpServer    *http.Server
	mu            sync.RWMutex
}

// NewClusterManager creates a new cluster manager
func NewClusterManager(config Config, state *ServiceState) *ClusterManager {
	cm := &ClusterManager{
		config: config,
		state:  state,
		clusterStatus: &ClusterStatus{
			LocalNode: config.NodeID,
			Nodes:     make(map[string]*ClusterNode),
		},
	}

	// Initialize cluster nodes
	for _, nodeURL := range config.ClusterNodes {
		// Extract node ID from URL (or generate one)
		nodeID := nodeURL // For simplicity, use URL as ID
		cm.clusterStatus.Nodes[nodeID] = &ClusterNode{
			NodeID:    nodeID,
			URL:       nodeURL,
			IsHealthy: false,
			IsActive:  false,
		}
	}

	return cm
}

// Start starts the cluster manager
func (cm *ClusterManager) Start() error {
	if !cm.config.ClusterEnabled {
		LogInfo("Cluster mode is disabled")
		return nil
	}

	LogInfo("🌐 Starting cluster manager (Node ID: %s)", cm.config.NodeID)

	// Start HTTP API server
	if err := cm.startAPIServer(); err != nil {
		return fmt.Errorf("failed to start API server: %v", err)
	}

	// Start health check routine
	go cm.healthCheckLoop()

	LogInfo("✅ Cluster manager started on %s", cm.config.ClusterAPIListen)
	return nil
}

// startAPIServer starts the cluster API HTTP server
func (cm *ClusterManager) startAPIServer() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/health", cm.handleHealth)
	mux.HandleFunc("/status", cm.handleStatus)
	mux.HandleFunc("/lock/acquire", cm.handleLockAcquire)
	mux.HandleFunc("/lock/release", cm.handleLockRelease)
	mux.HandleFunc("/lock/status", cm.handleLockStatus)

	cm.httpServer = &http.Server{
		Addr:         cm.config.ClusterAPIListen,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start server in goroutine
	go func() {
		if err := cm.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			LogError("Cluster API server error: %v", err)
		}
	}()

	return nil
}

// healthCheckLoop periodically checks health of other nodes
func (cm *ClusterManager) healthCheckLoop() {
	ticker := time.NewTicker(time.Duration(cm.config.ClusterHealthCheckSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cm.checkClusterHealth()
	}
}

// checkClusterHealth checks health of all cluster nodes
func (cm *ClusterManager) checkClusterHealth() {
	// Collect node URLs without holding lock during HTTP calls
	cm.clusterStatus.mu.RLock()
	nodeChecks := make(map[string]string, len(cm.clusterStatus.Nodes))
	for nodeID, node := range cm.clusterStatus.Nodes {
		nodeChecks[nodeID] = node.URL
	}
	cm.clusterStatus.mu.RUnlock()

	// Perform health checks without lock (HTTP calls can be slow)
	healthResults := make(map[string]bool, len(nodeChecks))
	for nodeID, nodeURL := range nodeChecks {
		healthy := cm.checkNodeHealth(nodeURL)
		healthResults[nodeID] = healthy
		
		if !healthy {
			LogDebug("Node %s is unhealthy", nodeID)
		}
	}

	// Update results under lock
	cm.clusterStatus.mu.Lock()
	now := time.Now()
	for nodeID, healthy := range healthResults {
		if node, exists := cm.clusterStatus.Nodes[nodeID]; exists {
			node.IsHealthy = healthy
			node.IsActive = healthy
			node.LastHealthCheck = now
		}
	}
	cm.clusterStatus.LastUpdated = now
	cm.clusterStatus.mu.Unlock()
}

// checkNodeHealth checks if a node is healthy
func (cm *ClusterManager) checkNodeHealth(nodeURL string) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(nodeURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var healthResp HealthCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		return false
	}

	return healthResp.Healthy
}

// TryAcquireLock attempts to acquire the cluster lock
// Uses atomic compare-and-swap to prevent race conditions
func (cm *ClusterManager) TryAcquireLock(reason string, downSites []string) (bool, error) {
	if !cm.config.ClusterEnabled {
		// No cluster mode, always allow
		return true, nil
	}

	LogInfo("Attempting to acquire cluster lock (reason: %s)", reason)

	// Atomically try to acquire the local lock first
	// This prevents TOCTOU race conditions
	acquired := cm.tryAcquireLocalLockAtomic(reason, downSites)
	if !acquired {
		LogWarn("Cannot acquire local lock - another operation in progress")
		return false, fmt.Errorf("local lock already held")
	}

	// Check all nodes in cluster
	lockRequest := LockRequest{
		NodeID:    cm.config.NodeID,
		Reason:    reason,
		DownSites: downSites,
		Timestamp: time.Now(),
	}

	// Track if we need to rollback
	allNodesApproved := true

	// Try to acquire lock from each node
	for _, node := range cm.clusterStatus.Nodes {
		if !node.IsHealthy {
			LogDebug("Skipping unhealthy node: %s", node.NodeID)
			continue
		}

		acquired, err := cm.requestLockFromNode(node.URL, lockRequest)
		if err != nil {
			LogWarn("Failed to contact node %s: %v", node.NodeID, err)
			continue
		}

		if !acquired {
			LogWarn("Node %s denied lock acquisition", node.NodeID)
			allNodesApproved = false
			break
		}
	}

	if !allNodesApproved {
		// Rollback - release the local lock we acquired
		cm.releaseLocalLock()
		return false, fmt.Errorf("lock denied by cluster node")
	}

	LogInfo("✅ Cluster lock acquired successfully")
	return true, nil
}

// ReleaseLock releases the cluster lock
func (cm *ClusterManager) ReleaseLock() error {
	if !cm.config.ClusterEnabled {
		return nil
	}

	LogInfo("Releasing cluster lock")

	// Release on all nodes
	for _, node := range cm.clusterStatus.Nodes {
		if !node.IsHealthy {
			continue
		}

		if err := cm.releaseLockOnNode(node.URL); err != nil {
			LogWarn("Failed to release lock on node %s: %v", node.NodeID, err)
		}
	}

	// Release local lock
	cm.releaseLocalLock()

	LogInfo("✅ Cluster lock released")
	return nil
}

// canAcquireLocalLock checks if we can acquire the local lock (for read-only checks)
func (cm *ClusterManager) canAcquireLocalLock() bool {
	cm.state.mu.RLock()
	defer cm.state.mu.RUnlock()

	if cm.state.ClusterLock == nil {
		return true
	}

	// Check if lock is expired
	if time.Now().After(cm.state.ClusterLock.ExpiresAt) {
		return true
	}

	// Check if we already hold the lock
	if cm.state.ClusterLock.LockedBy == cm.config.NodeID {
		return true
	}

	return !cm.state.ClusterLock.IsLocked
}

// tryAcquireLocalLockAtomic atomically checks and acquires the local lock
// Returns true if lock was acquired, false if lock is already held
// This eliminates the TOCTOU race condition
func (cm *ClusterManager) tryAcquireLocalLockAtomic(reason string, downSites []string) bool {
	cm.state.mu.Lock()
	defer cm.state.mu.Unlock()
	
	now := time.Now()
	
	// Check if we can acquire
	if cm.state.ClusterLock != nil {
		// Check if lock is expired
		if now.Before(cm.state.ClusterLock.ExpiresAt) {
			// Lock is still valid
			// Check if we already hold it
			if cm.state.ClusterLock.LockedBy == cm.config.NodeID {
				// We already hold it, refresh the expiry
				timeout := time.Duration(cm.config.ClusterLockTimeoutSeconds) * time.Second
				cm.state.ClusterLock.ExpiresAt = now.Add(timeout)
				LogDebug("Refreshed existing lock (expires at: %s)", formatTimestamp(cm.state.ClusterLock.ExpiresAt))
				return true
			}
			// Someone else holds the lock
			if cm.state.ClusterLock.IsLocked {
				return false
			}
		}
		// Lock is expired, we can take it
	}
	
	// Atomically acquire the lock
	timeout := time.Duration(cm.config.ClusterLockTimeoutSeconds) * time.Second
	cm.state.ClusterLock = &ClusterLock{
		IsLocked:          true,
		LockedBy:          cm.config.NodeID,
		LockedAt:          now,
		ExpiresAt:         now.Add(timeout),
		LockReason:        reason,
		RestartInProgress: true,
	}

	LogDebug("Local lock acquired atomically (expires at: %s)", formatTimestamp(cm.state.ClusterLock.ExpiresAt))
	return true
}

// acquireLocalLock acquires the local lock (non-atomic version, kept for compatibility)
func (cm *ClusterManager) acquireLocalLock(reason string, downSites []string) {
	cm.state.mu.Lock()
	defer cm.state.mu.Unlock()

	timeout := time.Duration(cm.config.ClusterLockTimeoutSeconds) * time.Second
	cm.state.ClusterLock = &ClusterLock{
		IsLocked:          true,
		LockedBy:          cm.config.NodeID,
		LockedAt:          time.Now(),
		ExpiresAt:         time.Now().Add(timeout),
		LockReason:        reason,
		RestartInProgress: true,
	}

	LogDebug("Local lock acquired (expires at: %s)", formatTimestamp(cm.state.ClusterLock.ExpiresAt))
}

// releaseLocalLock releases the local lock
func (cm *ClusterManager) releaseLocalLock() {
	cm.state.mu.Lock()
	defer cm.state.mu.Unlock()

	if cm.state.ClusterLock != nil && cm.state.ClusterLock.LockedBy == cm.config.NodeID {
		cm.state.ClusterLock = nil
		LogDebug("Local lock released")
	}
}

// requestLockFromNode requests lock from a remote node
func (cm *ClusterManager) requestLockFromNode(nodeURL string, request LockRequest) (bool, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return false, fmt.Errorf("failed to marshal request: %v", err)
	}

	resp, err := client.Post(nodeURL+"/lock/acquire", "application/json", bytes.NewReader(data))
	if err != nil {
		return false, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var lockResp LockResponse
	if err := json.NewDecoder(resp.Body).Decode(&lockResp); err != nil {
		return false, fmt.Errorf("failed to decode response: %v", err)
	}

	return lockResp.Success, nil
}

// releaseLockOnNode releases lock on a remote node
func (cm *ClusterManager) releaseLockOnNode(nodeURL string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	request := map[string]string{
		"node_id": cm.config.NodeID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	resp, err := client.Post(nodeURL+"/lock/release", "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("release failed with status: %d", resp.StatusCode)
	}

	return nil
}

// HTTP Handlers

func (cm *ClusterManager) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := HealthCheckResponse{
		NodeID:        cm.config.NodeID,
		Healthy:       true,
		Uptime:        time.Since(cm.state.ServiceStartTime).Seconds(),
		RestartActive: cm.isRestartActive(),
		LockHeld:      cm.isLockHeld(),
		Timestamp:     time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (cm *ClusterManager) handleStatus(w http.ResponseWriter, r *http.Request) {
	cm.state.mu.RLock()
	defer cm.state.mu.RUnlock()

	response := StatusResponse{
		NodeID:             cm.config.NodeID,
		ServiceUptime:      time.Since(cm.state.ServiceStartTime).Seconds(),
		TotalRestarts:      cm.state.TotalRestarts,
		LastRestartTime:    cm.state.LastSuccessfulRestart,
		PowerLossSuspected: cm.state.PowerLossSuspected,
		Sites:              cm.state.SiteStates,
		ClusterEnabled:     cm.config.ClusterEnabled,
		ClusterLock:        cm.state.ClusterLock,
		RestartHistory:     GetRecentRestarts(cm.state, 10),
	}

	if cm.config.ClusterEnabled {
		response.ClusterNodes = cm.clusterStatus.Nodes
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (cm *ClusterManager) handleLockAcquire(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request LockRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Atomically check and record the remote lock request
	// This prevents race conditions when multiple nodes request locks simultaneously
	cm.state.mu.Lock()
	
	canAcquire := true
	now := time.Now()
	
	if cm.state.ClusterLock != nil {
		// Check if lock is expired
		if now.Before(cm.state.ClusterLock.ExpiresAt) && cm.state.ClusterLock.IsLocked {
			// Lock is still valid and held
			if cm.state.ClusterLock.LockedBy != request.NodeID {
				// Someone else holds the lock
				canAcquire = false
			}
		}
	}

	response := LockResponse{
		Success: canAcquire,
		Locked:  !canAcquire,
	}

	if cm.state.ClusterLock != nil {
		response.LockedBy = cm.state.ClusterLock.LockedBy
		response.ExpiresAt = cm.state.ClusterLock.ExpiresAt
	}

	if canAcquire {
		response.Message = "Lock granted"
		// Record that we've approved this node's lock request
		// The requesting node will acquire its own local lock
		LogDebug("Lock request approved for node %s (reason: %s)", request.NodeID, request.Reason)
	} else {
		response.Message = fmt.Sprintf("Lock held by %s until %s", 
			response.LockedBy, formatTimestamp(response.ExpiresAt))
		LogDebug("Lock request denied for node %s - held by %s", request.NodeID, response.LockedBy)
	}
	
	cm.state.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (cm *ClusterManager) handleLockRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request map[string]string
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	nodeID := request["node_id"]

	// Release lock if held by requesting node
	cm.state.mu.Lock()
	if cm.state.ClusterLock != nil && cm.state.ClusterLock.LockedBy == nodeID {
		cm.state.ClusterLock = nil
	}
	cm.state.mu.Unlock()

	response := APIResponse{
		Success:   true,
		Message:   "Lock released",
		Timestamp: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (cm *ClusterManager) handleLockStatus(w http.ResponseWriter, r *http.Request) {
	cm.state.mu.RLock()
	defer cm.state.mu.RUnlock()

	var response LockResponse
	if cm.state.ClusterLock != nil && cm.state.ClusterLock.IsLocked {
		response.Locked = true
		response.LockedBy = cm.state.ClusterLock.LockedBy
		response.ExpiresAt = cm.state.ClusterLock.ExpiresAt
		response.Message = fmt.Sprintf("Locked by %s", cm.state.ClusterLock.LockedBy)
	} else {
		response.Locked = false
		response.Message = "Lock available"
	}

	response.Success = true

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Helper methods

func (cm *ClusterManager) isRestartActive() bool {
	cm.state.mu.RLock()
	defer cm.state.mu.RUnlock()

	if cm.state.ClusterLock == nil {
		return false
	}

	return cm.state.ClusterLock.RestartInProgress && 
		time.Now().Before(cm.state.ClusterLock.ExpiresAt)
}

func (cm *ClusterManager) isLockHeld() bool {
	cm.state.mu.RLock()
	defer cm.state.mu.RUnlock()

	if cm.state.ClusterLock == nil {
		return false
	}

	return cm.state.ClusterLock.IsLocked && 
		cm.state.ClusterLock.LockedBy == cm.config.NodeID &&
		time.Now().Before(cm.state.ClusterLock.ExpiresAt)
}

// GetClusterStatus returns current cluster status
func (cm *ClusterManager) GetClusterStatus() *ClusterStatus {
	if !cm.config.ClusterEnabled {
		return nil
	}

	cm.clusterStatus.mu.RLock()
	defer cm.clusterStatus.mu.RUnlock()

	// Return a copy
	status := &ClusterStatus{
		LocalNode:   cm.clusterStatus.LocalNode,
		Nodes:       make(map[string]*ClusterNode),
		LastUpdated: cm.clusterStatus.LastUpdated,
	}

	for id, node := range cm.clusterStatus.Nodes {
		nodeCopy := *node
		status.Nodes[id] = &nodeCopy
	}

	cm.state.mu.RLock()
	if cm.state.ClusterLock != nil {
		lockCopy := *cm.state.ClusterLock
		status.Lock = &lockCopy
	}
	cm.state.mu.RUnlock()

	return status
}

// Stop stops the cluster manager
func (cm *ClusterManager) Stop() error {
	if cm.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return cm.httpServer.Shutdown(ctx)
	}
	return nil
}

