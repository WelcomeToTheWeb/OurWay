package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	ServerURL    string
	DeviceCount  int
	RequestCount int
	Concurrency  int
	Timeout      time.Duration
}

type Results struct {
	TotalRequests  int64
	SuccessCount   int64
	FailureCount   int64
	TotalDuration  time.Duration
	SlowestRequest time.Duration
	FastestRequest time.Duration
	RequestsPerSec float64
	BytesSent      int64
	BytesReceived  int64
}

func main() {
	cfg := Config{
		ServerURL:    "http://localhost:9090",
		DeviceCount:  100,
		RequestCount: 1000,
		Concurrency:  50,
		Timeout:      10 * time.Second,
	}

	// Parse environment overrides
	if url := os.Getenv("LOADTEST_URL"); url != "" {
		cfg.ServerURL = url
	}
	if count := os.Getenv("LOADTEST_DEVICE_COUNT"); count != "" {
		fmt.Sscanf(count, "%d", &cfg.DeviceCount)
	}
	if reqs := os.Getenv("LOADTEST_REQUESTS"); reqs != "" {
		fmt.Sscanf(reqs, "%d", &cfg.RequestCount)
	}
	if conc := os.Getenv("LOADTEST_CONCURRENCY"); conc != "" {
		fmt.Sscanf(conc, "%d", &cfg.Concurrency)
	}

	fmt.Printf("Load Test Configuration:\n")
	fmt.Printf("  Server URL: %s\n", cfg.ServerURL)
	fmt.Printf("  Simulated Devices: %d\n", cfg.DeviceCount)
	fmt.Printf("  Total Requests: %d\n", cfg.RequestCount)
	fmt.Printf("  Concurrency: %d\n", cfg.Concurrency)
	fmt.Printf("  Timeout: %v\n\n", cfg.Timeout)

	client := &http.Client{Timeout: cfg.Timeout}

	// Phase 1: Register devices
	fmt.Printf("Phase 1: Registering %d devices...\n", cfg.DeviceCount)
	registerStart := time.Now()
	deviceKeys, registerResults := registerDevices(client, cfg, cfg.DeviceCount)
	fmt.Printf("  Registered %d devices in %v\n\n", registerResults, time.Since(registerStart))

	if len(deviceKeys) == 0 {
		log.Fatal("No devices registered, cannot continue with load test")
	}

	// Phase 2: Heartbeat load
	fmt.Printf("Phase 2: Sending %d heartbeats with %d concurrent workers...\n", cfg.RequestCount, cfg.Concurrency)
	heartbeatStart := time.Now()
	results := sendHeartbeats(client, cfg, deviceKeys)
	heartbeatDuration := time.Since(heartbeatStart)

	// Calculate stats
	results.RequestsPerSec = float64(cfg.RequestCount) / heartbeatDuration.Seconds()

	fmt.Printf("\n=== Load Test Results ===\n")
	fmt.Printf("  Total Requests:     %d\n", cfg.RequestCount)
	fmt.Printf("  Successful:         %d\n", results.SuccessCount)
	fmt.Printf("  Failed:             %d\n", results.FailureCount)
	fmt.Printf("  Duration:           %v\n", heartbeatDuration)
	fmt.Printf("  Requests/sec:       %.2f\n", results.RequestsPerSec)
	fmt.Printf("  Fastest Request:    %v\n", results.FastestRequest)
	fmt.Printf("  Slowest Request:    %v\n", results.SlowestRequest)

	successRate := float64(results.SuccessCount) / float64(cfg.RequestCount) * 100
	fmt.Printf("  Success Rate:       %.2f%%\n", successRate)

	if successRate < 99.0 {
		log.Printf("WARNING: Success rate below 99%% target")
	}
}

func registerDevices(client *http.Client, cfg Config, count int) ([]string, int) {
	var deviceKeys []string
	registered := 0
	var mu sync.Mutex

	for i := 0; i < count; i++ {
		deviceKey := fmt.Sprintf("loadtest-device-%d", i)
		body := map[string]interface{}{
			"device_key": deviceKey,
			"name":       fmt.Sprintf("Load Test Device %d", i),
			"os":         "linux",
			"arch":       "amd64",
			"hostname":   fmt.Sprintf("loadtest-%d.local", i),
			"version":    "1.0.0",
		}
		bodyBytes, _ := json.Marshal(body)

		req, err := http.NewRequest("POST", cfg.ServerURL+"/api/agent/register", bytes.NewReader(bodyBytes))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode == 200 || resp.StatusCode == 201 {
			var result struct {
				DeviceKey string `json:"device_key"`
			}
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()

			mu.Lock()
			registered++
			deviceKeys = append(deviceKeys, result.DeviceKey)
			mu.Unlock()
		} else {
			resp.Body.Close()
		}
	}

	return deviceKeys, registered
}

func sendHeartbeats(client *http.Client, cfg Config, deviceKeys []string) *Results {
	count := cfg.RequestCount
	results := &Results{
		TotalRequests:  int64(count),
		FastestRequest: time.Hour,
		SlowestRequest: 0,
	}

	var wg sync.WaitGroup

	// Distribute requests across workers so exactly 'count' requests are
	// sent: each worker gets base, the first 'rem' workers get one extra.
	base := count / cfg.Concurrency
	rem := count % cfg.Concurrency

	offset := 0
	for w := 0; w < cfg.Concurrency; w++ {
		workerBatch := base
		if w < rem {
			workerBatch++
		}
		if workerBatch == 0 {
			continue
		}
		start := offset
		offset += workerBatch
		for r := 0; r < workerBatch; r++ {
			wg.Add(1)
			go func(start, rid int) {
				defer wg.Done()

					deviceIdx := (start + rid) % len(deviceKeys)
					deviceKey := deviceKeys[deviceIdx]
					body := map[string]interface{}{
						"device_key": deviceKey,
						"timestamp":  time.Now().Unix(),
					}
					bodyBytes, _ := json.Marshal(body)

					req, err := http.NewRequest("POST", cfg.ServerURL+"/api/agent/heartbeat", bytes.NewReader(bodyBytes))
					if err != nil {
						atomic.AddInt64(&results.FailureCount, 1)
						return
					}
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Device-Key", deviceKey)

					reqStart := time.Now()
					resp, err := client.Do(req)
					duration := time.Since(reqStart)
					if err != nil {
						atomic.AddInt64(&results.FailureCount, 1)
						return
					}
					defer resp.Body.Close()

					respBody, _ := io.ReadAll(resp.Body)
					atomic.AddInt64(&results.BytesReceived, int64(len(respBody)))
					atomic.AddInt64(&results.BytesSent, int64(len(bodyBytes)))

					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						atomic.AddInt64(&results.SuccessCount, 1)
					} else {
						atomic.AddInt64(&results.FailureCount, 1)
					}

					// Track timing
					if duration < results.FastestRequest {
						atomic.StoreInt64((*int64)(&results.FastestRequest), int64(duration))
					}
					if duration > results.SlowestRequest {
						atomic.StoreInt64((*int64)(&results.SlowestRequest), int64(duration))
					}
				}(start, r)
		}
	}

	wg.Wait()
	return results
}
