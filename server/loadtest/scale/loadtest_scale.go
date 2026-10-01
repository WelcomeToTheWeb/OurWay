package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ScaleConfig holds the configuration for the large-scale load test.
type ScaleConfig struct {
	ServerURL         string
	DeviceCount       int
	Duration          time.Duration
	Concurrency       int
	HeartbeatInterval time.Duration
	MetricsInterval   time.Duration
	ReportInterval    time.Duration
	Timeout           time.Duration
}

// ScaleResults holds the results of the scale load test.
type ScaleResults struct {
	HeartbeatRequests  int64
	HeartbeatSuccess   int64
	HeartbeatFailed    int64
	MetricsRequests    int64
	MetricsSuccess     int64
	MetricsFailed      int64
	TotalBytesSent     int64
	TotalBytesReceived int64
}

func main() {
	var (
		serverURL       = flag.String("url", "http://localhost:9090", "Server URL")
		devices         = flag.Int("devices", 10000, "Number of simulated devices")
		duration        = flag.Duration("duration", 5*time.Minute, "Test duration")
		concurrency     = flag.Int("concurrency", 200, "Number of concurrent workers")
		hbInterval      = flag.Duration("heartbeat-interval", 30*time.Second, "Heartbeat interval")
		metricsInterval = flag.Duration("metrics-interval", 60*time.Second, "Metrics interval")
		reportInterval  = flag.Duration("report-interval", 30*time.Second, "Status report interval")
		timeout         = flag.Duration("timeout", 10*time.Second, "HTTP request timeout")
	)
	flag.Parse()

	cfg := ScaleConfig{
		ServerURL:         *serverURL,
		DeviceCount:       *devices,
		Duration:          *duration,
		Concurrency:       *concurrency,
		HeartbeatInterval: *hbInterval,
		MetricsInterval:   *metricsInterval,
		ReportInterval:    *reportInterval,
		Timeout:           *timeout,
	}

	fmt.Printf("=== OurWay RMM Scale Load Test ===\n")
	fmt.Printf("  Server: %s\n", cfg.ServerURL)
	fmt.Printf("  Devices: %d\n", cfg.DeviceCount)
	fmt.Printf("  Duration: %v\n", cfg.Duration)
	fmt.Printf("  Concurrency: %d\n", cfg.Concurrency)
	fmt.Printf("  Heartbeat Interval: %v\n", cfg.HeartbeatInterval)
	fmt.Printf("  Metrics Interval: %v\n", cfg.MetricsInterval)
	fmt.Printf("  Report Interval: %v\n\n", cfg.ReportInterval)

	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.Concurrency * 2,
			MaxIdleConnsPerHost: cfg.Concurrency * 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Phase 1: Register all devices
	fmt.Printf("Phase 1: Registering %d devices...\n", cfg.DeviceCount)
	registerStart := time.Now()
	deviceKeys, registerSuccess := registerDevices(client, cfg, cfg.DeviceCount)
	fmt.Printf("  Registered %d/%d devices in %v\n\n", registerSuccess, cfg.DeviceCount, time.Since(registerStart))

	if len(deviceKeys) == 0 {
		log.Fatal("No devices registered, cannot continue with load test")
	}

	// Phase 2: Run heartbeat and metrics simulation
	fmt.Printf("Phase 2: Running heartbeat and metrics simulation for %v...\n", cfg.Duration)

	results := &ScaleResults{}
	stop := make(chan struct{})

	// Start reporting goroutine
	go func() {
		ticker := time.NewTicker(cfg.ReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				hbRate := float64(atomic.LoadInt64(&results.HeartbeatRequests)) / time.Since(registerStart).Seconds()
				mRate := float64(atomic.LoadInt64(&results.MetricsRequests)) / time.Since(registerStart).Seconds()
				fmt.Printf("  [REPORT] Heartbeats: %d (%.1f/s) | Metrics: %d (%.1f/s) | "+
					"Failed: HB=%d M=%d\n",
					atomic.LoadInt64(&results.HeartbeatRequests), hbRate,
					atomic.LoadInt64(&results.MetricsRequests), mRate,
					atomic.LoadInt64(&results.HeartbeatFailed),
					atomic.LoadInt64(&results.MetricsFailed))
			case <-stop:
				return
			}
		}
	}()

	// Start device simulation workers
	runDeviceSimulation(client, cfg, deviceKeys, results, stop)

	// Wait for duration
	time.Sleep(cfg.Duration)

	// Stop simulation
	close(stop)

	// Final report
	totalRequests := atomic.LoadInt64(&results.HeartbeatRequests) + atomic.LoadInt64(&results.MetricsRequests)
	totalSuccess := atomic.LoadInt64(&results.HeartbeatSuccess) + atomic.LoadInt64(&results.MetricsSuccess)
	totalFailed := atomic.LoadInt64(&results.HeartbeatFailed) + atomic.LoadInt64(&results.MetricsFailed)
	totalDuration := time.Since(registerStart)

	fmt.Printf("\n=== Scale Load Test Results ===\n")
	fmt.Printf("  Devices:             %d\n", cfg.DeviceCount)
	fmt.Printf("  Duration:            %v\n", totalDuration)
	fmt.Printf("  Heartbeat Requests:  %d\n", atomic.LoadInt64(&results.HeartbeatRequests))
	fmt.Printf("  Heartbeat Success:   %d\n", atomic.LoadInt64(&results.HeartbeatSuccess))
	fmt.Printf("  Heartbeat Failed:    %d\n", atomic.LoadInt64(&results.HeartbeatFailed))
	fmt.Printf("  Metrics Requests:    %d\n", atomic.LoadInt64(&results.MetricsRequests))
	fmt.Printf("  Metrics Success:     %d\n", atomic.LoadInt64(&results.MetricsSuccess))
	fmt.Printf("  Metrics Failed:      %d\n", atomic.LoadInt64(&results.MetricsFailed))
	fmt.Printf("  Total Requests:      %d\n", totalRequests)
	fmt.Printf("  Total Success:       %d\n", totalSuccess)
	fmt.Printf("  Total Failed:        %d\n", totalFailed)
	fmt.Printf("  Requests/sec:        %.2f\n", float64(totalRequests)/totalDuration.Seconds())
	successRate := float64(totalSuccess) / float64(totalRequests) * 100
	fmt.Printf("  Success Rate:        %.2f%%\n", successRate)
	fmt.Printf("  Total Bytes Sent:    %d\n", atomic.LoadInt64(&results.TotalBytesSent))
	fmt.Printf("  Total Bytes Received: %d\n", atomic.LoadInt64(&results.TotalBytesReceived))

	if successRate < 99.0 {
		log.Printf("WARNING: Success rate below 99%% target")
	} else {
		fmt.Printf("\n  PASS: All targets met\n")
	}
}

func registerDevices(client *http.Client, cfg ScaleConfig, count int) ([]string, int) {
	var deviceKeys []string
	var registered int64
	var mu sync.Mutex

	// Register in batches with limited concurrency
	batchSize := 50
	var wg sync.WaitGroup
	sem := make(chan struct{}, batchSize)

	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			body := map[string]interface{}{
				"name":     fmt.Sprintf("Scale Test Device %d", i),
				"hostname": fmt.Sprintf("scale-%d.local", i),
				"os":       "linux",
				"arch":     "amd64",
				"version":  "1.0.0",
			}
			bodyBytes, _ := json.Marshal(body)

			req, err := http.NewRequest("POST", cfg.ServerURL+"/api/agent/register", bytes.NewReader(bodyBytes))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 || resp.StatusCode == 201 {
				var result struct {
					Device struct {
						DeviceKey string `json:"-"`
					} `json:"device"`
					DeviceKey string `json:"device_key"`
				}
				json.NewDecoder(resp.Body).Decode(&result)

				if result.DeviceKey != "" {
					atomic.AddInt64(&registered, 1)
					mu.Lock()
					deviceKeys = append(deviceKeys, result.DeviceKey)
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()
	return deviceKeys, int(registered)
}

func runDeviceSimulation(client *http.Client, cfg ScaleConfig, deviceKeys []string, results *ScaleResults, stop chan struct{}) {
	var wg sync.WaitGroup

	// Distribute devices across workers
	devicesPerWorker := len(deviceKeys) / cfg.Concurrency
	if devicesPerWorker < 1 {
		devicesPerWorker = 1
	}

	for w := 0; w < cfg.Concurrency; w++ {
		startIdx := w * devicesPerWorker
		endIdx := startIdx + devicesPerWorker
		if endIdx > len(deviceKeys) {
			endIdx = len(deviceKeys)
		}

		workerDevices := deviceKeys[startIdx:endIdx]
		if len(workerDevices) == 0 {
			continue
		}

		wg.Add(1)
		go func(devices []string) {
			defer wg.Done()

			// Each worker manages its assigned devices
			for _, key := range devices {
				go simulateDevice(client, cfg, key, results, stop)
			}
		}(workerDevices)
	}

	// Wait for stop signal then all workers
	<-stop
	wg.Wait()
}

func simulateDevice(client *http.Client, cfg ScaleConfig, deviceKey string, results *ScaleResults, stop chan struct{}) {
	hbTicker := time.NewTicker(cfg.HeartbeatInterval)
	metricsTicker := time.NewTicker(cfg.MetricsInterval)
	defer hbTicker.Stop()
	defer metricsTicker.Stop()

	// Send immediate heartbeat
	sendHeartbeat(client, cfg, deviceKey, results)

	for {
		select {
		case <-hbTicker.C:
			sendHeartbeat(client, cfg, deviceKey, results)
		case <-metricsTicker.C:
			sendMetrics(client, cfg, deviceKey, results)
		case <-stop:
			return
		}
	}
}

func sendHeartbeat(client *http.Client, cfg ScaleConfig, deviceKey string, results *ScaleResults) {
	atomic.AddInt64(&results.HeartbeatRequests, 1)

	body := map[string]interface{}{
		"timestamp": time.Now().Unix(),
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/agent/heartbeat", bytes.NewReader(bodyBytes))
	if err != nil {
		atomic.AddInt64(&results.HeartbeatFailed, 1)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Key", deviceKey)

	resp, err := client.Do(req)
	if err != nil {
		atomic.AddInt64(&results.HeartbeatFailed, 1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	atomic.AddInt64(&results.TotalBytesSent, int64(len(bodyBytes)))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		atomic.AddInt64(&results.HeartbeatSuccess, 1)
	} else {
		atomic.AddInt64(&results.HeartbeatFailed, 1)
	}
}

func sendMetrics(client *http.Client, cfg ScaleConfig, deviceKey string, results *ScaleResults) {
	atomic.AddInt64(&results.MetricsRequests, 1)

	body := map[string]interface{}{
		"cpu":        rand.Float64() * 100,
		"ram":        rand.Float64() * 100,
		"ram_used":   rand.Uint64() % 16000000000,
		"ram_total":  16000000000,
		"disk_usage": rand.Float64() * 100,
		"disk_used":  rand.Uint64() % 500000000000,
		"disk_total": 500000000000,
		"net_in":     rand.Uint64() % 1000000000,
		"net_out":    rand.Uint64() % 1000000000,
		"uptime":     rand.Uint64() % 1000000,
		"processes":  rand.Intn(500) + 50,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", cfg.ServerURL+"/api/agent/metrics", bytes.NewReader(bodyBytes))
	if err != nil {
		atomic.AddInt64(&results.MetricsFailed, 1)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Key", deviceKey)

	resp, err := client.Do(req)
	if err != nil {
		atomic.AddInt64(&results.MetricsFailed, 1)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	atomic.AddInt64(&results.TotalBytesSent, int64(len(bodyBytes)))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		atomic.AddInt64(&results.MetricsSuccess, 1)
	} else {
		atomic.AddInt64(&results.MetricsFailed, 1)
	}
}
