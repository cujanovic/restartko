package main

import (
	"fmt"
	"log"
	"time"
)

var logLevel = LogLevelInfo

// SetLogLevel sets the global log level
func SetLogLevel(level string) {
	logLevel = level
}

// LogDebug logs a debug message
func LogDebug(format string, args ...interface{}) {
	if logLevel == LogLevelDebug {
		msg := fmt.Sprintf(format, args...)
		log.Printf("[DEBUG] %s", msg)
	}
}

// LogInfo logs an info message
func LogInfo(format string, args ...interface{}) {
	if logLevel == LogLevelDebug || logLevel == LogLevelInfo {
		msg := fmt.Sprintf(format, args...)
		log.Printf("[INFO] %s", msg)
	}
}

// LogWarn logs a warning message
func LogWarn(format string, args ...interface{}) {
	if logLevel != LogLevelError {
		msg := fmt.Sprintf(format, args...)
		log.Printf("[WARN] %s", msg)
	}
}

// LogError logs an error message
func LogError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[ERROR] %s", msg)
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1f seconds", d.Seconds())
	} else if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%d minutes %d seconds", minutes, seconds)
	} else if d < 24*time.Hour {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%d hours %d minutes", hours, minutes)
	} else {
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%d days %d hours", days, hours)
	}
}

// formatTimestamp formats a timestamp
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02 15:04:05")
}

// formatLatency formats latency in ms
func formatLatency(latencyMs float64) string {
	if latencyMs < 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2f ms", latencyMs)
}

// isZeroTime checks if a time is zero
func isZeroTime(t time.Time) bool {
	return t.IsZero() || t.Unix() == 0
}

// timeSince returns duration since a time, or 0 if time is zero
func timeSince(t time.Time) time.Duration {
	if isZeroTime(t) {
		return 0
	}
	return time.Since(t)
}

// timeUntil returns duration until a time, or 0 if time is zero or past
func timeUntil(t time.Time) time.Duration {
	if isZeroTime(t) {
		return 0
	}
	d := time.Until(t)
	if d < 0 {
		return 0
	}
	return d
}

// min returns the minimum of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two ints
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// contains checks if a string slice contains a value
func contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// removeFromSlice removes a value from a string slice
func removeFromSlice(slice []string, value string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != value {
			result = append(result, item)
		}
	}
	return result
}

// getSiteDisplayName returns a display name for a site
func getSiteDisplayName(site Site) string {
	if site.Name != "" {
		return fmt.Sprintf("%s (%s)", site.Name, site.Address)
	}
	return site.Address
}

