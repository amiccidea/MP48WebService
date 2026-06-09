//go:build linux

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

func getSystemInfo() map[string]string {
	info := make(map[string]string)

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		var uptimeSec float64
		fmt.Sscanf(string(data), "%f", &uptimeSec)
		days := int(uptimeSec) / 86400
		hours := (int(uptimeSec) % 86400) / 3600
		minutes := (int(uptimeSec) % 3600) / 60
		info["Uptime"] = fmt.Sprintf("%d giorni, %d ore, %d minuti", days, hours, minutes)
	}

	// Load average
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			info["Load1"] = fields[0]
			info["Load5"] = fields[1]
			info["Load15"] = fields[2]
		}
	}

	// Memoria
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			switch {
			case strings.HasPrefix(line, "MemTotal:"):
				info["MemTotal"] = strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:"))
			case strings.HasPrefix(line, "MemAvailable:"):
				info["MemAvailable"] = strings.TrimSpace(strings.TrimPrefix(line, "MemAvailable:"))
			case strings.HasPrefix(line, "SwapTotal:"):
				info["SwapTotal"] = strings.TrimSpace(strings.TrimPrefix(line, "SwapTotal:"))
			case strings.HasPrefix(line, "SwapFree:"):
				info["SwapFree"] = strings.TrimSpace(strings.TrimPrefix(line, "SwapFree:"))
			}
		}
	}

	// Disco
	var stat syscall.Statfs_t
	syscall.Statfs("/", &stat)
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free
	info["DiskTotal"] = fmt.Sprintf("%d MiB", total/(1024*1024))
	info["DiskUsed"] = fmt.Sprintf("%d MiB", used/(1024*1024))
	info["DiskFree"] = fmt.Sprintf("%d MiB", free/(1024*1024))

	info["GoVersion"] = runtime.Version()
	info["GOARCH"] = runtime.GOARCH
	info["GOOS"] = runtime.GOOS
	info["GOMAXPROCS"] = fmt.Sprintf("%d", runtime.GOMAXPROCS(0))
	info["GOGC"] = fmt.Sprintf("%d", 100) // default, lo puoi cambiare con env

	return info
}
