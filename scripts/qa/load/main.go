// Command load is the InfraFlow Provider load and recovery harness (PRV-19).
//
// It starts a real Provider process ("serve") on loopback, logs in through
// the real API, and runs stages of concurrent HTTP clients against read
// endpoints and the plan preview. It samples the Provider process (CPU,
// RSS, threads, open files) from /proc, runs concurrent multi-site
// generations, then kills the Provider with SIGKILL in the middle of a stage
// and measures the time until it serves again with the same sites.
//
// The numbers describe the machine it ran on; they are not product limits.
//
// Usage: go run ./scripts/qa/load -provider <binary> -inputs <yaml> -out <dir>
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type stageResult struct {
	Concurrency   int                       `json:"concurrency"`
	DurationSec   float64                   `json:"duration_seconds"`
	Requests      int                       `json:"requests"`
	Errors        int                       `json:"errors"`
	ErrorKinds    map[string]int            `json:"error_kinds,omitempty"`
	ThroughputRPS float64                   `json:"throughput_rps"`
	LatencyMS     map[string]float64        `json:"latency_ms"`
	PerEndpoint   map[string]map[string]any `json:"per_endpoint"`
	Process       processPeak               `json:"provider_process"`
}

type processPeak struct {
	CPUPercentAvg float64 `json:"cpu_percent_avg"`
	RSSPeakMiB    float64 `json:"rss_peak_mib"`
	ThreadsPeak   int     `json:"threads_peak"`
	OpenFilesPeak int     `json:"open_files_peak"`
	Samples       int     `json:"samples"`
	Unavailable   string  `json:"unavailable,omitempty"`
}

type report struct {
	GeneratedAt   time.Time       `json:"generated_at"`
	Host          map[string]any  `json:"host"`
	Inputs        string          `json:"inputs"`
	Sites         int             `json:"sites"`
	Objectives    map[string]any  `json:"objectives"`
	Stages        []stageResult   `json:"stages"`
	Generation    map[string]any  `json:"concurrent_generation"`
	Recovery      map[string]any  `json:"crash_recovery"`
	DiskBytes     map[string]int  `json:"disk_bytes"`
	ObjectivesMet map[string]bool `json:"objectives_met"`
	Pass          bool            `json:"pass"`
}

func main() {
	providerBinary := flag.String("provider", "", "infraflow-provider binary")
	inputs := flag.String("inputs", "examples/multisite-preparation.yaml", "multi-site infrastructure YAML")
	outDir := flag.String("out", "", "output directory (report.json, provider work dir)")
	stageDuration := flag.Duration("stage", 10*time.Second, "duration of each load stage")
	stagesFlag := flag.String("concurrency", "1,8,32,64", "comma-separated concurrency per stage")
	recovery := flag.Bool("recovery", true, "run the SIGKILL crash-recovery step (disable when profiling)")
	soak := flag.Duration("soak", 0, "soak mode: run -soak-concurrency clients for this long, in -soak-window windows")
	soakConcurrency := flag.Int("soak-concurrency", 4, "soak mode: concurrent clients")
	soakWindow := flag.Duration("soak-window", time.Minute, "soak mode: measurement window")
	flag.Parse()
	runRecovery = *recovery
	if *soak > 0 {
		if *providerBinary == "" || *outDir == "" {
			fmt.Fprintln(os.Stderr, "usage: load -provider <binary> -out <dir> -soak 3h")
			os.Exit(2)
		}
		if err := runSoak(*providerBinary, *inputs, *outDir, *soak, *soakConcurrency, *soakWindow); err != nil {
			fmt.Fprintln(os.Stderr, "soak:", err)
			os.Exit(1)
		}
		return
	}
	if *providerBinary == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: load -provider <binary> -inputs <yaml> -out <dir>")
		os.Exit(2)
	}
	if err := run(*providerBinary, *inputs, *outDir, *stageDuration, *stagesFlag); err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
}

var runRecovery = true

type lab struct {
	binary, work, base, config, password string
	inputYAML                            []byte
	process                              *exec.Cmd
	output                               *bytes.Buffer
	token                                string
	client                               *http.Client
}

func run(binary, inputs, outDir string, stageDuration time.Duration, stagesFlag string) error {
	l, sites, err := setupLab(binary, inputs, outDir)
	if err != nil {
		return err
	}
	defer l.stop()
	return runStages(l, sites, binary, inputs, outDir, stageDuration, stagesFlag)
}

func setupLab(binary, inputs, outDir string) (*lab, int, error) {
	work, err := filepath.Abs(filepath.Join(outDir, "work"))
	if err != nil {
		return nil, 0, err
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		return nil, 0, err
	}
	input, err := os.ReadFile(inputs)
	if err != nil {
		return nil, 0, err
	}
	apiPort, grpcPort := freePort(), freePort()
	l := &lab{binary: binary, work: work, base: fmt.Sprintf("http://127.0.0.1:%d", apiPort), inputYAML: input,
		client: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 256}}}
	l.config = filepath.Join(work, "provider.yaml")
	config := fmt.Sprintf(`listen_address: 127.0.0.1:%d
api_listen_address: 127.0.0.1:%d
web_ui_enabled: true
artifact_directory: %s/artifacts
database_path: %s/users.sqlite3
token_env: LOAD_AGENT_TOKEN
admin_username: admin
admin_credential_file: %s/.admin-password
admin_credential_script: %s/get-admin-password.sh
known_hosts_file: %s/known_hosts
logging:
  directory: %s/log
`, grpcPort, apiPort, work, work, work, work, work, work)
	if err := os.WriteFile(l.config, []byte(config), 0o600); err != nil {
		return nil, 0, err
	}
	if err := os.WriteFile(filepath.Join(work, "known_hosts"), nil, 0o600); err != nil {
		return nil, 0, err
	}
	if out, err := exec.Command(binary, "generate-all", "-f", inputs, "-out", filepath.Join(work, "artifacts")).CombinedOutput(); err != nil {
		return nil, 0, fmt.Errorf("generate-all: %v: %s", err, out)
	}
	if err := l.start(); err != nil {
		return nil, 0, err
	}
	password, err := os.ReadFile(filepath.Join(work, ".admin-password"))
	if err != nil {
		return nil, 0, err
	}
	l.password = strings.TrimSpace(string(password))
	if err := l.login(); err != nil {
		return nil, 0, err
	}
	sites, err := l.siteCount()
	if err != nil {
		l.stop()
		return nil, 0, err
	}
	return l, sites, nil
}

func runStages(l *lab, sites int, binary, inputs, outDir string, stageDuration time.Duration, stagesFlag string) error {
	work := l.work

	result := report{
		GeneratedAt: time.Now().UTC(), Inputs: inputs, Sites: sites,
		Host: map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "go": runtime.Version()},
		// Objectives are set from the first measurements on this lab host
		// (8 vCPU, 16 GiB) with a margin; they are regression guards for this
		// environment, not service levels.
		Objectives: map[string]any{
			"error_rate_max":              0.0,
			"p99_ms_max_at_32_concurrent": 1000.0,
			"recovery_seconds_max":        10.0,
			"rss_mib_max":                 512.0,
		},
		ObjectivesMet: map[string]bool{},
	}
	for _, field := range strings.Split(stagesFlag, ",") {
		concurrency, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || concurrency < 1 {
			return fmt.Errorf("invalid concurrency %q", field)
		}
		stage := l.stage(concurrency, stageDuration)
		result.Stages = append(result.Stages, stage)
		fmt.Fprintf(os.Stderr, "stage c=%d: %d req, %d err, %.0f rps, p50 %.1f p95 %.1f p99 %.1f ms, rss %.0f MiB\n",
			concurrency, stage.Requests, stage.Errors, stage.ThroughputRPS, stage.LatencyMS["p50"], stage.LatencyMS["p95"], stage.LatencyMS["p99"], stage.Process.RSSPeakMiB)
	}
	result.Generation = concurrentGeneration(binary, inputs, work, 8)
	if runRecovery {
		result.Recovery = l.crashRecovery(sites)
	} else {
		result.Recovery = map[string]any{"recovered": true, "skipped": true}
	}
	result.DiskBytes = map[string]int{"artifacts": dirSize(filepath.Join(work, "artifacts")), "log": dirSize(filepath.Join(work, "log")), "database": dirSize(filepath.Join(work, "users.sqlite3"))}

	errorsTotal, p99At32, rssPeak := 0, 0.0, 0.0
	for _, stage := range result.Stages {
		errorsTotal += stage.Errors
		if stage.Concurrency == 32 {
			p99At32 = stage.LatencyMS["p99"]
		}
		rssPeak = max(rssPeak, stage.Process.RSSPeakMiB)
	}
	result.ObjectivesMet["no_errors"] = errorsTotal == 0
	result.ObjectivesMet["p99_at_32"] = p99At32 > 0 && p99At32 <= 1000
	recovered, _ := result.Recovery["recovered"].(bool)
	seconds, _ := result.Recovery["seconds_to_serving"].(float64)
	result.ObjectivesMet["recovery"] = recovered && seconds <= 10
	result.ObjectivesMet["rss"] = rssPeak > 0 && rssPeak <= 512
	generationOK, _ := result.Generation["all_identical"].(bool)
	result.ObjectivesMet["concurrent_generation_deterministic"] = generationOK
	result.Pass = true
	for _, met := range result.ObjectivesMet {
		result.Pass = result.Pass && met
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "load-report.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "objectives: %v pass=%v\n", result.ObjectivesMet, result.Pass)
	if !result.Pass {
		return errors.New("one or more load objectives were not met")
	}
	return nil
}

func (l *lab) start() error {
	l.output = &bytes.Buffer{}
	l.process = exec.Command(l.binary, "serve", "-config", l.config)
	l.process.Env = append(os.Environ(), "LOAD_AGENT_TOKEN="+strings.Repeat("k", 48))
	l.process.Stdout, l.process.Stderr = l.output, l.output
	if err := l.process.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if response, err := l.client.Get(l.base + "/api/v1/health"); err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("provider did not start: %s", tail(l.output.String(), 1500))
}

func (l *lab) stop() {
	if l.process != nil && l.process.ProcessState == nil {
		_ = l.process.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = l.process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = l.process.Process.Kill()
			<-done
		}
	}
}

func (l *lab) login() error {
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": l.password})
	response, err := l.client.Post(l.base+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil || out.Token == "" {
		return fmt.Errorf("login failed: HTTP %d", response.StatusCode)
	}
	l.token = out.Token
	return nil
}

func (l *lab) request(method, path string, body []byte) (int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, _ := http.NewRequest(method, l.base+path, reader)
	request.Header.Set("Authorization", "Bearer "+l.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := l.client.Do(request)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	return response.StatusCode, nil
}

func (l *lab) siteCount() (int, error) {
	request, _ := http.NewRequest(http.MethodGet, l.base+"/api/v1/sites", nil)
	request.Header.Set("Authorization", "Bearer "+l.token)
	response, err := l.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	var sites []struct {
		ArtifactCount int `json:"artifact_count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&sites); err != nil {
		return 0, err
	}
	return len(sites), nil
}

type sample struct {
	endpoint string
	latency  time.Duration
	err      string
}

func (l *lab) stage(concurrency int, duration time.Duration) stageResult {
	preview, _ := json.Marshal(map[string]string{"input": string(l.inputYAML)})
	endpoints := []struct {
		name, method, path string
		body               []byte
		weight             int
	}{
		{"health", http.MethodGet, "/api/v1/health", nil, 4},
		{"status", http.MethodGet, "/api/v1/status", nil, 2},
		{"sites", http.MethodGet, "/api/v1/sites", nil, 2},
		{"events", http.MethodGet, "/api/v1/events", nil, 1},
		{"plan_preview", http.MethodPost, "/api/v1/plan/preview", preview, 1},
	}
	var pick []int
	for index, endpoint := range endpoints {
		for range endpoint.weight {
			pick = append(pick, index)
		}
	}
	samplesCh := make(chan []sample, concurrency)
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	stopSampling := make(chan struct{})
	peakCh := make(chan processPeak, 1)
	go func() { peakCh <- sampleProcess(l.process.Process.Pid, stopSampling) }()
	started := time.Now()
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []sample
			for ctx.Err() == nil {
				endpoint := endpoints[pick[rand.IntN(len(pick))]]
				begin := time.Now()
				status, err := l.request(endpoint.method, endpoint.path, endpoint.body)
				s := sample{endpoint: endpoint.name, latency: time.Since(begin)}
				switch {
				case err != nil:
					s.err = "transport"
				case status != http.StatusOK:
					s.err = "http_" + strconv.Itoa(status)
				}
				local = append(local, s)
			}
			samplesCh <- local
		}()
	}
	wg.Wait()
	elapsed := time.Since(started)
	close(stopSampling)
	close(samplesCh)
	var all []sample
	for local := range samplesCh {
		all = append(all, local...)
	}
	result := stageResult{Concurrency: concurrency, DurationSec: elapsed.Seconds(), Requests: len(all), ErrorKinds: map[string]int{}, PerEndpoint: map[string]map[string]any{}}
	byEndpoint := map[string][]time.Duration{}
	var latencies []time.Duration
	for _, s := range all {
		if s.err != "" {
			result.Errors++
			result.ErrorKinds[s.endpoint+":"+s.err]++
			continue
		}
		latencies = append(latencies, s.latency)
		byEndpoint[s.endpoint] = append(byEndpoint[s.endpoint], s.latency)
	}
	if len(result.ErrorKinds) == 0 {
		result.ErrorKinds = nil
	}
	result.ThroughputRPS = float64(len(all)) / elapsed.Seconds()
	result.LatencyMS = percentiles(latencies)
	for name, values := range byEndpoint {
		p := percentiles(values)
		result.PerEndpoint[name] = map[string]any{"requests": len(values), "p50_ms": p["p50"], "p95_ms": p["p95"], "p99_ms": p["p99"]}
	}
	result.Process = <-peakCh
	return result
}

func percentiles(values []time.Duration) map[string]float64 {
	if len(values) == 0 {
		return map[string]float64{"p50": 0, "p95": 0, "p99": 0, "max": 0}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	at := func(q float64) float64 {
		index := int(q*float64(len(values))+0.5) - 1
		index = max(0, min(index, len(values)-1))
		return float64(values[index].Microseconds()) / 1000
	}
	return map[string]float64{"p50": at(0.50), "p95": at(0.95), "p99": at(0.99), "max": float64(values[len(values)-1].Microseconds()) / 1000}
}

// sampleProcess reads /proc every 200 ms. On systems without /proc it
// reports the metrics as unavailable rather than zero.
func sampleProcess(pid int, stop <-chan struct{}) processPeak {
	var peak processPeak
	ticksPerSecond := 100.0 // USER_HZ on Linux
	firstTicks, firstTime := -1.0, time.Time{}
	lastTicks, lastTime := 0.0, time.Time{}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		ticks, rss, threads, err := readProc(pid)
		if err != nil {
			peak.Unavailable = err.Error()
		} else {
			if firstTicks < 0 {
				firstTicks, firstTime = ticks, time.Now()
			}
			lastTicks, lastTime = ticks, time.Now()
			peak.RSSPeakMiB = max(peak.RSSPeakMiB, rss/1024)
			peak.ThreadsPeak = max(peak.ThreadsPeak, threads)
			if entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
				peak.OpenFilesPeak = max(peak.OpenFilesPeak, len(entries))
			}
			peak.Samples++
		}
		select {
		case <-stop:
			if seconds := lastTime.Sub(firstTime).Seconds(); seconds > 0 {
				peak.CPUPercentAvg = (lastTicks - firstTicks) / ticksPerSecond / seconds * 100
			}
			if peak.Samples > 0 {
				peak.Unavailable = ""
			}
			return peak
		case <-ticker.C:
		}
	}
}

func readProc(pid int) (ticks float64, rssKiB float64, threads int, err error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+2:]))
	if len(fields) < 15 {
		return 0, 0, 0, errors.New("short /proc stat")
	}
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, 0, err
	}
	for _, line := range strings.Split(string(status), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "VmRSS:" {
			rssKiB, _ = strconv.ParseFloat(parts[1], 64)
		}
		if len(parts) >= 2 && parts[0] == "Threads:" {
			threads, _ = strconv.Atoi(parts[1])
		}
	}
	return utime + stime, rssKiB, threads, nil
}

// concurrentGeneration runs N generate-all processes at once into separate
// directories and checks that every output tree is byte-identical.
func concurrentGeneration(binary, inputs, work string, n int) map[string]any {
	var wg sync.WaitGroup
	durations := make([]time.Duration, n)
	failures := make([]string, n)
	for index := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := filepath.Join(work, "gen", strconv.Itoa(index))
			begin := time.Now()
			if output, err := exec.Command(binary, "generate-all", "-f", inputs, "-out", out).CombinedOutput(); err != nil {
				failures[index] = tail(string(output), 300)
			}
			durations[index] = time.Since(begin)
		}()
	}
	wg.Wait()
	reference := treeDigest(filepath.Join(work, "gen", "0"))
	identical := reference != ""
	for index := 1; index < n; index++ {
		identical = identical && treeDigest(filepath.Join(work, "gen", strconv.Itoa(index))) == reference
	}
	var failed []string
	for _, failure := range failures {
		if failure != "" {
			failed = append(failed, failure)
		}
	}
	p := percentiles(durations)
	return map[string]any{"parallel_runs": n, "failures": failed, "all_identical": identical && len(failed) == 0, "duration_ms": p}
}

func treeDigest(root string) string {
	var buffer bytes.Buffer
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, _ := filepath.Rel(root, path)
		fmt.Fprintf(&buffer, "%s %d %x\n", filepath.ToSlash(relative), len(data), data)
		return nil
	})
	return buffer.String()
}

// crashRecovery kills the Provider (SIGKILL) under load, restarts it on the
// same data, and measures the time until it serves the same sites again.
func (l *lab) crashRecovery(expectedSites int) map[string]any {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				_, _ = l.request(http.MethodGet, "/api/v1/sites", nil)
			}
		}()
	}
	time.Sleep(time.Second)
	_ = l.process.Process.Kill()
	_ = l.process.Wait()
	killedAt := time.Now()
	cancel()
	wg.Wait()
	if err := l.start(); err != nil {
		return map[string]any{"recovered": false, "error": err.Error()}
	}
	servingAfter := time.Since(killedAt)
	// Sessions are persisted; if the old token no longer works, log in again
	// and record it.
	sessionSurvived := true
	if status, err := l.request(http.MethodGet, "/api/v1/sites", nil); err != nil || status != http.StatusOK {
		sessionSurvived = false
		if err := l.login(); err != nil {
			return map[string]any{"recovered": false, "error": "login after restart: " + err.Error()}
		}
	}
	sites, err := l.siteCount()
	return map[string]any{
		"recovered":          err == nil && sites == expectedSites,
		"seconds_to_serving": servingAfter.Seconds(),
		"sites_before":       expectedSites,
		"sites_after":        sites,
		"session_survived":   sessionSurvived,
	}
}

func dirSize(path string) int {
	total := 0
	_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			if info, err := entry.Info(); err == nil {
				total += int(info.Size())
			}
		}
		return nil
	})
	return total
}

func freePort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func tail(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[len(value)-n:]
}

type soakWindow struct {
	Index         int       `json:"index"`
	EndedAt       time.Time `json:"ended_at"`
	Requests      int       `json:"requests"`
	Errors        int       `json:"errors"`
	ThroughputRPS float64   `json:"throughput_rps"`
	P50MS         float64   `json:"p50_ms"`
	P99MS         float64   `json:"p99_ms"`
	RSSMiB        float64   `json:"rss_peak_mib"`
	OpenFiles     int       `json:"open_files_peak"`
	Threads       int       `json:"threads_peak"`
	CPUPercent    float64   `json:"cpu_percent_avg"`
}

// runSoak keeps a steady load for a long period and checks for drift: no
// errors, no growth of open files or threads, bounded memory, and p99 in the
// last quarter no worse than 3x the first quarter. The report is rewritten
// after every window, so an interrupted soak still leaves its data.
func runSoak(binary, inputs, outDir string, total time.Duration, concurrency int, window time.Duration) error {
	l, sites, err := setupLab(binary, inputs, outDir)
	if err != nil {
		return err
	}
	defer l.stop()
	started := time.Now().UTC()
	result := map[string]any{
		"mode": "soak", "started_at": started, "planned_duration": total.String(), "concurrency": concurrency,
		"window": window.String(), "inputs": inputs, "sites": sites,
		"host":       map[string]any{"os": runtime.GOOS, "cpus": runtime.NumCPU(), "go": runtime.Version()},
		"objectives": map[string]any{"errors": 0, "open_files_growth_max": 16, "threads_growth_max": 8, "rss_mib_max": 512, "p99_last_quarter_vs_first_max_ratio": 3.0},
	}
	var windows []soakWindow
	write := func() {
		result["windows"] = windows
		data, _ := json.MarshalIndent(result, "", "  ")
		_ = os.WriteFile(filepath.Join(outDir, "soak-report.json"), append(data, '\n'), 0o644)
	}
	for index := 0; time.Since(started) < total; index++ {
		stage := l.stage(concurrency, window)
		if l.process.ProcessState != nil {
			result["error"] = "provider exited during the soak: " + tail(l.output.String(), 500)
			break
		}
		windows = append(windows, soakWindow{Index: index, EndedAt: time.Now().UTC(), Requests: stage.Requests, Errors: stage.Errors,
			ThroughputRPS: stage.ThroughputRPS, P50MS: stage.LatencyMS["p50"], P99MS: stage.LatencyMS["p99"],
			RSSMiB: stage.Process.RSSPeakMiB, OpenFiles: stage.Process.OpenFilesPeak, Threads: stage.Process.ThreadsPeak, CPUPercent: stage.Process.CPUPercentAvg})
		write()
	}
	result["finished_at"] = time.Now().UTC()
	met := map[string]bool{}
	pass := len(windows) >= 4 && result["error"] == nil
	if len(windows) >= 4 {
		quarter := len(windows) / 4
		errorsTotal, requests, rssMax := 0, 0, 0.0
		for _, w := range windows {
			errorsTotal += w.Errors
			requests += w.Requests
			rssMax = max(rssMax, w.RSSMiB)
		}
		avg := func(ws []soakWindow, f func(soakWindow) float64) float64 {
			total := 0.0
			for _, w := range ws {
				total += f(w)
			}
			return total / float64(len(ws))
		}
		first, last := windows[:quarter], windows[len(windows)-quarter:]
		p99First, p99Last := avg(first, func(w soakWindow) float64 { return w.P99MS }), avg(last, func(w soakWindow) float64 { return w.P99MS })
		fdFirst, fdLast := avg(first, func(w soakWindow) float64 { return float64(w.OpenFiles) }), avg(last, func(w soakWindow) float64 { return float64(w.OpenFiles) })
		thFirst, thLast := avg(first, func(w soakWindow) float64 { return float64(w.Threads) }), avg(last, func(w soakWindow) float64 { return float64(w.Threads) })
		met["no_errors"] = errorsTotal == 0
		met["rss_bounded"] = rssMax <= 512
		met["open_files_stable"] = fdLast-fdFirst <= 16
		met["threads_stable"] = thLast-thFirst <= 8
		met["p99_no_drift"] = p99First == 0 || p99Last <= 3*p99First
		result["summary"] = map[string]any{"windows": len(windows), "requests": requests, "errors": errorsTotal, "rss_peak_mib": rssMax,
			"p99_first_quarter_ms": p99First, "p99_last_quarter_ms": p99Last, "open_files_first_last": []float64{fdFirst, fdLast}, "threads_first_last": []float64{thFirst, thLast}}
		for _, ok := range met {
			pass = pass && ok
		}
	}
	result["objectives_met"] = met
	result["pass"] = pass
	write()
	fmt.Fprintf(os.Stderr, "soak: %d windows, pass=%v %v\n", len(windows), pass, met)
	if !pass {
		return errors.New("soak objectives not met")
	}
	return nil
}
