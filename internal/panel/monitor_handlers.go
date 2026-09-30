package panel

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type SystemMetrics struct {
	CPUPercent    string
	MemoryPercent string
	MemoryUsed    string
	MemoryTotal   string
	DiskPercent   string
	DiskUsed      string
	DiskTotal     string
	LoadAverage   string
	Uptime        string
}

type ServiceState struct {
	Name   string
	Status string
}

func (a *App) handleMonitor(w http.ResponseWriter, r *http.Request) {
	metrics := collectSystemMetrics(r.Context())
	services := []ServiceState{
		{Name: "mini-panel", Status: systemctlStatus(r.Context(), "mini-panel")},
		{Name: "nginx", Status: systemctlStatus(r.Context(), "nginx")},
		{Name: "mariadb", Status: systemctlStatus(r.Context(), "mariadb")},
		{Name: "cron", Status: systemctlStatus(r.Context(), "cron")},
	}
	domains, _ := a.store.ListDomains(r.Context())
	var domainServices []ServiceState
	for _, domain := range domains {
		if !domainHasService(&domain) {
			continue
		}
		status := "unknown"
		if out, err := a.domainCtl.Output(r.Context(), "service", "status", domain.Domain); err == nil && out != "" {
			status = out
		}
		domainServices = append(domainServices, ServiceState{Name: domain.Domain, Status: status})
	}
	a.render(w, http.StatusOK, "monitor", pageData{
		Session:        sessionFromContext(r.Context()),
		Metrics:        metrics,
		Services:       services,
		DomainServices: domainServices,
	})
}

func collectSystemMetrics(ctx context.Context) SystemMetrics {
	return SystemMetrics{
		CPUPercent:    readCPUPercent(),
		MemoryPercent: readMemoryPercent(),
		MemoryUsed:    readMemoryValue("used"),
		MemoryTotal:   readMemoryValue("total"),
		DiskPercent:   readDiskValue(ctx, "percent"),
		DiskUsed:      readDiskValue(ctx, "used"),
		DiskTotal:     readDiskValue(ctx, "total"),
		LoadAverage:   readLoadAverage(),
		Uptime:        readUptime(),
	}
}

func readCPUPercent() string {
	if runtime.GOOS != "linux" {
		return "n/a"
	}
	first, err := readCPUStat()
	if err != nil {
		return "n/a"
	}
	time.Sleep(120 * time.Millisecond)
	second, err := readCPUStat()
	if err != nil {
		return "n/a"
	}
	totalDelta := second.total - first.total
	idleDelta := second.idle - first.idle
	if totalDelta <= 0 {
		return "n/a"
	}
	used := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	return fmt.Sprintf("%.1f%%", used)
}

type cpuStat struct {
	total uint64
	idle  uint64
}

func readCPUStat() (cpuStat, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuStat{}, err
	}
	line := strings.SplitN(string(data), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuStat{}, fmt.Errorf("invalid cpu stat")
	}
	var values []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuStat{}, err
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuStat{total: total, idle: idle}, nil
}

func readMemoryPercent() string {
	total, available := readMeminfo()
	if total <= 0 {
		return "n/a"
	}
	used := total - available
	return fmt.Sprintf("%.1f%%", float64(used)/float64(total)*100)
}

func readMemoryValue(kind string) string {
	total, available := readMeminfo()
	if total <= 0 {
		return "n/a"
	}
	if kind == "total" {
		return bytesHuman(total * 1024)
	}
	return bytesHuman((total - available) * 1024)
}

func readMeminfo() (totalKB, availableKB uint64) {
	if runtime.GOOS != "linux" {
		return 0, 0
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			totalKB = value
		case "MemAvailable":
			availableKB = value
		}
	}
	return totalKB, availableKB
}

func readDiskValue(ctx context.Context, kind string) string {
	if runtime.GOOS != "linux" {
		return "n/a"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "df", "-B1", "/")
	out, err := cmd.Output()
	if err != nil {
		return "n/a"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "n/a"
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return "n/a"
	}
	total, _ := strconv.ParseUint(fields[1], 10, 64)
	used, _ := strconv.ParseUint(fields[2], 10, 64)
	switch kind {
	case "total":
		return bytesHuman(total)
	case "used":
		return bytesHuman(used)
	default:
		if total == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.1f%%", float64(used)/float64(total)*100)
	}
}

func readLoadAverage() string {
	if runtime.GOOS != "linux" {
		return "n/a"
	}
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "n/a"
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return "n/a"
	}
	return strings.Join(fields[:3], " ")
}

func readUptime() string {
	if runtime.GOOS != "linux" {
		return "n/a"
	}
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "n/a"
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "n/a"
	}
	secondsFloat, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "n/a"
	}
	duration := time.Duration(secondsFloat) * time.Second
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func systemctlStatus(ctx context.Context, service string) string {
	if runtime.GOOS != "linux" {
		return "n/a"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", service)
	out, err := cmd.Output()
	status := strings.TrimSpace(string(out))
	if status == "" && err != nil {
		return "unknown"
	}
	return status
}

func bytesHuman(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
