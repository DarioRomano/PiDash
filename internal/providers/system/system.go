// Package system reports basic health of the Pi: load, memory, temperature, uptime, addresses.
package system

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Provider reports system statistics read from /proc and /sys.
type Provider struct {
	interval time.Duration
	// CoolAbove is the temperature in °C from which the Pi counts as hot;
	// 0 never calls it hot.
	CoolAbove float64

	mu  sync.Mutex
	hot bool
}

// coolBy is how far below CoolAbove the temperature has to fall before the
// Pi no longer counts as hot. Without the gap the effects would switch on
// and off every few seconds around the limit.
const coolBy = 7

// New returns a system provider polled every interval.
func New(interval time.Duration) *Provider { return &Provider{interval: interval} }

// judge takes a temperature reading and says whether the Pi is hot.
func (p *Provider) judge(temp float64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.CoolAbove <= 0:
		p.hot = false
	case temp >= p.CoolAbove:
		p.hot = true
	case temp <= p.CoolAbove-coolBy:
		p.hot = false
	}
	return p.hot
}

// Name implements providers.Provider.
func (p *Provider) Name() string { return "system" }

// Interval implements providers.Provider.
func (p *Provider) Interval() time.Duration { return p.interval }

// State is the JSON shape sent to the frontend.
type State struct {
	Hostname      string   `json:"hostname"`
	UptimeSeconds float64  `json:"uptimeSeconds"`
	Load1         float64  `json:"load1"`
	MemTotalMB    int64    `json:"memTotalMB"`
	MemUsedMB     int64    `json:"memUsedMB"`
	CPUTempC      *float64 `json:"cpuTempC"`
	Addresses     []string `json:"addresses"`
	// Hot asks the dashboard to do less until the Pi has cooled down.
	Hot bool `json:"hot"`
}

// Fetch implements providers.Provider.
func (p *Provider) Fetch(_ context.Context) (any, error) {
	s := State{Addresses: []string{}}
	s.Hostname, _ = os.Hostname()

	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			s.UptimeSeconds, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			s.Load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if f, err := os.Open("/proc/meminfo"); err == nil {
		var total, avail int64
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 2 {
				continue
			}
			v, _ := strconv.ParseInt(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				total = v
			case "MemAvailable:":
				avail = v
			}
		}
		f.Close()
		s.MemTotalMB = total / 1024
		s.MemUsedMB = (total - avail) / 1024
	}
	if t, ok := cpuTemp(thermalDir); ok {
		s.CPUTempC = &t
		s.Hot = p.judge(t)
	}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, it := range ifaces {
			if it.Flags&net.FlagLoopback != 0 || it.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, _ := it.Addrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
					s.Addresses = append(s.Addresses, ipn.IP.String())
				}
			}
		}
	}
	return s, nil
}

// thermalDir is where the kernel lists its temperature sensors.
var thermalDir = "/sys/class/thermal"

// cpuTemp reads the processor's temperature in degrees Celsius. A Raspberry
// Pi has one sensor; other boards and PCs have several, of which the one
// named after the processor is wanted, not the battery's or the Wi-Fi's.
func cpuTemp(dir string) (float64, bool) {
	zones, _ := filepath.Glob(filepath.Join(dir, "thermal_zone*"))
	sort.Strings(zones)
	best, bestRank := "", 99
	for _, z := range zones {
		kind, _ := os.ReadFile(filepath.Join(z, "type"))
		name := strings.ToLower(strings.TrimSpace(string(kind)))
		rank := 9
		switch {
		case strings.Contains(name, "x86_pkg") || strings.Contains(name, "cpu") || strings.Contains(name, "soc"):
			rank = 0
		case strings.Contains(name, "core") || strings.Contains(name, "pkg") || strings.Contains(name, "k10temp"):
			rank = 1
		case strings.Contains(name, "acpitz"):
			rank = 2
		case strings.Contains(name, "wifi") || strings.Contains(name, "iwl") || strings.Contains(name, "bat") || strings.Contains(name, "nvme") || strings.Contains(name, "gpu"):
			rank = 20
		}
		if rank < bestRank {
			best, bestRank = z, rank
		}
	}
	if best == "" || bestRank >= 20 {
		return 0, false
	}
	b, err := os.ReadFile(filepath.Join(best, "temp"))
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	// A sensor that is not connected reports nonsense.
	if err != nil || v <= 0 || v > 150000 {
		return 0, false
	}
	return v / 1000, true
}
