package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCPUCoresWithHotplugAndSparseIDs(t *testing.T) {
	first := cpuStatSample{cores: map[int]cpuTimes{
		0: {total: 100, idle: 60},
		3: {total: 200, idle: 100},
	}}
	second := cpuStatSample{cores: map[int]cpuTimes{
		0: {total: 200, idle: 110},
		2: {total: 400, idle: 300}, // appeared since the previous read
	}}
	cores := collectCPUCores(first, second, func(id int) float64 { return float64(id + 1) })
	if len(cores) != 2 || cores[0].CoreID != 0 || cores[1].CoreID != 2 {
		t.Fatalf("expected only active cores 0 and 2, got %+v", cores)
	}
	if cores[0].UsagePercent != 50 || cores[1].UsagePercent != 0 || cores[1].FrequencyMHz != 3 {
		t.Fatalf("unexpected core metrics after hotplug: %+v", cores)
	}
}

func TestReadCPUStatSparseIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stat")
	content := "cpu  100 0 20 80 0 0 0 0 0 0\n" +
		"cpu0 50 0 10 40 0 0 0 0 0 0\n" +
		"cpu3 50 0 10 40 0 0 0 0 0 0\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sample, err := readCPUStat(path)
	if err != nil || len(sample.cores) != 2 || sample.cores[3].total != 100 {
		t.Fatalf("unexpected sample: %+v, %v", sample, err)
	}
}

func TestReadMemInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	content := "MemTotal: 1024 kB\nMemAvailable: 256 kB\nMemFree: 64 kB\nBuffers: 32 kB\nCached: 128 kB\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := readMemInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.total != 1024*1024 || info.available != 256*1024 || info.cached != 128*1024 || info.buffers != 32*1024 {
		t.Fatalf("unexpected parsed memory: %+v", info)
	}
	if err := os.WriteFile(path, []byte("MemTotal: invalid kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMemInfo(path); err == nil {
		t.Fatal("expected parse error for invalid MemTotal")
	}
}

func TestMemoryAvailableZeroIsNotMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	for _, tc := range []struct {
		name          string
		content       string
		wantAvailable uint64
	}{
		{"zero", "MemTotal: 1024 kB\nMemAvailable: 0 kB\nMemFree: 64 kB\nBuffers: 32 kB\nCached: 128 kB\n", 0},
		{"missing", "MemTotal: 1024 kB\nMemFree: 64 kB\nBuffers: 32 kB\nCached: 128 kB\n", 224 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := readMemInfo(path)
			if err != nil {
				t.Fatal(err)
			}
			metrics, err := memoryMetrics(info)
			if err != nil {
				t.Fatal(err)
			}
			if metrics.AvailableBytes != tc.wantAvailable {
				t.Fatalf("available = %d, want %d", metrics.AvailableBytes, tc.wantAvailable)
			}
		})
	}
}
