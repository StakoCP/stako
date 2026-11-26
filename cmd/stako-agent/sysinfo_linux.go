//go:build linux

// SPDX-License-Identifier: MIT
package main

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func collectSystemInfo() map[string]any {
	data := map[string]any{}
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				data["os"] = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
				break
			}
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		data["kernel"] = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) >= 3 {
			l1, _ := strconv.ParseFloat(fields[0], 64)
			l5, _ := strconv.ParseFloat(fields[1], 64)
			l15, _ := strconv.ParseFloat(fields[2], 64)
			data["load_avg"] = []float64{l1, l5, l15}
			cpus := float64(runtime.NumCPU())
			if cpus > 0 {
				cpuPct := math.Min(100, math.Round((l1/cpus)*1000)/10)
				data["cpu_percent"] = cpuPct
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail int64
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			v, _ := strconv.ParseInt(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				total = v * 1024
			case "MemAvailable:":
				avail = v * 1024
			}
		}
		if total > 0 {
			data["memory_total_bytes"] = total
			data["memory_available_bytes"] = avail
			data["memory_percent"] = math.Round((float64(total-avail)/float64(total))*1000) / 10
		}
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil && st.Blocks > 0 {
		total := st.Blocks * uint64(st.Bsize)
		free := st.Bavail * uint64(st.Bsize)
		used := total - free
		data["disk_total_bytes"] = total
		data["disk_free_bytes"] = free
		data["disk_percent"] = math.Round((float64(used)/float64(total))*1000) / 10
	}
	return data
}
