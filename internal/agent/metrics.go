package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var metricSamples struct {
	sync.Mutex
	total uint64
	idle  uint64
}

func systemInfoMessage(ctx context.Context) map[string]any {
	hostname, _ := os.Hostname()
	osName, osVersion := linuxRelease()
	kernelBytes, _ := exec.Command("uname", "-r").Output()
	ipv4, ipv6 := interfaceAddresses()
	return map[string]any{
		"type": "system_info", "hostname": hostname, "os_name": osName, "os_version": osVersion,
		"kernel": strings.TrimSpace(string(kernelBytes)), "arch": runtime.GOARCH, "ipv4": ipv4, "ipv6": ipv6,
		"public_ipv4": publicIPv4(ctx),
	}
}

func metricsMessage() map[string]any {
	cpu := cpuPercent()
	memoryUsed, memoryTotal := memoryUsage()
	diskUsed, diskTotal := diskUsage()
	uptime := uptimeSeconds()
	rx, tx := networkUsage()
	return map[string]any{
		"type": "metrics", "cpu_percent": cpu, "memory_used_bytes": memoryUsed, "memory_total_bytes": memoryTotal,
		"disk_used_bytes": diskUsed, "disk_total_bytes": diskTotal, "uptime_seconds": uptime,
		"nic_rx_bytes": rx, "nic_tx_bytes": tx,
	}
}

func linuxRelease() (string, string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS, ""
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = strings.Trim(value, `"`)
		}
	}
	name := values["ID"]
	if name == "" {
		name = runtime.GOOS
	}
	return name, values["VERSION_ID"]
}

func interfaceAddresses() ([]string, []string) {
	addresses, _ := net.InterfaceAddrs()
	var ipv4, ipv6 []string
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip.IsLoopback() {
			continue
		}
		if ip.To4() != nil {
			ipv4 = append(ipv4, ip.String())
		} else {
			ipv6 = append(ipv6, ip.String())
		}
	}
	return ipv4, ipv6
}

func publicIPv4(ctx context.Context) string {
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(requestContext, http.MethodGet, "https://api.ipify.org", nil)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64))
	if err != nil {
		return ""
	}
	ip := net.ParseIP(strings.TrimSpace(string(data)))
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return ""
	}
	return ip.To4().String()
}

func cpuPercent() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	fields := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}
	var total uint64
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0
		}
		total += value
		values = append(values, value)
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	metricSamples.Lock()
	defer metricSamples.Unlock()
	previousTotal, previousIdle := metricSamples.total, metricSamples.idle
	metricSamples.total, metricSamples.idle = total, idle
	if previousTotal == 0 || total <= previousTotal || idle < previousIdle {
		return 0
	}
	deltaTotal, deltaIdle := total-previousTotal, idle-previousIdle
	if deltaIdle > deltaTotal {
		return 0
	}
	return float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100
}

func memoryUsage() (int64, int64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	values := map[string]int64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			value, _ := strconv.ParseInt(fields[1], 10, 64)
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024
		}
	}
	total, available := values["MemTotal"], values["MemAvailable"]
	if available > total {
		available = total
	}
	return total - available, total
}

func diskUsage() (int64, int64) {
	var value syscall.Statfs_t
	if syscall.Statfs("/", &value) != nil {
		return 0, 0
	}
	total := int64(value.Blocks) * int64(value.Bsize)
	available := int64(value.Bavail) * int64(value.Bsize)
	return total - available, total
}

func uptimeSeconds() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	value, _ := strconv.ParseFloat(strings.Fields(string(data))[0], 64)
	return int64(value)
}

func networkUsage() (int64, int64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	var rx, tx int64
	for _, line := range strings.Split(string(data), "\n") {
		name, counters, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(name) == "lo" {
			continue
		}
		fields := strings.Fields(counters)
		if len(fields) < 16 {
			continue
		}
		received, errRX := strconv.ParseInt(fields[0], 10, 64)
		transmitted, errTX := strconv.ParseInt(fields[8], 10, 64)
		if errRX == nil && errTX == nil {
			rx += received
			tx += transmitted
		}
	}
	return rx, tx
}

func metricSummary() string {
	used, total := memoryUsage()
	return fmt.Sprintf("memory=%d/%d", used, total)
}
