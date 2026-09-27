package collector

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestCollectHardwareSnapshot(t *testing.T) {
	if _, err := os.Stat(procStatPath); err != nil {
		t.Skipf("skipping: %q unavailable: %v", procStatPath, err)
	}

	snapshot, err := CollectHardware()
	if err != nil {
		t.Fatalf("CollectHardware: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected non-nil snapshot")
	}

	if got := snapshot.Timestamp.Location(); got != time.UTC {
		t.Fatalf("expected UTC timestamp, got %v", got)
	}
	if snapshot.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
	if snapshot.Battery.Source == "" {
		t.Fatalf("expected battery source to be populated: %+v", snapshot.Battery)
	}
	if snapshot.CPU.CoreCount <= 0 {
		t.Fatalf("expected at least one CPU core, got %d", snapshot.CPU.CoreCount)
	}
}

func TestCollectHardwareWithUnavailableNetwork(t *testing.T) {
	if _, err := os.Stat(procStatPath); err != nil {
		t.Skipf("skipping: %q unavailable: %v", procStatPath, err)
	}

	snapshot, err := collectHardwareWithNetwork(func() ([]NetworkMetrics, error) {
		return nil, errors.New("/proc/net/dev unavailable")
	})
	if err != nil {
		t.Fatalf("partial snapshot: %v", err)
	}
	if snapshot.CPU.CoreCount == 0 || snapshot.Memory.TotalBytes == 0 || snapshot.Battery.Source == "" || snapshot.Network == nil || len(snapshot.Network) != 0 {
		t.Fatalf("unexpected partial snapshot: %+v", snapshot)
	}
}
