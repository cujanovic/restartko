package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config represents the configuration structure
type Config struct {
	// Monitoring settings
	PingIntervalSeconds      int      `json:"ping_interval_seconds"`
	PingCount                int      `json:"ping_count"`
	PingTimeoutSeconds       int      `json:"ping_timeout_seconds"`
	VerificationSiteCount    int      `json:"verification_site_count"` // Number of other sites to check when one is down
	
	// Restart settings
	RestartCooldownMinutes   int      `json:"restart_cooldown_minutes"`   // Minimum time between restart attempts
	RestartWaitSeconds       int      `json:"restart_wait_seconds"`       // Wait time after restart before verification
	RestartMaxRetries        int      `json:"restart_max_retries"`        // Max consecutive restart attempts
	RestartRetryDelayMinutes int      `json:"restart_retry_delay_minutes"` // Delay between retry attempts
	PostRestartPingCount     int      `json:"post_restart_ping_count"`    // Number of verification pings after restart
	
	// Cluster settings
	ClusterEnabled           bool     `json:"cluster_enabled"`
	NodeID                   string   `json:"node_id"`                     // Unique identifier for this node
	ClusterNodes             []string `json:"cluster_nodes"`               // List of other node URLs (http://ip:port)
	ClusterAPIPort           int      `json:"cluster_api_port"`
	ClusterAPIListen         string   `json:"cluster_api_listen"`
	ClusterLockTimeoutSeconds int     `json:"cluster_lock_timeout_seconds"` // How long a lock is valid
	ClusterHealthCheckSeconds int     `json:"cluster_health_check_seconds"` // How often to check other nodes
	
	// State persistence
	StateFilePath            string   `json:"state_file_path"`
	StateSaveIntervalSeconds int      `json:"state_save_interval_seconds"`
	
	// Power loss handling
	PowerLossGracePeriodMinutes int   `json:"power_loss_grace_period_minutes"` // Time to wait after startup before assuming power loss
	PowerLossRestartDelayMinutes int   `json:"power_loss_restart_delay_minutes"` // Extra delay after detected power loss
	
	// DNS caching
	DNSCacheTTLMinutes       int      `json:"dns_cache_ttl_minutes"` // How long to cache DNS resolutions
	
	// Raw sockets
	UseRawSockets            bool     `json:"use_raw_sockets"` // Use raw sockets for ICMP (requires privileges)
	
	// Logging
	LogLevel                 string   `json:"log_level"` // debug, info, warn, error
	
	// Sites to monitor
	Sites                    []Site   `json:"sites"`
	
	// Router configuration
	Router                   RouterConfig `json:"router"`
}

// Site represents a site to monitor
type Site struct {
	Name                 string `json:"name"`
	Address              string `json:"address"` // IP or hostname
	Priority             int    `json:"priority"` // Higher priority sites are checked first for verification
	VerificationOnly     bool   `json:"verification_only"` // If true, only used for verification, not primary monitoring
}

// RouterConfig represents router configuration
type RouterConfig struct {
	Type                 string            `json:"type"`      // "generic_http", "telnet", "ssh"
	Address              string            `json:"address"`   // Router IP or hostname
	Username             string            `json:"username"`
	Password             string            `json:"password"`
	
	// HTTP-based router settings
	LoginURL             string            `json:"login_url"`
	RestartURL           string            `json:"restart_url"`
	LoginMethod          string            `json:"login_method"`    // POST, GET
	LoginFormFields      map[string]string `json:"login_form_fields"` // Field name -> value mapping
	RestartMethod        string            `json:"restart_method"`  // POST, GET
	RestartFormFields    map[string]string `json:"restart_form_fields"`
	SessionCookieName    string            `json:"session_cookie_name"` // Optional: cookie name for session
	VerifySSL            bool              `json:"verify_ssl"`
	
	// CSRF token support
	CSRFEnabled          bool              `json:"csrf_enabled"`           // Enable CSRF token handling
	CSRFTokenURL         string            `json:"csrf_token_url"`         // URL to fetch CSRF token from (if different from restart URL)
	CSRFTokenField       string            `json:"csrf_token_field"`       // Form field name for CSRF token (default: "csrftoken")
	CSRFTokenPattern     string            `json:"csrf_token_pattern"`     // Regex pattern to extract token from HTML
	CSRFTokenInputName   string            `json:"csrf_token_input_name"`  // HTML input name attribute to extract token from
	
	// Router uptime check (for power loss detection)
	UptimeCheckEnabled   bool              `json:"uptime_check_enabled"`   // Enable router uptime checking
	UptimeCheckURL       string            `json:"uptime_check_url"`       // URL to fetch router uptime from
	UptimePattern        string            `json:"uptime_pattern"`         // Regex pattern to extract uptime (default: auto-detect)
	
	// SSH/Telnet settings
	Port                 int               `json:"port"`
	RestartCommand       string            `json:"restart_command"` // Command to execute
	
	// Generic settings
	TimeoutSeconds       int               `json:"timeout_seconds"`
	CustomHeaders        map[string]string `json:"custom_headers"`
}

// loadConfig loads configuration from a JSON file
func loadConfig(filename string) (Config, error) {
	var config Config

	data, err := os.ReadFile(filename)
	if err != nil {
		return config, fmt.Errorf("failed to read config file: %v", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("failed to parse config file: %v", err)
	}

	return config, nil
}

// ValidateConfig validates the configuration
func ValidateConfig(config Config) error {
	errors := make([]string, 0)

	// Validate monitoring settings
	if config.PingIntervalSeconds <= 0 {
		errors = append(errors, "ping_interval_seconds must be greater than 0")
	}
	if config.PingIntervalSeconds < 10 {
		errors = append(errors, "ping_interval_seconds should be at least 10 seconds for stability")
	}
	if config.PingCount < 1 {
		errors = append(errors, "ping_count must be at least 1")
	}
	if config.PingCount > 10 {
		errors = append(errors, "ping_count should not exceed 10")
	}
	if config.PingTimeoutSeconds < 1 || config.PingTimeoutSeconds > 60 {
		errors = append(errors, "ping_timeout_seconds must be between 1 and 60")
	}
	if config.VerificationSiteCount < 1 {
		errors = append(errors, "verification_site_count must be at least 1")
	}

	// Validate restart settings
	if config.RestartCooldownMinutes < 1 {
		errors = append(errors, "restart_cooldown_minutes must be at least 1")
	}
	if config.RestartWaitSeconds < 10 {
		errors = append(errors, "restart_wait_seconds should be at least 10 seconds")
	}
	if config.RestartMaxRetries < 1 {
		errors = append(errors, "restart_max_retries must be at least 1")
	}
	if config.RestartMaxRetries > 10 {
		errors = append(errors, "restart_max_retries should not exceed 10 to prevent loops")
	}
	if config.RestartRetryDelayMinutes < 1 {
		errors = append(errors, "restart_retry_delay_minutes must be at least 1")
	}
	if config.PostRestartPingCount < 1 {
		errors = append(errors, "post_restart_ping_count must be at least 1")
	}

	// Validate cluster settings
	if config.ClusterEnabled {
		if config.NodeID == "" {
			errors = append(errors, "node_id cannot be empty when cluster_enabled is true")
		}
		if config.ClusterAPIPort < 1 || config.ClusterAPIPort > 65535 {
			errors = append(errors, "cluster_api_port must be between 1 and 65535")
		}
		if config.ClusterAPIListen == "" {
			errors = append(errors, "cluster_api_listen cannot be empty when cluster_enabled is true")
		}
		if config.ClusterLockTimeoutSeconds < 30 {
			errors = append(errors, "cluster_lock_timeout_seconds should be at least 30 seconds")
		}
		if config.ClusterHealthCheckSeconds < 5 {
			errors = append(errors, "cluster_health_check_seconds should be at least 5 seconds")
		}
	}

	// Validate state persistence
	if config.StateFilePath == "" {
		errors = append(errors, "state_file_path cannot be empty (required for power loss handling)")
	}
	if config.StateSaveIntervalSeconds < 1 {
		errors = append(errors, "state_save_interval_seconds must be at least 1")
	}

	// Validate power loss handling
	if config.PowerLossGracePeriodMinutes < 1 {
		errors = append(errors, "power_loss_grace_period_minutes must be at least 1")
	}
	if config.PowerLossRestartDelayMinutes < 0 {
		errors = append(errors, "power_loss_restart_delay_minutes cannot be negative")
	}

	// Validate log level
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[strings.ToLower(config.LogLevel)] {
		errors = append(errors, "log_level must be one of: debug, info, warn, error")
	}

	// Validate sites
	if len(config.Sites) < 2 {
		errors = append(errors, "at least 2 sites must be configured (one for monitoring, one for verification)")
	}
	
	siteNames := make(map[string]bool)
	siteAddrs := make(map[string]bool)
	primarySiteCount := 0
	
	for i, site := range config.Sites {
		if site.Name == "" {
			errors = append(errors, fmt.Sprintf("sites[%d].name cannot be empty", i))
		}
		if siteNames[site.Name] {
			errors = append(errors, fmt.Sprintf("duplicate site name: %s", site.Name))
		}
		siteNames[site.Name] = true
		
		if site.Address == "" {
			errors = append(errors, fmt.Sprintf("sites[%d].address cannot be empty", i))
		}
		if siteAddrs[site.Address] {
			errors = append(errors, fmt.Sprintf("duplicate site address: %s", site.Address))
		}
		siteAddrs[site.Address] = true
		
		if !site.VerificationOnly {
			primarySiteCount++
		}
	}
	
	if primarySiteCount < 1 {
		errors = append(errors, "at least one site must not be verification_only")
	}

	// Validate router configuration
	if config.Router.Type == "" {
		errors = append(errors, "router.type cannot be empty")
	}
	validRouterTypes := map[string]bool{"generic_http": true, "telnet": true, "ssh": true}
	if !validRouterTypes[config.Router.Type] {
		errors = append(errors, "router.type must be one of: generic_http, telnet, ssh")
	}
	
	if config.Router.Address == "" {
		errors = append(errors, "router.address cannot be empty")
	}
	if config.Router.Username == "" {
		errors = append(errors, "router.username cannot be empty")
	}
	if config.Router.Password == "" {
		errors = append(errors, "router.password cannot be empty")
	}
	
	// Type-specific validation
	switch config.Router.Type {
	case "generic_http":
		if config.Router.LoginURL == "" {
			errors = append(errors, "router.login_url cannot be empty for generic_http type")
		}
		if config.Router.RestartURL == "" {
			errors = append(errors, "router.restart_url cannot be empty for generic_http type")
		}
		if config.Router.LoginMethod == "" {
			config.Router.LoginMethod = "POST" // Default
		}
		if config.Router.RestartMethod == "" {
			config.Router.RestartMethod = "POST" // Default
		}
	case "ssh", "telnet":
		if config.Router.Port <= 0 {
			if config.Router.Type == "ssh" {
				config.Router.Port = 22 // Default SSH port
			} else {
				config.Router.Port = 23 // Default Telnet port
			}
		}
		if config.Router.RestartCommand == "" {
			errors = append(errors, "router.restart_command cannot be empty for ssh/telnet type")
		}
	}
	
	if config.Router.TimeoutSeconds <= 0 {
		config.Router.TimeoutSeconds = 30 // Default
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}

