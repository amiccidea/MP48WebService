//go:build windows

package main

func getSystemInfo() map[string]string {
	return map[string]string{
		"Uptime":       "Debug mode (Windows)",
		"Load1":        "0.00",
		"Load5":        "0.00",
		"Load15":       "0.00",
		"MemTotal":     "Simulated 1 GB",
		"MemAvailable": "Simulated 512 MB",
		"SwapTotal":    "Simulated 256 MB",
		"SwapFree":     "Simulated 128 MB",
		"DiskTotal":    "Simulated 32 GB",
		"DiskUsed":     "Simulated 8 GB",
		"DiskFree":     "Simulated 24 GB",
	}
}
