package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/crypto/ssh"
)

// Global circuit breaker for router HTTP calls
var routerCircuitBreaker = &CircuitBreaker{
	state:             CircuitClosed,
	maxFailures:       3,
	resetTimeout:      30 * time.Second,
	halfOpenSuccesses: 2,
}

// NewCircuitBreaker creates a new circuit breaker with default settings
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		state:             CircuitClosed,
		maxFailures:       3,
		resetTimeout:      30 * time.Second,
		halfOpenSuccesses: 2,
	}
}

// CanExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastStateChange) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.successes = 0
			cb.lastStateChange = time.Now()
			LogInfo("🔌 Circuit breaker: transitioning to half-open state")
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return true
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	switch cb.state {
	case CircuitHalfOpen:
		cb.successes++
		if cb.successes >= cb.halfOpenSuccesses {
			cb.state = CircuitClosed
			cb.failures = 0
			cb.lastStateChange = time.Now()
			LogInfo("🔌 Circuit breaker: closed (service recovered)")
		}
	case CircuitClosed:
		// Reset failure count on success
		cb.failures = 0
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.failures++
	cb.lastFailure = time.Now()
	
	switch cb.state {
	case CircuitClosed:
		if cb.failures >= cb.maxFailures {
			cb.state = CircuitOpen
			cb.lastStateChange = time.Now()
			LogWarn("🔌 Circuit breaker: opened (too many failures)")
		}
	case CircuitHalfOpen:
		// Any failure in half-open state reopens the circuit
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
		LogWarn("🔌 Circuit breaker: reopened (failure during recovery)")
	}
}

// GetState returns the current circuit breaker state as a string
func (cb *CircuitBreaker) GetState() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	switch cb.state {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// RestartRouter restarts the router based on configuration
func RestartRouter(config RouterConfig) RestartResult {
	startTime := time.Now()
	result := RestartResult{
		Started: startTime,
	}

	LogInfo("🔄 Starting router restart (type: %s, address: %s)", config.Type, config.Address)

	var err error
	switch config.Type {
	case "generic_http":
		err = restartRouterHTTP(config)
	case "ssh":
		err = restartRouterSSH(config)
	case "telnet":
		err = restartRouterTelnet(config)
	default:
		err = fmt.Errorf("unsupported router type: %s", config.Type)
	}

	result.Completed = time.Now()
	result.Duration = result.Completed.Sub(result.Started)

	if err != nil {
		result.Success = false
		result.Error = err
		result.Message = fmt.Sprintf("Failed to restart router: %v", err)
		LogError("❌ Router restart failed: %v", err)
	} else {
		result.Success = true
		result.Message = "Router restart command sent successfully"
		LogInfo("✅ Router restart completed in %s", formatDuration(result.Duration))
	}

	return result
}

// restartRouterHTTP restarts router via HTTP interface
func restartRouterHTTP(config RouterConfig) error {
	// Check circuit breaker before attempting
	if !routerCircuitBreaker.CanExecute() {
		return fmt.Errorf("circuit breaker open - router HTTP calls temporarily disabled (state: %s)", 
			routerCircuitBreaker.GetState())
	}

	// Create HTTP client with cookie jar for session management
	jar, err := cookiejar.New(nil)
	if err != nil {
		routerCircuitBreaker.RecordFailure()
		return fmt.Errorf("failed to create cookie jar: %v", err)
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !config.VerifySSL,
			},
		},
	}

	// Step 1: Login with retry
	LogInfo("🔐 Logging into router at %s", config.LoginURL)
	if err := loginRouterHTTPWithRetry(client, config, 2); err != nil {
		routerCircuitBreaker.RecordFailure()
		return fmt.Errorf("login failed: %v", err)
	}

	LogInfo("✅ Router login successful")

	// Step 2: Get CSRF token if enabled (with retry)
	var csrfToken string
	if config.CSRFEnabled {
		LogInfo("🔑 Fetching CSRF token...")
		token, err := getCSRFTokenWithRetry(client, config, 3)
		if err != nil {
			routerCircuitBreaker.RecordFailure()
			return fmt.Errorf("failed to get CSRF token: %v", err)
		}
		csrfToken = token
		LogInfo("✅ CSRF token obtained: %s", csrfToken)
	}

	// Step 3: Send restart command
	LogInfo("📤 Sending restart command to router")
	if err := sendRestartCommandHTTP(client, config, csrfToken); err != nil {
		routerCircuitBreaker.RecordFailure()
		return fmt.Errorf("restart command failed: %v", err)
	}
	
	// Record success
	routerCircuitBreaker.RecordSuccess()
	LogInfo("✅ Restart command sent successfully")

	return nil
}

// loginRouterHTTPWithRetry performs HTTP login with retry logic
func loginRouterHTTPWithRetry(client *http.Client, config RouterConfig, maxRetries int) error {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := loginRouterHTTP(client, config)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxRetries {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			LogDebug("Login attempt %d failed, retrying in %v: %v", attempt, delay, err)
			time.Sleep(delay)
		}
	}
	return lastErr
}

// getCSRFTokenWithRetry extracts CSRF token with retry logic
func getCSRFTokenWithRetry(client *http.Client, config RouterConfig, maxRetries int) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		token, err := getCSRFToken(client, config)
		if err == nil && token != "" {
			return token, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("empty token returned")
		}
		if attempt < maxRetries {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			LogDebug("CSRF token attempt %d failed, retrying in %v: %v", attempt, delay, lastErr)
			time.Sleep(delay)
		}
	}
	return "", lastErr
}

// loginRouterHTTP performs HTTP login
func loginRouterHTTP(client *http.Client, config RouterConfig) error {
	// Prepare login data
	formData := url.Values{}
	
	// Add configured form fields
	for key, value := range config.LoginFormFields {
		// Replace placeholders
		value = strings.ReplaceAll(value, "{username}", config.Username)
		value = strings.ReplaceAll(value, "{password}", config.Password)
		formData.Set(key, value)
	}

	// If no form fields configured, use defaults
	if len(config.LoginFormFields) == 0 {
		formData.Set("username", config.Username)
		formData.Set("password", config.Password)
	}

	// Create request
	var req *http.Request
	var err error

	encodedFormData := formData.Encode()
	LogDebug("Login form data being sent: %s", encodedFormData)

	if strings.ToUpper(config.LoginMethod) == "GET" {
		// GET request with query parameters
		loginURL := config.LoginURL
		if len(formData) > 0 {
			loginURL += "?" + encodedFormData
		}
		req, err = http.NewRequest("GET", loginURL, nil)
	} else {
		// POST request with form data
		req, err = http.NewRequest("POST", config.LoginURL, strings.NewReader(encodedFormData))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	if err != nil {
		return fmt.Errorf("failed to create login request: %v", err)
	}

	// Add custom headers
	for key, value := range config.CustomHeaders {
		req.Header.Set(key, value)
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response body (some routers require this)
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		LogDebug("Failed to read login response body: %v", err)
	}

	// Check response status (accept 2xx and 3xx as success, since redirects are common for logins)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("login failed with status code: %d", resp.StatusCode)
	}
	
	// Debug: Log cookies received from login response
	loginCookies := resp.Cookies()
	LogDebug("Login response status: %d, cookies received: %d", resp.StatusCode, len(loginCookies))
	for _, cookie := range loginCookies {
		LogDebug("  Login cookie: %s=%s (Domain: %s, Path: %s)", cookie.Name, cookie.Value, cookie.Domain, cookie.Path)
	}

	// Debug: Log all cookies in the jar for this URL
	parsedURL, _ := url.Parse(config.LoginURL)
	jarCookies := client.Jar.Cookies(parsedURL)
	LogDebug("Cookies in jar after login for %s: %d cookies", parsedURL.Host, len(jarCookies))
	for _, cookie := range jarCookies {
		LogDebug("  Jar cookie: %s=%s (Path: %s)", cookie.Name, cookie.Value, cookie.Path)
	}

	// Check for session cookie if specified
	if config.SessionCookieName != "" {
		found := false
		for _, cookie := range jarCookies {
			if cookie.Name == config.SessionCookieName {
				found = true
				LogDebug("Session cookie found in jar: %s", cookie.Name)
				break
			}
		}
		if !found {
			return fmt.Errorf("session cookie not found: %s", config.SessionCookieName)
		}
	}

	return nil
}

// getCSRFToken extracts CSRF token from the router page
func getCSRFToken(client *http.Client, config RouterConfig) (string, error) {
	// Determine which URL to fetch the token from
	tokenURL := config.CSRFTokenURL
	if tokenURL == "" {
		tokenURL = config.RestartURL
	}

	// Create request
	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	// Add custom headers
	for key, value := range config.CustomHeaders {
		req.Header.Set(key, value)
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read response body with size limit
	body, err := readResponseBody(resp)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	if len(body) == 0 {
		return "", fmt.Errorf("empty response body")
	}

	bodyStr := string(body)

	// Extract CSRF token
	var token string

	// Method 1: Use custom regex pattern if provided
	if config.CSRFTokenPattern != "" {
		re, err := regexp.Compile(config.CSRFTokenPattern)
		if err != nil {
			return "", fmt.Errorf("invalid CSRF token pattern: %v", err)
		}
		matches := re.FindStringSubmatch(bodyStr)
		if len(matches) > 1 {
			token = matches[1]
		}
	}

	// Method 2: Extract from input field by name attribute
	if token == "" && config.CSRFTokenInputName != "" {
		// Pattern: <input ... name="csrftoken" value="TOKEN" ...>
		pattern := fmt.Sprintf(`name="%s"\s+value="([^"]+)"`, regexp.QuoteMeta(config.CSRFTokenInputName))
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(bodyStr)
		if len(matches) > 1 {
			token = matches[1]
		}

		// Try reverse order: value="TOKEN" name="csrftoken"
		if token == "" {
			pattern = fmt.Sprintf(`value="([^"]+)"\s+name="%s"`, regexp.QuoteMeta(config.CSRFTokenInputName))
			re = regexp.MustCompile(pattern)
			matches = re.FindStringSubmatch(bodyStr)
			if len(matches) > 1 {
				token = matches[1]
			}
		}
	}

	// Method 3: Default fallback - look for common CSRF token patterns
	if token == "" {
		// Try common patterns
		patterns := []string{
			`name="csrftoken"\s+value="([^"]+)"`,
			`value="([^"]+)"\s+name="csrftoken"`,
			`name="csrf_token"\s+value="([^"]+)"`,
			`value="([^"]+)"\s+name="csrf_token"`,
			`name="_csrf"\s+value="([^"]+)"`,
			`value="([^"]+)"\s+name="_csrf"`,
		}

		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindStringSubmatch(bodyStr)
			if len(matches) > 1 {
				token = matches[1]
				break
			}
		}
	}

	if token == "" {
		return "", fmt.Errorf("CSRF token not found in response")
	}

	return token, nil
}

// sendRestartCommandHTTP sends restart command via HTTP
func sendRestartCommandHTTP(client *http.Client, config RouterConfig, csrfToken string) error {
	// Prepare restart data
	formData := url.Values{}
	
	// Add CSRF token if provided
	if csrfToken != "" {
		csrfFieldName := config.CSRFTokenField
		if csrfFieldName == "" {
			csrfFieldName = "csrftoken" // Default field name
		}
		formData.Set(csrfFieldName, csrfToken)
	}
	
	// Add configured form fields
	for key, value := range config.RestartFormFields {
		formData.Set(key, value)
	}

	// Create request
	var req *http.Request
	var err error

	if strings.ToUpper(config.RestartMethod) == "GET" {
		// GET request with query parameters
		restartURL := config.RestartURL
		if len(formData) > 0 {
			restartURL += "?" + formData.Encode()
		}
		req, err = http.NewRequest("GET", restartURL, nil)
	} else {
		// POST request with form data
		req, err = http.NewRequest("POST", config.RestartURL, strings.NewReader(formData.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	if err != nil {
		return fmt.Errorf("failed to create restart request: %v", err)
	}

	// Add custom headers
	for key, value := range config.CustomHeaders {
		req.Header.Set(key, value)
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("restart request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		LogDebug("Failed to read restart response body: %v", err)
	}

	// Check response status (be lenient - some routers disconnect before responding)
	if resp.StatusCode >= 400 && resp.StatusCode < 600 {
		return fmt.Errorf("restart command failed with status code: %d", resp.StatusCode)
	}

	return nil
}

// restartRouterSSH restarts router via SSH
func restartRouterSSH(config RouterConfig) error {
	// Read and parse allowed host public key if provided
	var hostKeyCallback ssh.HostKeyCallback
	
	if config.SSHHostKeyFile != "" {
		pubKeyBytes, err := os.ReadFile(config.SSHHostKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read SSH host key file '%s': %v", config.SSHHostKeyFile, err)
		}
		
		allowedKey, err := ssh.ParsePublicKey(pubKeyBytes)
		if err != nil {
			return fmt.Errorf("failed to parse SSH host key: %v", err)
		}
		
		hostKeyCallback = ssh.FixedHostKey(allowedKey)
		LogDebug("SSH host key validation enabled using: %s", config.SSHHostKeyFile)
	} else {
		// No host key file configured - use insecure callback with warning
		LogWarn("⚠️  SSH host key validation disabled (no host key file configured)")
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}
	
	// SSH client configuration
	sshConfig := &ssh.ClientConfig{
		User: config.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(config.Password),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Duration(config.TimeoutSeconds) * time.Second,
	}

	// Connect to router
	address := fmt.Sprintf("%s:%d", config.Address, config.Port)
	LogInfo("🔐 Connecting to router via SSH at %s", address)

	client, err := ssh.Dial("tcp", address, sshConfig)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %v", err)
	}
	defer client.Close()

	// Create session
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %v", err)
	}
	defer session.Close()

	// Execute restart command
	LogInfo("📤 Executing SSH restart command: %s", config.RestartCommand)
	
	var outputBuf bytes.Buffer
	session.Stdout = &outputBuf
	session.Stderr = &outputBuf

	err = session.Run(config.RestartCommand)
	
	// Note: SSH session might disconnect during restart, which is expected
	// Check output for success indicators
	output := outputBuf.String()
	if output != "" {
		LogInfo("📋 SSH command output: %s", strings.TrimSpace(output))
	}

	if err != nil {
		// Check if error is due to connection being closed (expected during restart)
		if strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "closed") {
			LogInfo("✅ Connection closed during restart (expected behavior)")
			return nil
		}
		return fmt.Errorf("restart command failed: %v", err)
	}

	return nil
}

// restartRouterTelnet restarts router via Telnet
func restartRouterTelnet(config RouterConfig) error {
	// Note: Telnet implementation requires additional package
	// For now, return not implemented error
	// In a full implementation, use a telnet library like github.com/ziutek/telnet
	
	return fmt.Errorf("telnet support not yet implemented - please use SSH or HTTP")
}

// VerifyRestartSuccess verifies that the restart was successful by pinging sites
func VerifyRestartSuccess(sites []Site, config Config, dnsCache *DNSCache) bool {
	LogInfo("⏳ Verifying restart success (waiting %d seconds for router to come online)...", config.RestartWaitSeconds)
	
	// Wait for router to come back online
	time.Sleep(time.Duration(config.RestartWaitSeconds) * time.Second)
	
	// Try pinging sites
	verificationCount := min(config.VerificationSiteCount, len(sites))
	successCount := 0
	
	LogInfo("🔍 Testing connectivity to %d verification sites...", verificationCount)
	
	for i := 0; i < verificationCount; i++ {
		site := sites[i]
		LogInfo("   Testing %s...", getSiteDisplayName(site))
		
		result := PingSiteWithDNS(site, config.PostRestartPingCount, config.PingTimeoutSeconds, dnsCache, config.UseRawSockets)
		if result.Success && result.PacketLoss < 50 {
			successCount++
			LogInfo("   ✓ %s is reachable (latency: %.2fms)", 
				getSiteDisplayName(site), result.Latency)
		} else {
			LogWarn("   ✗ %s is not reachable (packet loss: %d%%)", 
				getSiteDisplayName(site), result.PacketLoss)
		}
	}
	
	// Consider successful if at least half of verified sites are reachable
	threshold := (verificationCount + 1) / 2
	success := successCount >= threshold
	
	if success {
		LogInfo("✅ Restart verification successful (%d/%d sites reachable)", 
			successCount, verificationCount)
	} else {
		LogWarn("⚠️  Restart verification failed (%d/%d sites reachable, need %d)", 
			successCount, verificationCount, threshold)
	}
	
	return success
}

// maxResponseSize is the maximum size of HTTP response body to read (10MB)
const maxResponseSize = 10 * 1024 * 1024

// readResponseBody reads the response body with a size limit
func readResponseBody(resp *http.Response) ([]byte, error) {
	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	return io.ReadAll(limitedReader)
}

// GetRouterUptime fetches and parses the router uptime from the configured URL
// Returns uptime duration and whether a power outage is suspected
func GetRouterUptime(config RouterConfig) (time.Duration, bool, error) {
	if !config.UptimeCheckEnabled || config.UptimeCheckURL == "" {
		return 0, false, fmt.Errorf("uptime check not enabled or URL not configured")
	}
	
	// Check circuit breaker
	if !routerCircuitBreaker.CanExecute() {
		return 0, false, fmt.Errorf("circuit breaker open - router HTTP calls temporarily disabled")
	}
	
	LogInfo("📊 Checking router uptime from %s", config.UptimeCheckURL)
	
	// Create HTTP client with cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create cookie jar: %v", err)
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !config.VerifySSL,
			},
		},
	}
	
	// Login first if we need authentication
	if config.LoginURL != "" && config.Username != "" {
		LogInfo("🔐 Logging into router for uptime check...")
		if err := loginRouterHTTP(client, config); err != nil {
			LogWarn("⚠️  Login failed for uptime check, trying without auth: %v", err)
			// Continue anyway - the page might be accessible without login
		} else {
			LogInfo("✅ Login successful")
			
			// Debug: Log cookies in jar after login for uptime URL
			if parsedURL, err := url.Parse(config.UptimeCheckURL); err == nil {
				jarCookies := jar.Cookies(parsedURL)
				LogDebug("Cookies in jar after login for uptime URL (%s): %d cookies", parsedURL.Host, len(jarCookies))
				for _, cookie := range jarCookies {
					LogDebug("  Jar cookie: %s=%s (Path: %s)", cookie.Name, cookie.Value, cookie.Path)
				}
			}
		}
	}
	
	// Fetch the uptime page
	req, err := http.NewRequest("GET", config.UptimeCheckURL, nil)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create request: %v", err)
	}
	
	// Add custom headers
	for key, value := range config.CustomHeaders {
		req.Header.Set(key, value)
	}
	
	// Debug: Log cookies being sent with request
	if parsedURL, err := url.Parse(config.UptimeCheckURL); err == nil {
		cookies := jar.Cookies(parsedURL)
		LogDebug("Cookies being sent with uptime request (%s): %d cookies", config.UptimeCheckURL, len(cookies))
		for _, cookie := range cookies {
			LogDebug("  - %s=%s", cookie.Name, cookie.Value)
		}
	}
	
	resp, err := client.Do(req)
	if err != nil {
		routerCircuitBreaker.RecordFailure()
		return 0, false, fmt.Errorf("failed to fetch uptime page: %v", err)
	}
	defer resp.Body.Close()
	
	// Check for error status codes
	// Note: Go's http.Client follows redirects automatically, so 3xx won't appear here.
	// A redirect to login page would result in status 200 with the login page HTML,
	// which will fail during uptime parsing (and be caught there).
	if resp.StatusCode != 200 {
		routerCircuitBreaker.RecordFailure()
		return 0, false, fmt.Errorf("uptime page returned status %d", resp.StatusCode)
	}
	
	body, err := readResponseBody(resp)
	if err != nil {
		routerCircuitBreaker.RecordFailure()
		return 0, false, fmt.Errorf("failed to read response body: %v", err)
	}
	
	LogDebug("Uptime page response: status=%d, body_length=%d bytes", resp.StatusCode, len(body))
	
	if len(body) == 0 {
		routerCircuitBreaker.RecordFailure()
		return 0, false, fmt.Errorf("uptime page returned empty response")
	}
	
	bodyStr := string(body)
	
	// Parse uptime from HTML
	uptime, err := parseRouterUptime(bodyStr, config.UptimePattern)
	if err != nil {
		// Log a larger HTML preview at warn level for troubleshooting
		preview := bodyStr
		if len(preview) > 1000 {
			preview = preview[:1000] + "..."
		}
		LogDebug("Uptime page HTML that failed parsing:\n%s", preview)
		return 0, false, fmt.Errorf("failed to parse uptime: %v", err)
	}
	
	LogInfo("📊 Router uptime: %s", formatDuration(uptime))
	
	// Record success with circuit breaker
	routerCircuitBreaker.RecordSuccess()
	
	// Consider it a power outage if uptime is less than 10 minutes
	powerOutageSuspected := uptime < 10*time.Minute
	
	if powerOutageSuspected {
		LogWarn("⚡ Power outage suspected! Router uptime (%s) is less than 10 minutes", formatDuration(uptime))
	} else {
		LogInfo("✅ Router uptime is normal (no recent power loss detected)")
	}
	
	return uptime, powerOutageSuspected, nil
}

// parseRouterUptime parses uptime from HTML content using proper HTML parsing
// Supports formats like: "24 days 17h:22m:34s", "17h:22m:34s", "2h:30m:15s"
func parseRouterUptime(html string, customPattern string) (time.Duration, error) {
	var uptimeStr string
	
	// Use custom pattern if provided
	// Supports two modes:
	// 1. HTML attribute value (e.g., "atg_system_uptime") - looks for data-i18n="atg_system_uptime"
	// 2. Regex pattern (e.g., "uptime.*?(\d+\s+days)") - extracts via regex
	if customPattern != "" {
		// First, try as HTML attribute selector (no special regex chars)
		// If pattern looks like a simple identifier, treat it as data-i18n value
		if !strings.ContainsAny(customPattern, ".*+?[](){}^$|\\") {
			LogDebug("Using custom pattern as HTML attribute selector: %s", customPattern)
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err == nil {
				// Look for data-i18n attribute matching the pattern
				doc.Find("[data-i18n]").Each(func(i int, s *goquery.Selection) {
					if uptimeStr != "" {
						return
					}
					if dataI18n, exists := s.Attr("data-i18n"); exists {
						if dataI18n == customPattern || strings.Contains(dataI18n, customPattern) {
							// Found the element, get the next td or the content
							nextTd := s.Next()
							if nextTd.Length() > 0 && nextTd.Is("td") {
								uptimeStr = strings.TrimSpace(nextTd.Text())
								LogDebug("Found uptime using attribute selector [data-i18n='%s']: %s", customPattern, uptimeStr)
							}
						}
					}
				})
				
				if uptimeStr != "" {
					return parseUptimeString(uptimeStr)
				}
			}
		}
		
		// Fallback to regex pattern matching
		LogDebug("Using custom pattern as regex: %s", customPattern)
		re, err := regexp.Compile(customPattern)
		if err != nil {
			return 0, fmt.Errorf("invalid uptime pattern: %v", err)
		}
		
		matches := re.FindStringSubmatch(html)
		if len(matches) > 1 {
			uptimeStr = matches[1]
		}
		
		if uptimeStr != "" {
			LogDebug("Found uptime string using custom regex pattern: %s", uptimeStr)
			return parseUptimeString(uptimeStr)
		}
	}
	
	// Parse HTML using goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return 0, fmt.Errorf("failed to parse HTML: %v", err)
	}
	
	// Verify document is valid
	if doc == nil {
		return 0, fmt.Errorf("HTML document is nil after parsing")
	}
	
	// Debug: Log a sample of the HTML to see what we're working with
	htmlPreview := html
	if len(html) > 500 {
		htmlPreview = html[:500] + "..."
	}
	LogDebug("HTML preview: %s", htmlPreview)
	
	// Strategy 1: Look for table cells with uptime-related attributes or content
	// Common patterns: data-i18n="*uptime*", text containing "uptime", etc.
	doc.Find("th, td").Each(func(i int, s *goquery.Selection) {
		if uptimeStr != "" {
			return // Already found
		}
		
		text := strings.TrimSpace(s.Text())
		textLower := strings.ToLower(text)
		
		// Check if this is a header cell containing "uptime"
		if s.Is("th") && strings.Contains(textLower, "uptime") {
			// Get the next td sibling or the td in the same row
			nextTd := s.Next()
			if nextTd.Length() > 0 {
				uptimeStr = strings.TrimSpace(nextTd.Text())
			} else {
				// Try finding td in the same row
				row := s.Parent()
				if row.Is("tr") {
					row.Find("td").Each(func(j int, td *goquery.Selection) {
						if uptimeStr == "" {
							uptimeStr = strings.TrimSpace(td.Text())
						}
					})
				}
			}
		}
		
		// Check if this td contains uptime-like format
		if s.Is("td") && isUptimeFormat(text) {
			uptimeStr = text
		}
		
		// Check data-i18n attribute for uptime
		if dataI18n, exists := s.Attr("data-i18n"); exists {
			if strings.Contains(strings.ToLower(dataI18n), "uptime") {
				// This is the label, get the value from next td
				nextTd := s.Next()
				if nextTd.Length() > 0 && nextTd.Is("td") {
					uptimeStr = strings.TrimSpace(nextTd.Text())
				}
			}
		}
	})
	
	// Strategy 2: If not found, look for any text matching uptime format
	if uptimeStr == "" {
		doc.Find("td").Each(func(i int, s *goquery.Selection) {
			if uptimeStr != "" {
				return
			}
			
			text := strings.TrimSpace(s.Text())
			if isUptimeFormat(text) {
				uptimeStr = text
			}
		})
	}
	
	// Strategy 3: Search raw HTML for JavaScript variables containing uptime
	// Technicolor routers populate uptime via JS: var system_uptime = "73 days 2h:15m:30s"
	if uptimeStr == "" {
		LogDebug("Strategy 3: Searching raw HTML for JS uptime variables")
		
		jsUptimePatterns := []string{
			// var system_uptime = "73 days 2h:15m:30s"
			`(?i)(?:system_?uptime|uptime)\s*=\s*["'](\d+\s+days?\s+\d+h:\d+m:\d+s)["']`,
			`(?i)(?:system_?uptime|uptime)\s*=\s*["'](\d+h:\d+m:\d+s)["']`,
			// data-value="73 days 2h:15m:30s" or value="73 days 2h:15m:30s"
			`(?i)(?:data-value|value)\s*=\s*["'](\d+\s+days?\s+\d+h:\d+m:\d+s)["']`,
			`(?i)(?:data-value|value)\s*=\s*["'](\d+h:\d+m:\d+s)["']`,
			// Generic JS: "73 days 2h:15m:30s" anywhere in script context
			`["'](\d+\s+days?\s+\d+h:\d+m:\d+s)["']`,
			`["'](\d+h:\d+m:\d+s)["']`,
		}
		
		for _, pattern := range jsUptimePatterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindStringSubmatch(html)
			if len(matches) > 1 {
				uptimeStr = strings.TrimSpace(matches[1])
				LogDebug("Strategy 3: Found uptime via JS pattern: %s", uptimeStr)
				break
			}
		}
	}
	
	// Strategy 4: Fallback to searching entire text content
	if uptimeStr == "" {
		bodyText := doc.Find("body").Text()
		LogDebug("Strategy 4: Searching body text for uptime pattern (text length: %d)", len(bodyText))
		
		// Look for uptime pattern in text
		uptimeRe := regexp.MustCompile(`(\d+\s+days?\s+\d+h:\d+m:\d+s|\d+h:\d+m:\d+s)`)
		matches := uptimeRe.FindStringSubmatch(bodyText)
		if len(matches) > 1 {
			uptimeStr = strings.TrimSpace(matches[1])
			LogDebug("Strategy 4: Found uptime via regex: %s", uptimeStr)
		} else {
			LogDebug("Strategy 4: No match found with regex")
			// Try a more lenient pattern
			uptimeRe2 := regexp.MustCompile(`(\d+\s+day[s]?\s+\d+h:\d+m:\d+s)`)
			matches2 := uptimeRe2.FindStringSubmatch(bodyText)
			if len(matches2) > 1 {
				uptimeStr = strings.TrimSpace(matches2[1])
				LogDebug("Strategy 4: Found uptime via lenient regex: %s", uptimeStr)
			}
		}
	}
	
	// Strategy 5: Search raw HTML source (not parsed text) for uptime-like values
	// This catches cases where goquery can't find it but the value is in the raw source
	if uptimeStr == "" {
		LogDebug("Strategy 5: Searching raw HTML source for uptime patterns")
		rawPatterns := []string{
			`(\d+\s+days?\s+\d+h:\d+m:\d+s)`,
			`(\d+h:\d+m:\d+s)`,
			`(\d+\s+days?\s+\d+h\s+\d+m\s+\d+s)`,
			`(\d+h\s+\d+m\s+\d+s)`,
		}
		for _, pattern := range rawPatterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindStringSubmatch(html)
			if len(matches) > 1 {
				uptimeStr = strings.TrimSpace(matches[1])
				LogDebug("Strategy 5: Found uptime in raw HTML: %s", uptimeStr)
				break
			}
		}
	}
	
	if uptimeStr == "" {
		LogWarn("Failed to find uptime in HTML after all strategies")
		// Log some of the body text to help debug
		bodyText := doc.Find("body").Text()
		if len(bodyText) > 200 {
			LogDebug("Body text sample: %s", bodyText[:200])
		}
		return 0, fmt.Errorf("uptime not found in HTML")
	}
	
	LogDebug("Found uptime string: %s", uptimeStr)
	
	// Parse the uptime string into duration
	duration, err := parseUptimeString(uptimeStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse uptime string '%s': %v", uptimeStr, err)
	}
	
	return duration, nil
}

// isUptimeFormat checks if a string looks like an uptime format
func isUptimeFormat(s string) bool {
	// Check for common uptime patterns
	uptimePatterns := []string{
		`^\d+\s+days?\s+\d+h:\d+m:\d+s$`,     // 24 days 17h:22m:34s
		`^\d+h:\d+m:\d+s$`,                    // 17h:22m:34s
		`^\d+\s+days?\s+\d+h\s+\d+m\s+\d+s$`, // 24 days 17h 22m 34s
		`^\d+h\s+\d+m\s+\d+s$`,                // 17h 22m 34s
	}
	
	for _, pattern := range uptimePatterns {
		if matched, _ := regexp.MatchString(pattern, s); matched {
			return true
		}
	}
	
	return false
}

// parseUptimeString converts uptime strings to time.Duration
// Supports: "24 days 17h:22m:34s", "17h:22m:34s", "2h 30m 15s", etc.
func parseUptimeString(uptime string) (time.Duration, error) {
	var days, hours, minutes, seconds int
	
	// Normalize the string
	uptime = strings.TrimSpace(uptime)
	uptime = strings.ReplaceAll(uptime, ":", " ")
	
	// Try to parse "X days Yh Zm Ws" format
	daysRe := regexp.MustCompile(`(\d+)\s*days?`)
	hoursRe := regexp.MustCompile(`(\d+)\s*h`)
	minutesRe := regexp.MustCompile(`(\d+)\s*m`)
	secondsRe := regexp.MustCompile(`(\d+)\s*s`)
	
	if matches := daysRe.FindStringSubmatch(uptime); len(matches) > 1 {
		days, _ = strconv.Atoi(matches[1])
	}
	
	if matches := hoursRe.FindStringSubmatch(uptime); len(matches) > 1 {
		hours, _ = strconv.Atoi(matches[1])
	}
	
	if matches := minutesRe.FindStringSubmatch(uptime); len(matches) > 1 {
		minutes, _ = strconv.Atoi(matches[1])
	}
	
	if matches := secondsRe.FindStringSubmatch(uptime); len(matches) > 1 {
		seconds, _ = strconv.Atoi(matches[1])
	}
	
	// Calculate total duration
	duration := time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second
	
	if duration == 0 {
		return 0, fmt.Errorf("could not parse uptime from: %s", uptime)
	}
	
	return duration, nil
}

