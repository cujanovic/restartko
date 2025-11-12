# RestartKO

**Network Connectivity Monitor & Automatic Router Restart Service**

RestartKO is a robust Golang service that monitors internet connectivity by pinging configured sites and automatically restarts your router when connectivity is lost. It includes cluster coordination for running multiple instances, power loss detection, CSRF token support, DNS caching, and comprehensive state management.

---

## Table of Contents

### Getting Started
- [Features](#features)
- [Quick Start](#quick-start)
  - [Installation](#installation)
  - [Configuration](#configuration)
  - [Running the Service](#running-the-service)
- [Command Line Tools](#command-line-tools)

### Configuration
- [Configuration Reference](#configuration-reference)
  - [Monitoring Settings](#monitoring-settings)
  - [Restart Settings](#restart-settings)
  - [Cluster Settings](#cluster-settings)
  - [DNS Caching](#dns-caching)
  - [Power Loss Handling](#power-loss-handling)
  - [Sites Configuration](#sites-configuration)
- [Site Monitoring Recommendations](#site-monitoring-recommendations)
  - [General Principles](#general-principles)
  - [Country-Specific Recommendations](#country-specific-recommendations)
  - [How to Find Your Local ISP DNS](#how-to-find-your-local-isp-dns)
  - [Testing Your Configuration](#testing-your-configuration)
  - [Priority Configuration](#priority-configuration)
  - [Common Mistakes to Avoid](#common-mistakes-to-avoid)
  
### Router Configuration
- [Supported Routers](#supported-routers)
- [Router Configuration Examples](#router-configuration-examples)
  - [Technicolor Routers](#technicolor-routers)
  - [Generic HTTP Routers](#generic-http-routers)
  - [OpenWrt/LEDE](#openwrtlede)
  - [MikroTik](#mikrotik)
  - [Ubiquiti](#ubiquiti)
  - [TP-Link](#tp-link)
  - [ASUS](#asus)
  
### CSRF Token Support
- [CSRF Overview](#csrf-token-support)
- [CSRF Configuration](#csrf-configuration)
- [Token Extraction Methods](#token-extraction-methods)
- [Finding CSRF Tokens](#finding-csrf-token-information)
- [CSRF Troubleshooting](#csrf-troubleshooting)

### Advanced Features
- [Cluster Coordination](#cluster-coordination)
- [Power Loss Detection](#power-loss-detection-advanced)
- [State Management](#state-management)
- [Site Monitoring Strategies](#site-monitoring-strategies)
- [Restart Logic](#restart-logic)

### Deployment & Operations
- [Systemd Service](#systemd-service)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)

### Reference
- [Architecture](#architecture)
- [Changelog](#changelog)
- [License](#license)
- [Contributing](#contributing)

---

## Features

- ✅ **Multi-Site Monitoring** - Monitor multiple sites simultaneously with priority-based checking
- 🔄 **Automatic Router Restart** - Restart router when connectivity fails with intelligent verification
- 🌐 **Cluster Coordination** - Run multiple instances with distributed locking to prevent conflicts
- ⚡ **Power Loss Detection** - Handle unexpected power outages gracefully with grace periods
- 💾 **Persistent State** - Maintain state, statistics, and history across restarts
- 🔐 **Multiple Router Types** - Support for HTTP, SSH, and Telnet routers
- 🛡️ **CSRF Token Support** - Handles routers with CSRF protection (Technicolor, OpenWrt/LuCI, etc.)
- 🗄️ **DNS Caching** - Reduce DNS load, fallback on failure, DDNS IP change detection
- 📊 **Status Monitoring** - Check service status, statistics, and restart history
- 🧪 **Testing Tools** - Test ping and router connectivity before deployment
- 📈 **Statistics Tracking** - Per-site uptime, latency (min/avg/max), failure counts
- 🔒 **Distributed Locking** - Prevent multiple nodes from restarting simultaneously
- ⏱️ **Cooldown & Retries** - Configurable cooldown periods and maximum retry limits
- 🎯 **Smart Verification** - Multi-site verification to reduce false positives

---

## Quick Start

### Installation

```bash
# Clone or copy the service to your machine
cd restartko

# Install dependencies
go mod download

# Build the service
go build -o restartko

# Or use the install script
chmod +x install.sh
./install.sh
```

### Configuration

1. Copy and edit the configuration file:

```bash
cp config.json my-config.json
nano my-config.json
```

2. Configure your router details:

For **Technicolor routers** (with CSRF):
```bash
cp config.technicolor.json my-config.json
nano my-config.json
```

For **generic HTTP routers**:
```json
{
  "router": {
    "type": "generic_http",
    "address": "192.168.1.1",
    "username": "admin",
    "password": "your-router-password",
    "login_url": "http://192.168.1.1/login.cgi",
    "restart_url": "http://192.168.1.1/reboot.cgi",
    "csrf_enabled": false
  }
}
```

3. Configure sites to monitor:

```json
{
  "sites": [
    {
      "name": "Google DNS",
      "address": "8.8.8.8",
      "priority": 100,
      "verification_only": false
    },
    {
      "name": "Cloudflare DNS",
      "address": "1.1.1.1",
      "priority": 90,
      "verification_only": false
    }
  ]
}
```

### Running the Service

```bash
# Start the service
./restartko -config config.json

# Test ping to a site
./restartko -test-ping 8.8.8.8

# Test router connection (dry-run, no actual restart)
./restartko -test-router -config config.json

# Show current status and statistics
./restartko -status -config config.json
```

---

## Command Line Tools

```bash
# Normal operation
./restartko -config config.json

# Test ping connectivity
./restartko -test-ping <address>
# Example: ./restartko -test-ping google.com

# Test router configuration (dry-run)
./restartko -test-router -config config.json

# Show service status
./restartko -status -config config.json

# Help
./restartko -h
```

---

## Configuration Reference

### Monitoring Settings

```json
{
  "ping_interval_seconds": 30,
  "ping_count": 3,
  "ping_timeout_seconds": 10,
  "verification_site_count": 3
}
```

- `ping_interval_seconds` - Time between ping checks (default: 30)
- `ping_count` - Number of pings per check (default: 3)
- `ping_timeout_seconds` - Timeout for each ping (default: 10)
- `verification_site_count` - Number of sites to check for verification (default: 3)

### Restart Settings

```json
{
  "restart_cooldown_minutes": 15,
  "restart_grace_period_seconds": 60,
  "restart_wait_seconds": 180,
  "restart_max_retries": 3,
  "restart_retry_delay_minutes": 10,
  "post_restart_ping_count": 5
}
```

- `restart_cooldown_minutes` - Minimum time between restart attempts (default: 15)
- `restart_grace_period_seconds` - Wait time before restart to allow router self-recovery (default: 60)
- `restart_wait_seconds` - Wait time after restart before verification (default: 180)
- `restart_max_retries` - Maximum consecutive restart attempts (default: 3)
- `restart_retry_delay_minutes` - Delay between retry attempts (default: 10)
- `post_restart_ping_count` - Number of pings for verification (default: 5)

### Cluster Settings

Enable cluster mode to run multiple instances with coordination:

```json
{
  "cluster_enabled": true,
  "node_id": "node-1",
  "cluster_nodes": [
    "http://192.168.1.10:8080",
    "http://192.168.1.11:8080"
  ],
  "cluster_api_port": 8080,
  "cluster_api_listen": "0.0.0.0:8080",
  "cluster_lock_timeout_seconds": 300,
  "cluster_health_check_seconds": 10,
  "cluster_stagger_seconds": 15,
  "cluster_all_node_ids": ["node-1", "node-2", "node-3"]
}
```

- `cluster_enabled` - Enable cluster coordination (default: false)
- `node_id` - Unique identifier for this node
- `cluster_nodes` - List of other node URLs (for health checks and locking)
- `cluster_all_node_ids` - **List of ALL node IDs in the cluster** (required for stagger calculation - must be identical on all nodes)
- `cluster_api_port` - Port for cluster API
- `cluster_api_listen` - Listen address for API (default: "0.0.0.0:8080")
- `cluster_lock_timeout_seconds` - Lock expiry time (default: 300)
- `cluster_health_check_seconds` - Health check interval (default: 10)
- `cluster_stagger_seconds` - Stagger ping checks across nodes to avoid simultaneous checks (0=disabled, recommended: 15-30)

### DNS Caching

```json
{
  "dns_cache_ttl_minutes": 5
}
```

- `dns_cache_ttl_minutes` - How long to cache DNS resolutions (default: 5)

Benefits:
- Reduces DNS lookup overhead
- Fallback to cached IP if DNS fails
- Detects DDNS IP changes
- Pre-resolves hostnames on startup

### Raw Sockets

```json
{
  "use_raw_sockets": false
}
```

- `use_raw_sockets` - Use raw sockets for ICMP ping (default: false)

**Benefits of raw sockets:**
- More reliable and accurate ping timing
- Better performance and lower latency
- Works consistently across different systems

**Requirements:**
- **Linux**: Run as root OR set capabilities:
  ```bash
  sudo setcap cap_net_raw+ep ./restartko
  ```
- **macOS**: Requires root (use `sudo`)
- **Windows**: Requires administrator privileges

**When to use:**
- `false` (default): Uses unprivileged mode, works without special permissions
- `true`: Better performance but requires elevated privileges

### Power Loss Handling

```json
{
  "power_loss_grace_period_minutes": 5,
  "power_loss_restart_delay_minutes": 2
}
```

- `power_loss_grace_period_minutes` - Time window to consider restart as normal (default: 5)
- `power_loss_restart_delay_minutes` - Extra delay after power loss (default: 2)

**How it works:**
1. When RestartKO starts, it checks if the last shutdown was clean
2. If not (e.g., power loss), it waits `power_loss_grace_period_minutes` before taking action
3. **If router uptime check is enabled**: Checks router's System Uptime to detect recent power outages
4. After a power loss is detected, it applies `power_loss_restart_delay_minutes` before restarting
5. Re-verifies connectivity after the delay - if restored, skips the restart
6. This prevents unnecessary restarts when the ISP or router is still recovering from power loss

### Router Uptime Check (Power Outage Detection)

```json
{
  "router": {
    "uptime_check_enabled": true,
    "uptime_check_url": "http://192.168.0.1/st_gateway.html",
    "uptime_pattern": "atg_system_uptime"
  }
}
```

- `uptime_check_enabled` - Enable router uptime checking (default: false)
- `uptime_check_url` - URL to fetch router uptime from (e.g., status/info page)
- `uptime_pattern` - Optional: Custom pattern to extract uptime (auto-detects if empty)
  - **HTML attribute mode**: Simple string like `"atg_system_uptime"` → Matches `data-i18n="atg_system_uptime"`
  - **Regex mode**: Pattern with special chars like `"uptime.*?(\\d+ days)"` → Extracts via regex
  - **Empty string**: Uses automatic detection with multiple fallback strategies

**Benefits:**
- **Accurate power outage detection**: Checks router's actual uptime instead of relying only on service uptime
- **Prevents unnecessary restarts**: If router uptime is < 10 minutes when connectivity fails, it's likely a power outage
- **Smart pattern matching**: Supports both HTML attribute selectors and regex patterns
- **Automatic parsing**: Uses proper HTML parsing (goquery) for reliable extraction
- **Multiple extraction strategies**: 
  - Custom `uptime_pattern` (HTML attribute or regex)
  - Finds table cells with `data-i18n` attributes containing "uptime"
  - Searches for `<th>` elements with "uptime" text and extracts adjacent `<td>` values
  - Pattern matching for common uptime formats
- **Supports common formats**: "24 days 17h:22m:34s", "17h:22m:34s", "2h 30m 15s", etc.
- **Works with most routers**: Any status page showing system uptime can be used

**Example for Technicolor routers:**
The `st_gateway.html` page shows "System Uptime" in a table:
```html
<th data-i18n="atg_system_uptime"></th>
<td>29 days 15h:41m:18s</td>
```

To reliably parse this, set:
```json
"uptime_pattern": "atg_system_uptime"
```

This targets the specific `data-i18n` attribute for more reliable parsing than generic auto-detection.

### Sites Configuration

```json
{
  "sites": [
    {
      "name": "Google DNS",
      "address": "8.8.8.8",
      "priority": 100,
      "verification_only": false
    },
    {
      "name": "Cloudflare DNS",
      "address": "1.1.1.1",
      "priority": 90,
      "verification_only": true
    }
  ]
}
```

- `name` - Friendly name for the site
- `address` - IP address or hostname to ping
- `priority` - Higher priority sites are checked first for verification
- `verification_only` - If true, only used for verification, not primary monitoring

**Recommended sites strategy**:

✅ **Use IP Addresses** (Not hostnames like `google.com`)  
✅ **Geographic Diversity**: Local ISP → Regional → Global  
✅ **Multiple Providers**: Don't rely on single network

**Best practice order:**
1. **Local ISP DNS** (fastest, detects ISP issues first)
2. **Regional DNS** (geographic redundancy)
3. **Global DNS** (worldwide fallback)

**Example for Serbia** 🇷🇸:
- Local: `213.149.96.100` (SBB), `212.15.172.2` (Orion)
- Regional: `9.9.9.9` (Quad9 Europe)
- Global: `1.1.1.1` (Cloudflare), `8.8.8.8` (Google)

See `config.serbia.json` for production-ready Serbia configuration.

---

## Site Monitoring Recommendations

### General Principles

#### 1. Use IP Addresses, Not Hostnames

**Why?**
- **No DNS dependency** - If DNS fails, hostname pings fail even with working internet
- **Faster** - No DNS lookup delay
- **More reliable** - Eliminates DNS as a point of failure

❌ **Bad**: `google.com`, `cloudflare.com`, `facebook.com`  
✅ **Good**: `8.8.8.8`, `1.1.1.1`, `9.9.9.9`

#### 2. Geographic Diversity Strategy

```
Priority Order:
1. Local ISP DNS (your country/region)
2. Regional DNS (your continent)
3. Global DNS (worldwide availability)
```

This approach:
- Detects local ISP issues first
- Provides geographic redundancy
- Helps diagnose where the problem is (local vs global)

#### 3. Multiple ISPs

Use DNS servers from **different providers** to avoid single point of failure.

### Country-Specific Recommendations

#### Serbia 🇷🇸

**Local Serbian ISPs:**
- SBB: `213.149.96.100`, `213.149.96.101`
- Orion Telekom: `212.15.172.2`
- Telekom Srbija: `212.200.230.154`
- MTS: `212.200.232.211`

**European/Regional:**
- Quad9: `9.9.9.9` (Anycast, has European nodes)
- OpenDNS: `208.67.222.222` (European presence)

**Global (Fallback):**
- Cloudflare: `1.1.1.1`, `1.0.0.1` (Belgrade node)
- Google: `8.8.8.8`, `8.8.4.4` (Regional presence)

**Optimal Configuration:** See `config.serbia.json`

#### United States 🇺🇸

**Local ISPs (examples):**
- Comcast: `75.75.75.75`, `75.75.76.76`
- AT&T: `68.94.156.1`, `68.94.157.1`
- Verizon: `151.197.0.38`, `151.197.0.39`

**Regional:**
- Level3: `4.2.2.1`, `4.2.2.2`
- Quad9: `9.9.9.9`

**Global:**
- Cloudflare: `1.1.1.1`, `1.0.0.1`
- Google: `8.8.8.8`, `8.8.4.4`

#### Europe (General) 🇪🇺

**Regional DNS:**
- Quad9: `9.9.9.9`, `149.112.112.112`
- Freenom World: `80.80.80.80`, `80.80.81.81`

**Country-specific:**
- UK (BT): `195.99.66.220`
- Germany (Deutsche Telekom): `217.237.148.22`
- France (Free): `212.27.40.240`

**Global:**
- Cloudflare: `1.1.1.1`
- Google: `8.8.8.8`

#### Asia Pacific 🌏

**Regional:**
- Quad9: `9.9.9.9`
- Cloudflare: `1.1.1.1` (Has nodes in Singapore, Tokyo, Mumbai, etc.)

**Country-specific examples:**
- Japan (IIJ): `202.232.2.11`
- Australia (Telstra): `139.130.4.5`
- Singapore (StarHub): `202.156.2.2`

### How to Find Your Local ISP DNS

#### Method 1: Check Your Router
1. Log into your router
2. Look for "WAN" or "Internet" status page
3. Find "DNS Servers" - these are what your ISP provides

#### Method 2: Linux/macOS
```bash
cat /etc/resolv.conf
# or
nmcli device show | grep DNS
```

#### Method 3: Windows
```cmd
ipconfig /all
# Look for "DNS Servers" under your active adapter
```

#### Method 4: Online Tools
- Visit: https://www.dnsleaktest.com/
- Click "Standard test" or "Extended test"
- Shows DNS servers your ISP is using

### Testing Your Configuration

Before deploying, test your sites:

```bash
# Test each site manually
ping -c 4 213.149.96.100
ping -c 4 1.1.1.1
ping -c 4 8.8.8.8

# Test with RestartKO
./restartko -test-ping 213.149.96.100
./restartko -test-ping 1.1.1.1
```

**Good latency indicators:**
- Local ISP DNS: 1-20ms
- Regional DNS: 10-50ms  
- Global DNS: 20-100ms

If latency is higher, consider choosing closer servers.

### Priority Configuration

Use the `priority` field to control verification order:

```json
{
  "sites": [
    {
      "name": "SBB DNS Primary",
      "address": "213.149.96.100",
      "priority": 100,           // Highest priority - checked first
      "verification_only": false // Monitored continuously
    },
    {
      "name": "Cloudflare DNS",
      "address": "1.1.1.1",
      "priority": 75,            // Lower priority
      "verification_only": true  // Only used for verification
    }
  ]
}
```

**Best practice:**
- **100-90**: Local ISP DNS (continuous monitoring)
- **90-80**: Regional DNS (continuous monitoring)
- **80-70**: Global DNS (verification only)
- **70-60**: Additional global sites (verification only)

### Common Mistakes to Avoid

❌ **Using only global sites** (8.8.8.8, 1.1.1.1)
   - Can't detect local ISP issues quickly
   - May miss regional outages

❌ **Using hostnames instead of IPs**
   - Adds DNS dependency
   - Slower and less reliable

❌ **Using only one ISP's servers**
   - Single point of failure
   - Can't detect ISP-specific issues

❌ **Not testing latency first**
   - May use distant servers
   - Slower detection

✅ **Correct approach**: Mix of local, regional, and global **IP addresses** from multiple providers

### Need Help?

If you need recommendations for your specific country/region, please open an issue on GitHub with:
- Your country/region
- Your ISP name
- Output of `cat /etc/resolv.conf` or `ipconfig /all`

---

## Supported Routers

RestartKO supports various router types:

- ✅ **Generic HTTP** - Most consumer routers with web interface
- ✅ **CSRF-Protected** - Technicolor, OpenWrt/LuCI, and others with CSRF tokens
- ✅ **SSH** - OpenWrt, MikroTik, Ubiquiti, and enterprise routers
- ✅ **Telnet** - Older routers (framework in place)

---

## Router Configuration Examples

### Technicolor Routers

Technicolor routers (common with ISPs like Telstra, Optus, etc.) use CSRF tokens for security.

#### Configuration

```json
{
  "router": {
    "type": "generic_http",
    "address": "192.168.0.1",
    "username": "admin",
    "password": "YOUR_PASSWORD",
    
    "login_url": "http://192.168.0.1/goform/logon",
    "restart_url": "http://192.168.0.1/goform/ad_restart_gateway",
    "login_method": "POST",
    "login_form_fields": {
      "username_login": "{username}",
      "password_login": "{password}",
      "language_selector": "en"
    },
    "restart_method": "POST",
    "restart_form_fields": {
      "tch_devicerestart": "0x00"
    },
    "verify_ssl": false,
    
    "csrf_enabled": true,
    "csrf_token_url": "http://192.168.0.1/ad_restart_gateway.html",
    "csrf_token_field": "csrftoken",
    "csrf_token_input_name": "csrftoken",
    
    "uptime_check_enabled": true,
    "uptime_check_url": "http://192.168.0.1/st_gateway.html",
    "uptime_pattern": "",
    
    "timeout_seconds": 30,
    "custom_headers": {
      "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
      "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
      "Accept-Language": "en-US,en;q=0.9",
      "Origin": "http://192.168.0.1",
      "Referer": "http://192.168.0.1/"
    }
  }
}
```

**See `config.technicolor.json` for a ready-to-use template.**

#### How It Works

1. **Login**: POST to `/goform/logon` with credentials
2. **Get CSRF Token**: GET `/ad_restart_gateway.html` and extract token
3. **Restart**: POST to `/goform/ad_restart_gateway` with token

### Generic HTTP Routers

#### Basic Configuration (No CSRF)

```json
{
  "router": {
    "type": "generic_http",
    "address": "192.168.1.1",
    "username": "admin",
    "password": "admin",
    
    "login_url": "http://192.168.1.1/login.cgi",
    "restart_url": "http://192.168.1.1/reboot.cgi",
    "login_method": "POST",
    "login_form_fields": {
      "username": "{username}",
      "password": "{password}"
    },
    "restart_method": "POST",
    "restart_form_fields": {
      "action": "reboot"
    },
    "verify_ssl": false,
    "csrf_enabled": false
  }
}
```

#### With Session Cookie

```json
{
  "router": {
    "type": "generic_http",
    "address": "192.168.1.1",
    "username": "admin",
    "password": "admin",
    
    "login_url": "http://192.168.1.1/login",
    "restart_url": "http://192.168.1.1/system/reboot",
    "login_method": "POST",
    "login_form_fields": {
      "user": "{username}",
      "pass": "{password}"
    },
    "restart_method": "GET",
    "restart_form_fields": {},
    "session_cookie_name": "SID",
    "verify_ssl": false,
    "csrf_enabled": false
  }
}
```

### OpenWrt/LEDE

#### SSH Configuration

```json
{
  "router": {
    "type": "ssh",
    "address": "192.168.1.1",
    "port": 22,
    "username": "root",
    "password": "YOUR_PASSWORD",
    "restart_command": "reboot",
    "timeout_seconds": 30
  }
}
```

#### LuCI Web Interface (with CSRF)

```json
{
  "router": {
    "type": "generic_http",
    "address": "192.168.1.1",
    "username": "root",
    "password": "YOUR_PASSWORD",
    
    "login_url": "http://192.168.1.1/cgi-bin/luci",
    "restart_url": "http://192.168.1.1/cgi-bin/luci/admin/system/reboot/call",
    "login_method": "POST",
    "login_form_fields": {
      "luci_username": "{username}",
      "luci_password": "{password}"
    },
    "restart_method": "POST",
    "restart_form_fields": {},
    "verify_ssl": false,
    
    "csrf_enabled": true,
    "csrf_token_url": "http://192.168.1.1/cgi-bin/luci/admin/system/reboot",
    "csrf_token_field": "token",
    "csrf_token_input_name": "token"
  }
}
```

### MikroTik

```json
{
  "router": {
    "type": "ssh",
    "address": "192.168.88.1",
    "port": 22,
    "username": "admin",
    "password": "YOUR_PASSWORD",
    "restart_command": "/system reboot",
    "timeout_seconds": 30
  }
}
```

### Ubiquiti

#### UniFi Security Gateway / EdgeRouter

```json
{
  "router": {
    "type": "ssh",
    "address": "192.168.1.1",
    "port": 22,
    "username": "ubnt",
    "password": "ubnt",
    "restart_command": "reboot",
    "timeout_seconds": 30,
    "ssh_host_key_file": ""
  }
}
```

**SSH Host Key Validation (Optional):**

For enhanced security, you can verify the router's SSH host key by providing its public key file:

1. Get the host public key:
   ```bash
   ssh-keyscan -t rsa 192.168.1.1 > router_hostkey.pub
   ```

2. Add the path to config:
   ```json
   "ssh_host_key_file": "/opt/restartko/router_hostkey.pub"
   ```

If omitted, host key validation is skipped (you'll see a warning in logs).

### TP-Link

#### Archer Series (Web Interface)

```json
{
  "router": {
    "type": "generic_http",
    "address": "192.168.0.1",
    "username": "admin",
    "password": "admin",
    
    "login_url": "http://192.168.0.1/",
    "restart_url": "http://192.168.0.1/userRpm/SysRebootRpm.htm",
    "login_method": "GET",
    "login_form_fields": {},
    "restart_method": "GET",
    "restart_form_fields": {},
    "verify_ssl": false,
    
    "csrf_enabled": false,
    "custom_headers": {
      "Authorization": "Basic YWRtaW46YWRtaW4=",
      "Referer": "http://192.168.0.1/"
    }
  }
}
```

**Note**: Authorization header is Base64 encoded `username:password`. Generate with:
```bash
echo -n "admin:admin" | base64
```

### ASUS

#### RT-AC Series

```json
{
  "router": {
    "type": "generic_http",
    "address": "192.168.1.1",
    "username": "admin",
    "password": "admin",
    
    "login_url": "http://192.168.1.1/login.cgi",
    "restart_url": "http://192.168.1.1/apply.cgi",
    "login_method": "POST",
    "login_form_fields": {
      "login_username": "{username}",
      "login_passwd": "{password}"
    },
    "restart_method": "POST",
    "restart_form_fields": {
      "action_mode": "apply",
      "rc_service": "reboot"
    },
    "session_cookie_name": "asus_token",
    "verify_ssl": false,
    "csrf_enabled": false
  }
}
```

### Finding Router Form Fields

Use browser developer tools:

1. Open router web interface
2. Open Developer Tools (F12)
3. Go to Network tab
4. Perform login
5. Click on the login request
6. View "Payload" or "Form Data"
7. Copy field names and values to config

---

## CSRF Token Support

RestartKO supports routers that use CSRF (Cross-Site Request Forgery) tokens for security. Many modern routers (like Technicolor, OpenWrt/LuCI, etc.) use CSRF tokens to protect administrative actions.

### How RestartKO Handles CSRF

When `csrf_enabled` is set to `true`, RestartKO:

1. **Login** to the router and establish a session
2. **Fetch the restart page** to obtain the CSRF token
3. **Extract the CSRF token** from the HTML response
4. **Include the token** in the restart request

### CSRF Configuration

```json
{
  "router": {
    "csrf_enabled": true,
    "csrf_token_url": "http://192.168.0.1/restart_page.html",
    "csrf_token_field": "csrftoken",
    "csrf_token_input_name": "csrftoken",
    "csrf_token_pattern": ""
  }
}
```

#### Configuration Options

| Field | Description | Default | Example |
|-------|-------------|---------|---------|
| `csrf_enabled` | Enable CSRF token handling | `false` | `true` |
| `csrf_token_url` | URL to fetch token from | (uses `restart_url`) | `http://192.168.0.1/restart.html` |
| `csrf_token_field` | Form field name for the token | `"csrftoken"` | `"csrftoken"`, `"token"` |
| `csrf_token_input_name` | HTML input name to extract token from | `"csrftoken"` | `"csrftoken"`, `"token"` |
| `csrf_token_pattern` | Custom regex pattern (advanced) | `""` | `name="token" value="([^"]+)"` |

### Token Extraction Methods

RestartKO tries multiple methods to extract CSRF tokens:

#### 1. Custom Regex Pattern (if provided)

```json
{
  "csrf_token_pattern": "var csrfToken = '([^']+)'"
}
```

Useful when the token is in JavaScript or a non-standard format.

#### 2. HTML Input Name

```json
{
  "csrf_token_input_name": "csrftoken"
}
```

Looks for: `<input name="csrftoken" value="TOKEN_HERE">`

Works with both attribute orders:
- `name="csrftoken" value="abc123"`
- `value="abc123" name="csrftoken"`

#### 3. Common Patterns (Fallback)

If no pattern is provided, RestartKO tries these common patterns:
- `name="csrftoken"`
- `name="csrf_token"`
- `name="_csrf"`

### Finding CSRF Token Information

#### Using Browser Developer Tools

1. Open your router's web interface
2. Press F12 to open Developer Tools
3. Navigate to the restart/reboot page
4. In the Elements/Inspector tab, search for "csrf" or "token"
5. Look for hidden input fields:

```html
<input type="hidden" name="csrftoken" value="abc123xyz...">
```

#### Using curl

```bash
# Get the page and search for CSRF token
curl -s "http://192.168.0.1/restart.html" | grep -i csrf
```

#### Network Tab Method

1. Open Developer Tools → Network tab
2. Perform a restart manually
3. Click on the restart request
4. View the "Payload" or "Form Data"
5. Look for token field

### CSRF Troubleshooting

#### "CSRF token not found in response"

**Solution 1**: Check the token URL
```json
{
  "csrf_token_url": "http://192.168.0.1/correct_page.html"
}
```

**Solution 2**: Verify the input name
```json
{
  "csrf_token_input_name": "csrftoken"
}
```

**Solution 3**: Use a custom pattern
```json
{
  "csrf_token_pattern": "name=\"token\" value=\"([^\"]+)\""
}
```

#### Token Extraction Works but Restart Fails

**Solution 1**: Check the field name for submission
```json
{
  "csrf_token_field": "token"
}
```

**Solution 2**: Enable debug logging
```json
{
  "log_level": "debug"
}
```

### Testing CSRF Configuration

```bash
# Run test mode to verify connection
./restartko -test-router -config config.json
```

Expected output:
```
🔧 Testing router connection: 192.168.0.1 (generic_http)
Testing HTTP login...
✅ Login test PASSED!

Note: CSRF token would be fetched during actual restart
Router configuration appears to be correct.
```

---

## Cluster Coordination

Cluster mode allows multiple RestartKO instances to run simultaneously while ensuring only one instance performs a router restart at a time.

### Benefits

- **Redundancy**: If one node fails, others continue monitoring
- **Network Diversity**: Monitor from different network locations
- **High Availability**: No single point of failure

### How It Works

```
┌─────────┐        ┌─────────┐        ┌─────────┐
│ Node 1  │◄──────►│ Node 2  │◄──────►│ Node 3  │
└────┬────┘        └────┬────┘        └────┬────┘
     │                  │                  │
     │  All detect connectivity failure
     ▼                  ▼                  ▼
     │                  │                  │
     └──────────────────┴──────────────────┘
                        │
                        ▼
              Node 1 tries to acquire lock
                        │
                        ▼
         Requests lock from Node 2 and Node 3
                        │
                   Both grant?
                        │
                       Yes
                        ▼
              Node 1 acquires lock
                        │
                        ▼
            Router restart by Node 1
                        │
                        ▼
          Other nodes see lock, wait
                        │
                        ▼
               Lock auto-released
```

### Configuration Example

```json
{
  "cluster_enabled": true,
  "node_id": "monitoring-node-1",
  "cluster_nodes": [
    "http://192.168.1.10:8080",
    "http://192.168.1.11:8080",
    "http://192.168.1.12:8080"
  ],
  "cluster_api_port": 8080,
  "cluster_api_listen": "0.0.0.0:8080",
  "cluster_lock_timeout_seconds": 300,
  "cluster_health_check_seconds": 10,
  "cluster_stagger_seconds": 20,
  "cluster_all_node_ids": ["node-1", "node-2", "node-3"]
}
```

**Important:** The `cluster_all_node_ids` array must contain ALL node IDs in your cluster and must be **identical on every node**. This is used to calculate each node's stagger offset.

**Cluster Stagger:** With `cluster_stagger_seconds` set to 20 and 3 nodes, checks will be staggered:
- Node 1 (`node-1`): Position 0 → 0s offset → Starts immediately
- Node 2 (`node-2`): Position 1 → 6s offset → Starts after 6 seconds (20s / 3)
- Node 3 (`node-3`): Position 2 → 13s offset → Starts after 13 seconds (20s / 3 × 2)

This prevents all nodes from pinging simultaneously and provides better load distribution.

### Lock Mechanism

1. **Lock Acquisition**:
   - Node detects connectivity failure
   - Node checks cooldown and retry limits
   - Node requests lock from ALL healthy nodes
   - If all nodes grant lock → proceed
   - If any node denies → abort

2. **Lock Timeout**:
   - Locks expire after `cluster_lock_timeout_seconds`
   - Prevents deadlock if node crashes

3. **Lock Release**:
   - Explicit release after restart completes
   - Automatic release on timeout
   - Release on service shutdown

### API Endpoints

When cluster mode is enabled:

- `GET /health` - Health check
- `GET /status` - Current node status
- `POST /lock/acquire` - Acquire restart lock
- `POST /lock/release` - Release restart lock
- `GET /lock/status` - Check lock status

---

## Power Loss Detection (Advanced)

### The Problem

When power is lost and restored:
1. Router reboots and takes time to come online
2. RestartKO service starts
3. Internet is still down (router still booting)
4. Service might try to restart router again
5. Creates unnecessary restart cycles

### Solution

RestartKO detects power losses by analyzing the time gap between last shutdown and current startup.

```
Last Shutdown: 2025-11-07 10:00:00
Current Start: 2025-11-07 10:02:30
Gap: 2.5 minutes

If gap < grace_period (5 min) → Normal restart
If gap > grace_period → Power loss suspected
```

### Configuration

```json
{
  "power_loss_grace_period_minutes": 5,
  "power_loss_restart_delay_minutes": 2
}
```

### Behavior

Power loss detection uses multiple signals to avoid false positives:

1. **Quick restart check**: Service downtime < 5 min → **Skip detection** (assumed clean restart)
2. **Router uptime check** (if enabled):
   - Router uptime > service downtime → **No power loss** (just service restart)
   - Router uptime < service downtime → **Power loss detected** (router rebooted)
3. **Fallback** (if uptime check disabled/failed): **Skip detection** to avoid false positives

When **actual power loss** is detected:

1. **Log Warning**: "⚡ Power loss detected!"
2. **Wait**: Delay for configured minutes (default: 2 min)
3. **Resume**: Continue normal monitoring
4. **State**: Mark in state file

This gives the router time to fully boot before monitoring begins.

**Example scenarios:**
- ✅ Service downtime: 2 min → **No check** (quick restart)
- ✅ Service downtime: 16 min, Router uptime: 29 days → **No power loss** (manual restart)
- ❌ Service downtime: 30 min, Router uptime: 2 min → **Power loss detected** (router rebooted)
- ✅ Service downtime: 19 min, uptime_check disabled → **No check** (avoid false positive)

**Important:** Enable `uptime_check_enabled` for accurate power loss detection. Without it, power loss detection is disabled to prevent false positives on manual restarts.

---

## State Management

RestartKO maintains persistent state across restarts for:
- Restart history and statistics
- Site uptime and latency metrics
- Power loss detection
- Cooldown enforcement

### State File Structure

```json
{
  "service_start_time": "2025-11-07T12:00:00Z",
  "last_shutdown_time": "2025-11-07T11:50:00Z",
  "total_restarts": 5,
  "consecutive_restart_fails": 0,
  "restart_history": [...],
  "site_states": {
    "8.8.8.8": {
      "name": "Google DNS",
      "total_checks": 1200,
      "successful_checks": 1195,
      "last_latency_ms": 8.5,
      "average_latency_ms": 9.2
    }
  }
}
```

### Features

- **Automatic Saves**: Every 30 seconds (configurable)
- **Atomic Writes**: Write to .tmp, then rename
- **Corruption Handling**: Backup corrupted files
- **Load on Startup**: Restore previous state

---

## Site Monitoring Strategies

### Primary vs Verification Sites

- **Primary Sites**: Continuously monitored (`verification_only: false`)
- **Verification Sites**: Only checked when primary site is down (`verification_only: true`)

### Priority System

Sites with higher priority are checked first during verification:

```json
{
  "sites": [
    {
      "name": "Google DNS",
      "address": "8.8.8.8",
      "priority": 100,
      "verification_only": false
    },
    {
      "name": "Cloudflare DNS",
      "address": "1.1.1.1",
      "priority": 90,
      "verification_only": true
    }
  ]
}
```

### Recommended Site Mix

1. **DNS Servers** (high priority):
   - `8.8.8.8` (Google)
   - `1.1.1.1` (Cloudflare)
   - `208.67.222.222` (OpenDNS)

2. **Popular Websites** (medium priority):
   - `google.com`
   - `cloudflare.com`
   - `amazon.com`

3. **Local Infrastructure** (if applicable):
   - Your ISP's DNS
   - Local gateway

### Verification Logic

```
Primary Site Down
       │
       ▼
Check verification_site_count sites
       │
       ▼
   All down? ─── No ──► Specific site issue, continue monitoring
       │
      Yes
       │
       ▼
Internet connectivity issue
       │
       ▼
Attempt router restart
```

---

## Restart Logic

### Decision Flow

```
Site Down Detected
       │
       ▼
Verify with other sites
       │
       ▼
Multiple sites down? ─── No ──► Continue monitoring
       │
      Yes
       │
       ▼
Wait grace period (60s)
       │
       ▼
Re-verify connectivity
       │
       ▼
Still down? ─── No ──► Router recovered on its own!
       │
      Yes
       │
       ▼
Check restart cooldown
       │
       ▼
Cooldown passed? ─── No ──► Wait
       │
      Yes
       │
       ▼
Check max retries
       │
       ▼
Within limit? ─── No ──► Manual intervention required
       │
      Yes
       │
       ▼
Try acquire cluster lock (if enabled)
       │
       ▼
Lock acquired? ─── No ──► Another node handling it
       │
      Yes
       │
       ▼
Restart Router
       │
       ▼
Wait (restart_wait_seconds)
       │
       ▼
Verify connectivity
       │
       ▼
Success? ─── No ──► Schedule retry
       │
      Yes
       │
       ▼
Release lock, reset counters
```

### Cooldown and Retry Logic

**Grace Period**: Allow router self-recovery before restart
```
Connectivity failure detected
Wait: 60 seconds (configurable)
Re-verify connectivity
If restored → No restart needed!
If still down → Proceed with restart logic
```

**Cooldown**: Minimum time between restart attempts
```
Attempt 1: Time 0
Cooldown: 15 minutes
Attempt 2: Earliest at Time 15:00
```

**Max Retries**: Prevent infinite restart loops
```
Max Retries: 3
Attempt 1: Fail
Attempt 2: Fail
Attempt 3: Fail
Attempt 4: BLOCKED
```

**Retry Delay**: Extra time between retries
```
Attempt 1: Fail
Wait: 10 minutes
Re-verify connectivity
Still down? → Attempt 2
```

### Verification After Restart

1. **Wait**: `restart_wait_seconds` for router to boot
2. **Ping**: `post_restart_ping_count` pings to sites
3. **Threshold**: At least 50% of sites must respond
4. **Result**:
   - Success → Reset fail counter
   - Failure → Increment counter, schedule retry

---

## Systemd Service

The systemd service is automatically created by `install.sh` when you choose to install as a service.

The service includes:
- ✅ **Dedicated service user** - Runs as `restartko` user (not root) for better security
- ✅ **Network readiness wait** - Waits for `network-online.target` before starting
- ✅ **Smart network wait** - Actively checks for network readiness (exits immediately when ready, max 60s)
- ✅ **Auto-restart** - Automatically restarts if service crashes
- ✅ **Journal logging** - Logs to systemd journal (compatible with log2ram)
- ✅ **Security hardening** - NoNewPrivileges, PrivateTmp, ProtectSystem, ProtectHome
- ✅ **Resource limits** - MemoryMax 256M, LimitNOFILE 65536

**Service configuration:**
```ini
[Unit]
Description=RestartKO Network Monitor
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=restartko
Group=restartko
WorkingDirectory=/opt/restartko

# Wait for network to be ready before starting
ExecStartPre=/opt/restartko/wait-for-network.sh
ExecStart=/opt/restartko/restartko -config /opt/restartko/config.json

# Auto-restart if service crashes
Restart=always
RestartSec=10

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/restartko

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=restartko

# Resource limits
LimitNOFILE=65536
MemoryMax=256M

[Install]
WantedBy=multi-user.target
```

**Managing the service:**

```bash
# Start service
sudo systemctl start restartko

# Stop service
sudo systemctl stop restartko

# Restart service
sudo systemctl restart restartko

# Check status
sudo systemctl status restartko

# View logs
sudo journalctl -u restartko -f

# View logs since boot
sudo journalctl -u restartko -b
```

**How does the network wait work?**

After a power loss or system boot, the network may not be immediately ready. The `wait-for-network.sh` script:
1. **Checks for default route** - Waits until `ip route` shows a default gateway
2. **Exits immediately** - Once network is ready (typically 2-5 seconds)
3. **Times out at 60 seconds** - Proceeds anyway if network takes too long
4. **Service auto-restarts** - If network isn't ready, systemd will retry

This is much faster than a fixed 30-second delay while still handling power loss scenarios gracefully.

**Service User Security:**

The service runs as a dedicated `restartko` system user (not root) for improved security:
- Automatically created during installation
- No shell access (`/bin/false`)
- Limited permissions (only access to `/opt/restartko` and `/var/log/restartko`)
- CAP_NET_RAW capability granted for raw socket ICMP (if enabled)

---

## Troubleshooting

### Ping Not Working

**If using raw sockets** (`use_raw_sockets: true`):
- On Linux, run as root or set capabilities:
  ```bash
  sudo setcap cap_net_raw+ep ./restartko
  ```
- On macOS, run with sudo:
  ```bash
  sudo ./restartko -config config.json
  ```

**If using unprivileged mode** (`use_raw_sockets: false`):
- Should work without special permissions
- Uses UDP for ICMP echo requests

### Router Restart Fails

1. **Test connection**:
   ```bash
   ./restartko -test-router -config config.json
   ```

2. **Check credentials**: Verify username and password

3. **Verify URLs**: Check login and restart URLs

4. **Check form fields**: Use browser dev tools to find field names

5. **Enable debug logging**:
   ```json
   {
     "log_level": "debug"
   }
   ```

6. **For CSRF routers**: Verify token extraction settings

### State File Issues

If state file becomes corrupted:

```bash
# Backup corrupted state
mv state.json state.json.backup

# Service will create fresh state on next start
```

### DNS Resolution Fails

- Check `dns_cache_ttl_minutes` setting
- Verify hostnames are correct
- Check if DNS servers are reachable
- Enable debug logging to see DNS cache activity

### Cluster Issues

- Verify all nodes can reach each other
- Check `cluster_api_listen` and firewall rules
- Ensure `node_id` is unique for each node
- Check cluster node URLs are correct

---

## Architecture

```
┌─────────────────┐
│   Main Service  │
└────────┬────────┘
         │
    ┌────┴─────────────────────┐
    │                          │
┌───▼────┐              ┌──────▼──────┐
│ Monitor│              │   Cluster   │
│        │              │   Manager   │
└───┬────┘              └──────┬──────┘
    │                          │
    │ ┌─────────┐      ┌───────▼──────┐
    ├─▶  Ping   │      │ Distributed  │
    │ │ Service │      │   Locking    │
    │ └─────────┘      └──────────────┘
    │
    │ ┌─────────┐
    ├─▶ Router  │
    │ │ Restart │
    │ └─────────┘
    │
    │ ┌─────────┐
    ├─▶  State  │
    │ │  Mgmt   │
    │ └─────────┘
    │
    │ ┌─────────┐
    └─▶   DNS   │
      │  Cache  │
      └─────────┘
```

---

## Best Practices

1. **Multiple Sites** - Configure at least 3-5 sites for reliable detection
2. **Site Diversity** - Use a mix of IPs and hostnames from different providers
3. **Cooldown Period** - Set 10-20 minutes to prevent restart loops
4. **Cluster Mode** - Use for critical installations with multiple monitoring points
5. **Test First** - Always test ping and router connection before deployment
6. **Router Templates** - Use `config.technicolor.json` for Technicolor routers
7. **State Backups** - Periodically backup the state file
8. **Log Monitoring** - Watch logs for patterns and issues
9. **DNS Caching** - Keep TTL at 5 minutes for good balance
10. **Security** - Set restrictive permissions on config file: `chmod 600 config.json`

---

## Changelog

### [1.2.0] - 2025-11-07

#### Added
- **Router Uptime Check** - Power outage detection using router's system uptime
  - Fetches and parses uptime from router status pages (e.g., `st_gateway.html`)
  - Applies grace period when power outage detected
  - Re-verifies connectivity after delay before restarting
  - Configurable uptime check URL and custom patterns
- **HTML Parsing Library** - Proper HTML parsing with goquery
  - Reliable extraction of uptime values from HTML tables
  - Multiple extraction strategies (data-i18n, th/td pairs, pattern matching)
  - Replaces fragile regex-based parsing
- **Raw Socket Ping** - ICMP ping with raw socket support
  - Better performance and reliability
  - Configurable (works without privileges in unprivileged mode)
  - Uses `github.com/go-ping/ping` library

#### Updated
- **Go 1.25.1** - Updated from Go 1.21
- **All dependencies to latest versions**:
  - `golang.org/x/crypto v0.43.0` (was v0.17.0)
  - `golang.org/x/net v0.46.0` (was v0.10.0)
  - `golang.org/x/sync v0.10.0`
  - `golang.org/x/sys v0.37.0` (was v0.15.0)
  - `github.com/go-ping/ping v1.2.0`
  - Added `github.com/PuerkitoBio/goquery v1.10.3`

#### Improved
- Power loss detection now checks router uptime for accuracy
- HTML parsing is more robust and maintainable
- Better error handling for uptime extraction

### [1.1.0] - 2025-11-07

#### Added
- **CSRF Token Support** - Full support for routers using CSRF protection
  - Automatic token extraction from HTML
  - Multiple extraction methods (custom regex, input name, common patterns)
  - Support for Technicolor and OpenWrt/LuCI routers
- **DNS Caching** - Reduce DNS load with TTL-based caching
  - Fallback to cached IP on DNS failure
  - DDNS IP change detection
  - Pre-resolution on startup
- **Technicolor Configuration** - Pre-configured template
- **Comprehensive Documentation** - Router examples and CSRF guide

#### Changed
- Enhanced HTTP router restart logic with CSRF handling
- Improved router configuration documentation

### [1.0.0] - 2025-11-07

#### Added
- Initial release of RestartKO
- Multi-site connectivity monitoring with ICMP ping
- Automatic router restart on connectivity failure
- Support for HTTP, SSH, and Telnet-based routers
- Cluster coordination for multi-node deployments
- Distributed locking mechanism
- Power loss detection and handling
- Persistent state management
- CLI tools for testing
- Statistics tracking per site
- Event history and restart audit trail
- Installation and uninstallation scripts

---

## License

MIT License - See LICENSE file for details

Copyright (c) 2025 RestartKO Contributors

**GitHub Repository**: https://github.com/cujanovic/restartko

---

## Contributing

Contributions are welcome! If you've successfully configured a router not listed here, please share:

- Router brand and model
- Configuration JSON
- Any special notes or requirements

Open an issue or pull request on GitHub.

---

## Support

For issues, questions, or feature requests, please open an issue on GitHub.

---

**RestartKO** - Keep your internet connection alive with intelligent router restart automation.
