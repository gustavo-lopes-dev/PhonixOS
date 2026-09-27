package collector

import (
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
