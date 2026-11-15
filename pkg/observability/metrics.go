package observability

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

var generationDurationBuckets = [...]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

type Metrics struct {
	mu                 sync.Mutex
	generationBuckets  [len(generationDurationBuckets)]uint64
	generationCount    uint64
	generationDuration float64
	generationErrors   uint64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (metrics *Metrics) ObserveGeneration(seconds float64, failed bool) {
	if metrics == nil {
		return
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.generationCount++
	metrics.generationDuration += seconds
	if failed {
		metrics.generationErrors++
	}
	for index, bound := range generationDurationBuckets {
		if seconds <= bound {
			metrics.generationBuckets[index]++
		}
	}
}

func (metrics *Metrics) Prometheus(executions map[string]int, synchronizedAgents int) string {
	var generationBuckets [len(generationDurationBuckets)]uint64
	var count, generationErrors uint64
	var duration float64
	if metrics != nil {
		metrics.mu.Lock()
		generationBuckets = metrics.generationBuckets
		count = metrics.generationCount
		duration = metrics.generationDuration
		generationErrors = metrics.generationErrors
		metrics.mu.Unlock()
	}

	var output strings.Builder
	output.WriteString("# HELP infraflow_generation_duration_seconds Duration of HTTP artifact generation requests.\n")
	output.WriteString("# TYPE infraflow_generation_duration_seconds histogram\n")
	for index, bound := range generationDurationBuckets {
		fmt.Fprintf(&output, "infraflow_generation_duration_seconds_bucket{le=%q} %d\n", fmt.Sprintf("%g", bound), generationBuckets[index])
	}
	fmt.Fprintf(&output, "infraflow_generation_duration_seconds_bucket{le=\"+Inf\"} %d\n", count)
	fmt.Fprintf(&output, "infraflow_generation_duration_seconds_sum %g\n", duration)
	fmt.Fprintf(&output, "infraflow_generation_duration_seconds_count %d\n", count)
	output.WriteString("# HELP infraflow_generation_errors_total Failed HTTP artifact generation requests.\n")
	output.WriteString("# TYPE infraflow_generation_errors_total counter\n")
	fmt.Fprintf(&output, "infraflow_generation_errors_total %d\n", generationErrors)
	output.WriteString("# HELP infraflow_executions Current persisted executions by lifecycle status.\n")
	output.WriteString("# TYPE infraflow_executions gauge\n")
	statuses := make([]string, 0, len(executions))
	for status := range executions {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	for _, status := range statuses {
		fmt.Fprintf(&output, "infraflow_executions{status=%q} %d\n", status, executions[status])
	}
	output.WriteString("# HELP infraflow_agents_synchronized Number of registered agents currently synchronized with the provider.\n")
	output.WriteString("# TYPE infraflow_agents_synchronized gauge\n")
	fmt.Fprintf(&output, "infraflow_agents_synchronized %d\n", synchronizedAgents)
	return output.String()
}
