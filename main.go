package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Parse command line flags
	configFile := flag.String("config", "config.json", "Path to configuration file")
	testPing := flag.String("test-ping", "", "Test ping a specific site address")
	testRouter := flag.Bool("test-router", false, "Test router restart (dry-run)")
	showStatus := flag.Bool("status", false, "Show current status from state file")
	flag.Parse()

	// Handle test modes
	if *testPing != "" {
		handleTestPing(*testPing)
		return
	}

	if *showStatus {
		handleShowStatus(*configFile)
		return
	}

	// Normal service mode
	log.Printf("🚀 RestartKO - Network Connectivity Monitor & Router Restart Service")
	log.Printf("═══════════════════════════════════════════════════════════════════")

	// Load configuration
	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("❌ Failed to load configuration: %v", err)
	}

	// Set log level
	SetLogLevel(config.LogLevel)

	// Set defaults for optional fields
	if config.StateFilePath == "" {
		config.StateFilePath = "./state.json"
	}
	if config.StateSaveIntervalSeconds <= 0 {
		config.StateSaveIntervalSeconds = 30
	}
	if config.RestartWaitSeconds <= 0 {
		config.RestartWaitSeconds = 60
	}
	if config.PostRestartPingCount <= 0 {
		config.PostRestartPingCount = 3
	}
	if config.Router.TimeoutSeconds <= 0 {
		config.Router.TimeoutSeconds = 30
	}
	if config.ClusterLockTimeoutSeconds <= 0 {
		config.ClusterLockTimeoutSeconds = 300 // 5 minutes default
	}
	if config.ClusterHealthCheckSeconds <= 0 {
		config.ClusterHealthCheckSeconds = 10
	}

	// Validate configuration
	if err := ValidateConfig(config); err != nil {
		log.Fatalf("❌ Configuration validation failed:\n%v", err)
	}

	LogInfo("✅ Configuration loaded and validated successfully")
	LogInfo("   Config file: %s", *configFile)
	LogInfo("   State file: %s", config.StateFilePath)

	// Test router connection if requested
	if *testRouter {
		handleTestRouter(config)
		return
	}

	// Load or initialize state
	state, err := LoadState(config.StateFilePath)
	if err != nil {
		LogWarn("Failed to load previous state: %v", err)
		LogInfo("Starting with fresh state")
		state = NewServiceState()
	}

	// Initialize state for new sites
	for _, site := range config.Sites {
		if _, exists := state.SiteStates[site.Address]; !exists {
			state.SiteStates[site.Address] = &SiteState{
				Name:         site.Name,
				Address:      site.Address,
			}
		}
	}

	// Update service start time
	state.mu.Lock()
	state.ServiceStartTime = time.Now()
	state.mu.Unlock()

	// Create monitor
	monitor := NewMonitor(config, state)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		LogInfo("📡 Shutdown signal received...")
		LogInfo("👋 Shutting down gracefully...")

		// Save final state
		LogInfo("💾 Saving final state...")
		if err := SaveState(state, config.StateFilePath); err != nil {
			LogError("⚠️  Failed to save final state: %v", err)
		} else {
			LogInfo("✅ Final state saved successfully")
		}

		// Stop monitor
		if err := monitor.Stop(); err != nil {
			LogError("Error stopping monitor: %v", err)
		}

		LogInfo("✅ Shutdown complete")
		os.Exit(0)
	}()

	// Start the monitor
	if err := monitor.Start(); err != nil {
		log.Fatalf("❌ Failed to start monitor: %v", err)
	}
}

// handleTestPing tests pinging a specific site
func handleTestPing(address string) {
	fmt.Printf("🏓 Testing ping to: %s\n", address)
	fmt.Println("═══════════════════════════════════════")

	site := Site{
		Name:    address,
		Address: address,
	}

	result := PingSite(site, 4, 10)

	fmt.Printf("\nResults:\n")
	fmt.Printf("  Success: %v\n", result.Success)
	fmt.Printf("  Latency: %.2f ms\n", result.Latency)
	fmt.Printf("  Packet Loss: %d%%\n", result.PacketLoss)

	if result.Error != nil {
		fmt.Printf("  Error: %v\n", result.Error)
	}

	if result.Success {
		fmt.Println("\n✅ Ping successful!")
	} else {
		fmt.Println("\n❌ Ping failed!")
	}
}

// handleTestRouter tests router restart connection
func handleTestRouter(config Config) {
	fmt.Printf("🔧 Testing router connection: %s (%s)\n", config.Router.Address, config.Router.Type)
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("\n⚠️  WARNING: This is a DRY-RUN test.")
	fmt.Println("   The router will NOT actually be restarted.")
	fmt.Println("   This only tests authentication and connectivity.\n")

	// For HTTP routers, test login
	if config.Router.Type == "generic_http" {
		fmt.Println("Testing HTTP login...")

		// Create a test HTTP client
		jar, _ := cookiejar.New(nil)
		client := &http.Client{
			Jar:     jar,
			Timeout: time.Duration(config.Router.TimeoutSeconds) * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: !config.Router.VerifySSL,
				},
			},
		}

		if err := loginRouterHTTP(client, config.Router); err != nil {
			fmt.Printf("❌ Login test FAILED: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("✅ Login test PASSED!")
		fmt.Println("\nRouter configuration appears to be correct.")
		fmt.Println("You can now run the service in normal mode.")
	} else {
		fmt.Printf("⚠️  Test mode not implemented for router type: %s\n", config.Router.Type)
		fmt.Println("The service will attempt to use this configuration when needed.")
	}
}

// handleShowStatus shows current status from state file
func handleShowStatus(configFile string) {
	config, err := loadConfig(configFile)
	if err != nil {
		log.Fatalf("❌ Failed to load configuration: %v", err)
	}

	if config.StateFilePath == "" {
		config.StateFilePath = "./state.json"
	}

	state, err := LoadState(config.StateFilePath)
	if err != nil {
		log.Fatalf("❌ Failed to load state: %v", err)
	}

	fmt.Println("📊 RestartKO Status")
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Printf("\nService:\n")
	fmt.Printf("  Uptime: %s\n", formatDuration(time.Since(state.ServiceStartTime)))
	fmt.Printf("  Total Restarts: %d\n", state.TotalRestarts)
	fmt.Printf("  Last Restart: %s\n", formatTimestamp(state.LastSuccessfulRestart))
	fmt.Printf("  Consecutive Failures: %d\n", state.ConsecutiveRestartFails)

	if state.PowerLossSuspected {
		fmt.Printf("  Power Loss: SUSPECTED (at %s)\n", formatTimestamp(state.PowerLossDetectedAt))
	}

	if state.ClusterLock != nil && state.ClusterLock.IsLocked {
		fmt.Printf("\nCluster Lock:\n")
		fmt.Printf("  Locked: YES\n")
		fmt.Printf("  Locked By: %s\n", state.ClusterLock.LockedBy)
		fmt.Printf("  Expires: %s\n", formatTimestamp(state.ClusterLock.ExpiresAt))
	}

	fmt.Printf("\nSites (%d):\n", len(state.SiteStates))
	for addr, siteState := range state.SiteStates {
		status := "UP"
		if siteState.IsDown {
			status = "DOWN"
		}

		fmt.Printf("  [%s] %s (%s)\n", status, siteState.Name, addr)
		
		if siteState.IsDown {
			fmt.Printf("      Down Since: %s (%s)\n",
				formatTimestamp(siteState.DownSince),
				formatDuration(time.Since(siteState.DownSince)))
		}
	}

	if len(state.RestartHistory) > 0 {
		fmt.Printf("\nRecent Restart History (last 5):\n")
		recentRestarts := GetRecentRestarts(state, 5)
		for i := len(recentRestarts) - 1; i >= 0; i-- {
			r := recentRestarts[i]
			status := "✓"
			if !r.Success {
				status = "✗"
			}
			fmt.Printf("  %s %s - %s (duration: %s, verified: %v)\n",
				status, formatTimestamp(r.Timestamp), r.Reason,
				formatDuration(r.Duration), r.VerificationOK)
			if r.Error != "" {
				fmt.Printf("     Error: %s\n", r.Error)
			}
		}
	}

	fmt.Println()
}

