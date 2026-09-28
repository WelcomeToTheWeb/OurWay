package collector

import (
	"sort"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// ProcessCollector collects top process metrics.
type ProcessCollector struct {
	topN     int
	interval time.Duration
	// handles keeps one *process.Process per PID across Collect() calls.
	// gopsutil computes CPUPercent as a delta against the previous sample
	// stored on the handle, so a fresh handle every cycle would always
	// report 0% CPU.
	handles map[int32]*process.Process
}

// NewProcessCollector creates a new process collector.
func NewProcessCollector() *ProcessCollector {
	return &ProcessCollector{
		topN:     10,
		interval: 500 * time.Millisecond,
		handles:  make(map[int32]*process.Process),
	}
}

// Name returns the collector name.
func (c *ProcessCollector) Name() string {
	return "processes"
}

// Collect gathers top processes by CPU and memory usage.
func (c *ProcessCollector) Collect() (map[string]interface{}, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	// First pass: collect basic info for all processes
	type procInfo struct {
		p      *process.Process
		cpu    float64
		memory uint64
	}

	var infos []procInfo
	seen := make(map[int32]bool, len(procs))
	for _, p := range procs {
		if p == nil {
			continue
		}
		seen[p.Pid] = true
		// Reuse the previous cycle's handle when available so that
		// CPUPercent() has a prior sample to diff against.
		if prev, ok := c.handles[p.Pid]; ok {
			p = prev
		} else {
			c.handles[p.Pid] = p
		}

		cpuPercent, err := p.CPUPercent()
		if err != nil {
			cpuPercent = 0
		}

		memInfo, err := p.MemoryInfo()
		var memUsed uint64
		if err == nil && memInfo != nil {
			memUsed = memInfo.RSS
		}

			infos = append(infos, procInfo{
				p:      p,
				cpu:    cpuPercent,
				memory: memUsed,
			})
	}

	// Drop handles for processes that have exited.
	for pid := range c.handles {
		if !seen[pid] {
			delete(c.handles, pid)
		}
	}

	// Top by CPU
	topCPU := make([]ProcessInfo, 0, c.topN)
	byCPU := make([]procInfo, len(infos))
	copy(byCPU, infos)
	sort.Slice(byCPU, func(i, j int) bool {
		return byCPU[i].cpu > byCPU[j].cpu
	})
	for i, info := range byCPU {
		if i >= c.topN {
			break
		}
		name, _ := info.p.Name()
		topCPU = append(topCPU, ProcessInfo{
			PID:    info.p.Pid,
			Name:   name,
			CPU:    info.cpu,
			Memory: info.memory,
		})
	}

	// Top by Memory
	topMem := make([]ProcessInfo, 0, c.topN)
	byMem := make([]procInfo, len(infos))
	copy(byMem, infos)
	sort.Slice(byMem, func(i, j int) bool {
		return byMem[i].memory > byMem[j].memory
	})
	for i, info := range byMem {
		if i >= c.topN {
			break
		}
		name, _ := info.p.Name()
		topMem = append(topMem, ProcessInfo{
			PID:    info.p.Pid,
			Name:   name,
			CPU:    info.cpu,
			Memory: info.memory,
		})
	}

	return map[string]interface{}{
		"top_cpu":  topCPU,
		"top_mem":  topMem,
		"count":    len(procs),
	}, nil
}
