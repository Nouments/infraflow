package gns3

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// HostResources describes the machine that runs the local compute. Values
// that cannot be read on this OS are left zero and named in Unavailable.
type HostResources struct {
	CPUs           int      `json:"cpus"`
	MemTotalBytes  uint64   `json:"mem_total_bytes"`
	MemAvailBytes  uint64   `json:"mem_available_bytes"`
	DiskPath       string   `json:"disk_path"`
	DiskFreeBytes  uint64   `json:"disk_free_bytes"`
	DiskTotalBytes uint64   `json:"disk_total_bytes"`
	Load1          float64  `json:"load1"`
	Unavailable    []string `json:"unavailable,omitempty"`
}

// ReadHostResources reads CPU count, memory, load and free disk space under
// diskPath.
func ReadHostResources(diskPath string) HostResources {
	resources := HostResources{CPUs: runtime.NumCPU(), DiskPath: diskPath}
	if total, available, err := readMeminfo(); err == nil {
		resources.MemTotalBytes, resources.MemAvailBytes = total, available
	} else {
		resources.Unavailable = append(resources.Unavailable, "memory")
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		if fields := strings.Fields(string(data)); len(fields) > 0 {
			resources.Load1, _ = strconv.ParseFloat(fields[0], 64)
		}
	} else {
		resources.Unavailable = append(resources.Unavailable, "load")
	}
	if free, total, err := diskSpace(diskPath); err == nil {
		resources.DiskFreeBytes, resources.DiskTotalBytes = free, total
	} else {
		resources.Unavailable = append(resources.Unavailable, "disk")
	}
	return resources
}

// Budget is what a planned topology needs before any node is started.
type Budget struct {
	MemBytes  uint64 `json:"mem_bytes"`
	DiskBytes uint64 `json:"disk_bytes"`
	CPUs      int    `json:"cpus"`
}

// Check returns the reasons the host cannot take the budget. A reserve of
// 1 GiB RAM and 2 GiB disk is kept for the host itself. Unknown values are
// reported as problems instead of being assumed sufficient.
func (resources HostResources) Check(budget Budget) []string {
	const gib = 1 << 30
	var problems []string
	if resources.MemAvailBytes == 0 {
		problems = append(problems, "available memory unknown")
	} else if budget.MemBytes+gib > resources.MemAvailBytes {
		problems = append(problems, fmt.Sprintf("needs %d MiB RAM + 1 GiB reserve, %d MiB available", budget.MemBytes>>20, resources.MemAvailBytes>>20))
	}
	if resources.DiskFreeBytes == 0 {
		problems = append(problems, "free disk space unknown")
	} else if budget.DiskBytes+2*gib > resources.DiskFreeBytes {
		problems = append(problems, fmt.Sprintf("needs %d MiB disk + 2 GiB reserve, %d MiB free", budget.DiskBytes>>20, resources.DiskFreeBytes>>20))
	}
	if budget.CPUs > resources.CPUs {
		problems = append(problems, fmt.Sprintf("needs %d vCPU, host has %d", budget.CPUs, resources.CPUs))
	}
	return problems
}

func readMeminfo() (uint64, uint64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				values[strings.TrimSuffix(fields[0], ":")] = value * 1024
			}
		}
	}
	if values["MemTotal"] == 0 || values["MemAvailable"] == 0 {
		return 0, 0, fmt.Errorf("meminfo incomplete")
	}
	return values["MemTotal"], values["MemAvailable"], scanner.Err()
}
