package main

import (
	"net"
	"sync"
	"time"
)

// DNSCacheEntry holds cached DNS resolution with expiry
type DNSCacheEntry struct {
	ResolvedIP  string
	OriginalDNS string
	CachedAt    time.Time
	ExpiresAt   time.Time
}

// DNSCache manages DNS resolution caching
type DNSCache struct {
	entries map[string]*DNSCacheEntry // key: hostname
	ttl     time.Duration
	mu      sync.RWMutex
}

// NewDNSCache creates a new DNS cache with specified TTL
func NewDNSCache(ttl time.Duration) *DNSCache {
	if ttl == 0 {
		ttl = 5 * time.Minute // Default: 5 minutes
	}
	return &DNSCache{
		entries: make(map[string]*DNSCacheEntry),
		ttl:     ttl,
	}
}

// Resolve resolves a hostname to IP, using cache if available and valid
// Returns the resolved IP, whether IP changed, and any error
func (dc *DNSCache) Resolve(address string) (string, bool, error) {
	// Check if it's already an IP address
	if net.ParseIP(address) != nil {
		// It's an IP, no DNS needed
		return address, false, nil
	}

	// Check cache
	dc.mu.RLock()
	if entry, exists := dc.entries[address]; exists {
		now := time.Now()
		
		// If cache is still valid, use it
		if now.Before(entry.ExpiresAt) {
			dc.mu.RUnlock()
			LogDebug("DNS cache hit for %s -> %s", address, entry.ResolvedIP)
			return entry.ResolvedIP, false, nil
		}
	}
	dc.mu.RUnlock()

	// Need to resolve DNS
	LogDebug("Resolving DNS for %s...", address)
	ips, err := net.LookupIP(address)
	if err != nil {
		// If we have an expired cache entry, use it as fallback
		dc.mu.RLock()
		entry, exists := dc.entries[address]
		dc.mu.RUnlock()
		if exists {
			LogWarn("DNS lookup failed for %s, using cached IP %s: %v", address, entry.ResolvedIP, err)
			return entry.ResolvedIP, false, err
		}
		return "", false, err
	}

	if len(ips) == 0 {
		// No IPs resolved, use cache fallback if available
		dc.mu.RLock()
		entry, exists := dc.entries[address]
		dc.mu.RUnlock()
		if exists {
			LogWarn("No IPs resolved for %s, using cached IP %s", address, entry.ResolvedIP)
			return entry.ResolvedIP, false, nil
		}
		return "", false, &net.DNSError{Err: "no IP addresses found", Name: address, IsNotFound: true}
	}

	// Use first IPv4 address (prefer IPv4 for ICMP ping compatibility)
	var resolvedIP string
	for _, ip := range ips {
		if ip.To4() != nil {
			resolvedIP = ip.String()
			break
		}
	}
	
	// If no IPv4, use first IPv6
	if resolvedIP == "" {
		resolvedIP = ips[0].String()
	}

	// Check if IP changed (for DDNS detection)
	dc.mu.RLock()
	entry, exists := dc.entries[address]
	dc.mu.RUnlock()
	
	ipChanged := false
	if exists && entry.ResolvedIP != resolvedIP {
		ipChanged = true
		LogInfo("🔄 DDNS IP changed for %s: %s → %s", address, entry.ResolvedIP, resolvedIP)
	}

	// Update cache
	now := time.Now()
	dc.mu.Lock()
	dc.entries[address] = &DNSCacheEntry{
		ResolvedIP:  resolvedIP,
		OriginalDNS: address,
		CachedAt:    now,
		ExpiresAt:   now.Add(dc.ttl),
	}
	dc.mu.Unlock()
	
	LogDebug("📝 DNS cached: %s → %s (TTL: %v)", address, resolvedIP, dc.ttl)

	return resolvedIP, ipChanged, nil
}

// GetCachedIP returns the cached IP without resolving, or empty string if not cached
func (dc *DNSCache) GetCachedIP(address string) string {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	
	if entry, exists := dc.entries[address]; exists {
		return entry.ResolvedIP
	}
	return ""
}

// InvalidateCache forces a cache entry to be re-resolved on next lookup
func (dc *DNSCache) InvalidateCache(address string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	
	if entry, exists := dc.entries[address]; exists {
		entry.ExpiresAt = time.Now().Add(-1 * time.Minute) // Expire it
		LogDebug("🗑️  DNS cache invalidated for %s", address)
	}
}

// CleanupExpired removes expired cache entries (call periodically)
func (dc *DNSCache) CleanupExpired() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	
	now := time.Now()
	removed := 0
	
	for addr, entry := range dc.entries {
		if now.After(entry.ExpiresAt) {
			delete(dc.entries, addr)
			removed++
		}
	}
	
	if removed > 0 {
		LogDebug("🧹 Cleaned up %d expired DNS cache entries", removed)
	}
}

// GetCacheStats returns cache statistics
func (dc *DNSCache) GetCacheStats() map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	
	stats := make(map[string]interface{})
	stats["total_entries"] = len(dc.entries)
	stats["ttl_minutes"] = dc.ttl.Minutes()
	
	entries := make([]map[string]interface{}, 0)
	now := time.Now()
	
	for _, entry := range dc.entries {
		entryInfo := map[string]interface{}{
			"hostname":       entry.OriginalDNS,
			"ip":             entry.ResolvedIP,
			"cached_at":      entry.CachedAt.Format("2006-01-02 15:04:05"),
			"expires_in_sec": int(entry.ExpiresAt.Sub(now).Seconds()),
		}
		entries = append(entries, entryInfo)
	}
	
	stats["entries"] = entries
	return stats
}

