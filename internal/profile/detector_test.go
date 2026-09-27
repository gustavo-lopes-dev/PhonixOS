package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyProfileThreshold(t *testing.T) {
	tests := []struct {
		name             string
		totalMemoryBytes uint64
		wantProfile      ProfileType
		wantIsLite       bool
		wantPollInterval int
	}{
		{
			name:             "just below 2GB is lite",
			totalMemoryBytes: liteMemoryThresholdBytes - 1,
			wantProfile:      ProfileLite,
			wantIsLite:       true,
			wantPollInterval: 5,
		},
		{
			name:             "exactly 2GB is performance",
			totalMemoryBytes: liteMemoryThresholdBytes,
			wantProfile:      ProfilePerformance,
			wantIsLite:       false,
			wantPollInterval: 2,
		},
		{
			name:             "above 2GB is performance",
			totalMemoryBytes: liteMemoryThresholdBytes + 1,
			wantProfile:      ProfilePerformance,
			wantIsLite:       false,
			wantPollInterval: 2,
		},
		{
			name:             "zero memory is lite",
			totalMemoryBytes: 0,
			wantProfile:      ProfileLite,
			wantIsLite:       true,
			wantPollInterval: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyProfile(tt.totalMemoryBytes, "wal")

			if got.Profile != tt.wantProfile {
				t.Errorf("Profile = %q, want %q", got.Profile, tt.wantProfile)
			}
			if got.IsLite != tt.wantIsLite {
				t.Errorf("IsLite = %v, want %v", got.IsLite, tt.wantIsLite)
			}
			if got.RecommendedPollIntervalS != tt.wantPollInterval {
				t.Errorf("RecommendedPollIntervalS = %d, want %d", got.RecommendedPollIntervalS, tt.wantPollInterval)
			}
			if got.TotalMemoryBytes != tt.totalMemoryBytes {
				t.Errorf("TotalMemoryBytes = %d, want %d", got.TotalMemoryBytes, tt.totalMemoryBytes)
			}
			if got.StorageLockMode != "wal" {
				t.Errorf("StorageLockMode = %q, want %q", got.StorageLockMode, "wal")
			}
		})
	}
}

func TestParseMemTotal(t *testing.T) {
	const sample = "MemTotal:        1987547 kB\n" +
		"MemFree:          943718 kB\n" +
		"MemAvailable:     943718 kB\n"

	got, err := parseMemTotal(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("parseMemTotal returned error: %v", err)
	}

	const want = uint64(1987547) * 1024
	if got != want {
		t.Errorf("total = %d, want %d", got, want)
	}
}

func TestParseMemTotalMissing(t *testing.T) {
	if _, err := parseMemTotal(strings.NewReader("MemFree: 1 kB\n")); err == nil {
		t.Fatal("parseMemTotal expected error when MemTotal is absent")
	}
}

func TestTotalMemoryBytesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	content := []byte("MemTotal:        1048576 kB\nMemFree: 1 kB\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write temp meminfo: %v", err)
	}

	got, err := totalMemoryBytes(path)
	if err != nil {
		t.Fatalf("totalMemoryBytes returned error: %v", err)
	}

	const want = uint64(1048576) * 1024
	if got != want {
		t.Errorf("total = %d, want %d", got, want)
	}
}

func TestDetectProfile(t *testing.T) {
	if _, err := os.Stat(memInfoPath); err != nil {
		t.Skipf("%s unavailable: %v", memInfoPath, err)
	}

	got, err := DetectProfile("truncate")
	if err != nil {
		t.Fatalf("DetectProfile returned error: %v", err)
	}

	if got.Profile != ProfileLite && got.Profile != ProfilePerformance {
		t.Errorf("unexpected profile %q", got.Profile)
	}
	if got.IsLite != (got.Profile == ProfileLite) {
		t.Errorf("IsLite = %v inconsistent with profile %q", got.IsLite, got.Profile)
	}
	if got.RecommendedPollIntervalS != litePollIntervalS && got.RecommendedPollIntervalS != performancePollIntervalS {
		t.Errorf("unexpected poll interval %d", got.RecommendedPollIntervalS)
	}
	if got.StorageLockMode != "truncate" {
		t.Errorf("StorageLockMode = %q, want %q", got.StorageLockMode, "truncate")
	}
}
