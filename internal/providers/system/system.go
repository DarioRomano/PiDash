// Package system reports basic health of the Pi: load, memory, temperature, uptime, addresses.
package system

import (
	"bufio"
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Provider reports system statistics read from /proc and /sys.
type Provider struct{ interval time.Duration }

// New returns a system provider polled every interval.
func New(interval time.Duration) *Provider { return &Provider{interval: interval} }

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
	if b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil {
			t := v / 1000
			s.CPUTempC = &t
		}
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
