//go:build infraflow_pprof

// Profiling build for the load harness only (go build -tags infraflow_pprof).
// Normal builds do not contain this file. When INFRAFLOW_CPU_PROFILE is set,
// the process writes a CPU profile to <path>.<pid> for
// INFRAFLOW_CPU_PROFILE_SECONDS (default 10) and then stops profiling.
package main

import (
	"os"
	"runtime/pprof"
	"strconv"
	"time"
)

func init() {
	path := os.Getenv("INFRAFLOW_CPU_PROFILE")
	if path == "" {
		return
	}
	seconds, err := strconv.Atoi(os.Getenv("INFRAFLOW_CPU_PROFILE_SECONDS"))
	if err != nil || seconds <= 0 {
		seconds = 10
	}
	file, err := os.Create(path + "." + strconv.Itoa(os.Getpid()))
	if err != nil || pprof.StartCPUProfile(file) != nil {
		return
	}
	time.AfterFunc(time.Duration(seconds)*time.Second, func() {
		pprof.StopCPUProfile()
		_ = file.Close()
	})
}
