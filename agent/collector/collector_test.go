package collector

import (
	"testing"
)

func TestCPUCollector(t *testing.T) {
	t.Run("returns valid CPU percent in range 0-100", func(t *testing.T) {
		c := NewCPUCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("CPU collect failed: %v", err)
		}

		percent, ok := data["percent"].(float64)
		if !ok {
			t.Fatal("expected 'percent' field to be float64")
		}

		if percent < 0 || percent > 100 {
			t.Errorf("CPU percent %.2f out of range [0, 100]", percent)
		}

		t.Logf("CPU usage: %.2f%%", percent)
	})

	t.Run("per-core data has at least one core", func(t *testing.T) {
		c := NewCPUCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("CPU collect failed: %v", err)
		}

		perCore, ok := data["per_core"].([]float64)
		if !ok {
			t.Fatal("expected 'per_core' field to be []float64")
		}

		if len(perCore) == 0 {
			t.Error("expected at least one core in per_core data")
		}

		for i, p := range perCore {
			if p < 0 || p > 100 {
				t.Errorf("core %d CPU percent %.2f out of range [0, 100]", i, p)
			}
		}

		t.Logf("Per-core CPU: %v", perCore)
	})

	t.Run("collector name is 'cpu'", func(t *testing.T) {
		c := NewCPUCollector()
		if c.Name() != "cpu" {
			t.Errorf("expected name 'cpu', got %q", c.Name())
		}
	})
}

func TestMemoryCollector(t *testing.T) {
	t.Run("returns valid memory percent in range 0-100", func(t *testing.T) {
		c := NewMemoryCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("memory collect failed: %v", err)
		}

		percent, ok := data["percent"].(float64)
		if !ok {
			t.Fatal("expected 'percent' field to be float64")
		}

		if percent < 0 || percent > 100 {
			t.Errorf("memory percent %.2f out of range [0, 100]", percent)
		}

		t.Logf("Memory usage: %.2f%%", percent)
	})

	t.Run("returns non-zero total memory", func(t *testing.T) {
		c := NewMemoryCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("memory collect failed: %v", err)
		}

		total, ok := data["total"].(uint64)
		if !ok {
			t.Fatal("expected 'total' field to be uint64")
		}

		if total == 0 {
			t.Error("expected non-zero total memory")
		}

		t.Logf("Total memory: %d bytes", total)
	})

	t.Run("used memory is less than or equal to total", func(t *testing.T) {
		c := NewMemoryCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("memory collect failed: %v", err)
		}

		used, ok := data["used"].(uint64)
		if !ok {
			t.Fatal("expected 'used' field to be uint64")
		}

		total, ok := data["total"].(uint64)
		if !ok {
			t.Fatal("expected 'total' field to be uint64")
		}

		if used > total {
			t.Errorf("used memory (%d) exceeds total (%d)", used, total)
		}
	})

	t.Run("collector name is 'memory'", func(t *testing.T) {
		c := NewMemoryCollector()
		if c.Name() != "memory" {
			t.Errorf("expected name 'memory', got %q", c.Name())
		}
	})
}

func TestDiskCollector(t *testing.T) {
	t.Run("returns at least one partition", func(t *testing.T) {
		c := NewDiskCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("disk collect failed: %v", err)
		}

		partitions, ok := data["partitions"].([]DiskInfo)
		if !ok {
			t.Fatal("expected 'partitions' field to be []DiskInfo")
		}

		if len(partitions) == 0 {
			t.Error("expected at least one disk partition")
		}

		t.Logf("Found %d partitions", len(partitions))
	})

	t.Run("partition usage percent is valid", func(t *testing.T) {
		c := NewDiskCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("disk collect failed: %v", err)
		}

		partitions, ok := data["partitions"].([]DiskInfo)
		if !ok {
			t.Fatal("expected 'partitions' field to be []DiskInfo")
		}

		for i, p := range partitions {
			if p.UsagePercent < 0 || p.UsagePercent > 100 {
				t.Errorf("partition %d (%s) usage percent %.2f out of range", i, p.MountPoint, p.UsagePercent)
			}
			if p.Total == 0 {
				t.Errorf("partition %d (%s) has zero total space", i, p.MountPoint)
			}
			if p.Used > p.Total {
				t.Errorf("partition %d (%s) used (%d) exceeds total (%d)", i, p.MountPoint, p.Used, p.Total)
			}
		}
	})

	t.Run("collector name is 'disk'", func(t *testing.T) {
		c := NewDiskCollector()
		if c.Name() != "disk" {
			t.Errorf("expected name 'disk', got %q", c.Name())
		}
	})
}

func TestNetworkCollector(t *testing.T) {
	t.Run("returns interface data", func(t *testing.T) {
		c := NewNetworkCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("network collect failed: %v", err)
		}

		interfaces, ok := data["interfaces"].(map[string]NetworkInfo)
		if !ok {
			t.Fatal("expected 'interfaces' field to be map[string]NetworkInfo")
		}

		if len(interfaces) == 0 {
			t.Error("expected at least one network interface")
		}

		t.Logf("Found %d interfaces", len(interfaces))
	})

	t.Run("interface stats are non-negative", func(t *testing.T) {
		c := NewNetworkCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("network collect failed: %v", err)
		}

		interfaces, ok := data["interfaces"].(map[string]NetworkInfo)
		if !ok {
			t.Fatal("expected 'interfaces' field to be map[string]NetworkInfo")
		}

		for name, iface := range interfaces {
			if iface.Name != name {
				t.Errorf("interface %s has wrong Name field: %s", name, iface.Name)
			}
			if iface.BytesSent == 0 && iface.BytesRecv == 0 {
				// Some interfaces may never have traffic; just log
				t.Logf("Interface %s has no traffic", name)
			}
		}
	})

	t.Run("collector name is 'network'", func(t *testing.T) {
		c := NewNetworkCollector()
		if c.Name() != "network" {
			t.Errorf("expected name 'network', got %q", c.Name())
		}
	})
}

func TestProcessCollector(t *testing.T) {
	t.Run("returns non-empty process list", func(t *testing.T) {
		c := NewProcessCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("process collect failed: %v", err)
		}

		topCPU, ok := data["top_cpu"].([]ProcessInfo)
		if !ok {
			t.Fatal("expected 'top_cpu' field to be []ProcessInfo")
		}

		if len(topCPU) == 0 {
			t.Error("expected at least one top CPU process")
		}

		count, ok := data["count"].(int)
		if !ok {
			t.Fatal("expected 'count' field to be int")
		}

		if count == 0 {
			t.Error("expected non-zero process count")
		}

		t.Logf("Found %d processes, top CPU: %d listed", count, len(topCPU))
	})

	t.Run("top CPU processes are sorted", func(t *testing.T) {
		c := NewProcessCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("process collect failed: %v", err)
		}

		topCPU, ok := data["top_cpu"].([]ProcessInfo)
		if !ok {
			t.Fatal("expected 'top_cpu' field to be []ProcessInfo")
		}

		for i := 1; i < len(topCPU); i++ {
			if topCPU[i].CPU > topCPU[i-1].CPU {
				t.Errorf("top CPU list not sorted: process %d (%s: %.1f%%) > %d (%s: %.1f%%)",
					i-1, topCPU[i-1].Name, topCPU[i-1].CPU,
					i, topCPU[i].Name, topCPU[i].CPU)
			}
		}
	})

	t.Run("top memory processes are sorted", func(t *testing.T) {
		c := NewProcessCollector()

		data, err := c.Collect()
		if err != nil {
			t.Fatalf("process collect failed: %v", err)
		}

		topMem, ok := data["top_mem"].([]ProcessInfo)
		if !ok {
			t.Fatal("expected 'top_mem' field to be []ProcessInfo")
		}

		for i := 1; i < len(topMem); i++ {
			if topMem[i].Memory > topMem[i-1].Memory {
				t.Errorf("top memory list not sorted: process %d (%s: %d) > %d (%s: %d)",
					i-1, topMem[i-1].Name, topMem[i-1].Memory,
					i, topMem[i].Name, topMem[i].Memory)
			}
		}
	})

	t.Run("collector name is 'processes'", func(t *testing.T) {
		c := NewProcessCollector()
		if c.Name() != "processes" {
			t.Errorf("expected name 'processes', got %q", c.Name())
		}
	})
}

func TestCollectorManager(t *testing.T) {
	t.Run("CollectAll returns valid metrics", func(t *testing.T) {
		cm := NewCollectorManager()

		metrics, err := cm.CollectAll()
		if err != nil {
			t.Fatalf("CollectAll failed: %v", err)
		}

		// Validate CPU
		if metrics.CPU < 0 || metrics.CPU > 100 {
			t.Errorf("CPU %.2f out of range", metrics.CPU)
		}

		// Validate RAM
		if metrics.RAM < 0 || metrics.RAM > 100 {
			t.Errorf("RAM %.2f out of range", metrics.RAM)
		}

		// Validate disks
		if len(metrics.Disks) == 0 {
			t.Error("expected at least one disk partition")
		}

		// Validate network interfaces
		if len(metrics.Network) == 0 {
			t.Error("expected at least one network interface")
		}

		// Validate uptime is non-zero (system should be running)
		if metrics.Uptime == 0 {
			t.Error("expected non-zero uptime")
		}

		t.Logf("Metrics: CPU=%.1f%% RAM=%.1f%% Disks=%d Networks=%d Uptime=%ds",
			metrics.CPU, metrics.RAM, len(metrics.Disks), len(metrics.Network), metrics.Uptime)
	})
}

func TestUptime(t *testing.T) {
	uptime, err := Uptime()
	if err != nil {
		t.Fatalf("Uptime failed: %v", err)
	}
	if uptime == 0 {
		t.Error("expected non-zero uptime")
	}
	t.Logf("System uptime: %d seconds", uptime)
}

func TestLoadAvg(t *testing.T) {
	load, err := LoadAvg()
	if err != nil {
		t.Fatalf("LoadAvg failed: %v", err)
	}
	if len(load) != 3 {
		t.Errorf("expected 3 load averages, got %d", len(load))
	}
	for i, l := range load {
		if l < 0 {
			t.Errorf("load average %d is negative: %.2f", i, l)
		}
	}
	t.Logf("Load averages: 1m=%.2f 5m=%.2f 15m=%.2f", load[0], load[1], load[2])
}
