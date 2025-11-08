package main

import (
	"fmt"
	"time"

	"github.com/go-ping/ping"
)

// PingSite performs a ping check on a site
func PingSite(site Site, count int, timeoutSeconds int) PingResult {
	return PingSiteWithDNS(site, count, timeoutSeconds, nil, false)
}

// PingSiteWithDNS performs a ping check on a site with DNS cache support
func PingSiteWithDNS(site Site, count int, timeoutSeconds int, dnsCache *DNSCache, useRawSockets bool) PingResult {
	// Resolve DNS if cache is available and address is a hostname
	targetAddress := site.Address
	if dnsCache != nil {
		resolvedIP, ipChanged, err := dnsCache.Resolve(site.Address)
		if err == nil {
			targetAddress = resolvedIP
			
			// Log if IP changed (DDNS detection)
			if ipChanged {
				LogWarn("🔄 IP changed for %s, using new IP: %s", site.Address, resolvedIP)
			}
		} else {
			// DNS resolution failed, but we'll still try with original address
			LogDebug("DNS resolution failed for %s, using original address: %v", site.Address, err)
		}
	}
	
	// Create pinger
	pinger, err := ping.NewPinger(targetAddress)
	if err != nil {
		return PingResult{
			Success: false,
			Error:   fmt.Errorf("failed to create pinger: %v", err),
		}
	}

	pinger.Count = count
	pinger.Timeout = time.Duration(timeoutSeconds) * time.Second
	pinger.SetPrivileged(useRawSockets) // Use raw sockets if enabled (requires CAP_NET_RAW on Linux)

	// Run the ping
	err = pinger.Run()
	if err != nil {
		return PingResult{
			Success:    false,
			PacketLoss: 100,
			Error:      fmt.Errorf("ping failed: %v", err),
		}
	}

	// Get statistics
	stats := pinger.Statistics()
	packetsRecv := stats.PacketsRecv
	packetsSent := stats.PacketsSent

	// Calculate packet loss
	var packetLossPercent int
	if packetsSent > 0 {
		packetLossPercent = int(100 * (packetsSent - packetsRecv) / packetsSent)
	} else {
		packetLossPercent = 100
	}

	success := packetsRecv > 0
	avgRttMs := float64(stats.AvgRtt) / float64(time.Millisecond)

	return PingResult{
		Success:    success,
		Latency:    avgRttMs,
		PacketLoss: packetLossPercent,
		Error:      nil,
	}
}

// QuickPing performs a fast single ping to check if a site is reachable
func QuickPing(address string, timeoutSeconds int) bool {
	site := Site{Address: address}
	result := PingSite(site, 1, timeoutSeconds)
	return result.Success
}

// VerifyConnectivity checks multiple sites to verify internet connectivity
func VerifyConnectivity(sites []Site, count int, config Config) VerificationResult {
	return VerifyConnectivityWithDNS(sites, count, config, nil)
}

// VerifyConnectivityWithDNS checks multiple sites with DNS cache support
func VerifyConnectivityWithDNS(sites []Site, count int, config Config, dnsCache *DNSCache) VerificationResult {
	result := VerificationResult{
		TotalSites:   len(sites),
		SitesChecked: 0,
		SitesDown:    0,
		SitesUp:      0,
		DownSites:    make([]string, 0),
	}
	
	if len(sites) == 0 {
		result.Error = fmt.Errorf("no sites provided for verification")
		return result
	}
	
	// Check the specified number of sites or all if count is more
	checkCount := min(count, len(sites))
	
	LogInfo("Verifying connectivity with %d sites...", checkCount)
	
	for i := 0; i < checkCount; i++ {
		site := sites[i]
		result.SitesChecked++
		
		LogDebug("Checking site %s (%s)...", site.Name, site.Address)
		pingResult := PingSiteWithDNS(site, config.PingCount, config.PingTimeoutSeconds, dnsCache, config.UseRawSockets)
		
		if pingResult.Success && pingResult.PacketLoss < 50 {
			result.SitesUp++
			LogDebug("Site %s is UP (latency: %.2f ms, loss: %d%%)", 
				getSiteDisplayName(site), pingResult.Latency, pingResult.PacketLoss)
		} else {
			result.SitesDown++
			result.DownSites = append(result.DownSites, site.Address)
			LogDebug("Site %s is DOWN (loss: %d%%)", getSiteDisplayName(site), pingResult.PacketLoss)
		}
	}
	
	result.AllDown = (result.SitesUp == 0)
	
	LogInfo("Verification complete: %d up, %d down out of %d checked", 
		result.SitesUp, result.SitesDown, result.SitesChecked)
	
	return result
}
