package androidhelper

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInterfaceCountersUnavailableOutsideAndroid(t *testing.T) {
	if _, err := os.Stat("/system/bin/app_process"); err == nil {
		t.Skip("running on Android")
	}
	client := &Client{dir: t.TempDir()}
	_, err := client.InterfaceCounters([]string{"wlan0"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	// Indisponibilidade não é falha: nenhuma suspensão por backoff.
	if !client.failedUntil.IsZero() {
		t.Fatal("unavailable environment must not trigger backoff")
	}
}

func TestInterfaceCountersRejectsUnsafeNames(t *testing.T) {
	client := &Client{dir: t.TempDir()}
	if _, err := client.InterfaceCounters([]string{"bad name", `x"y`, ""}); err == nil {
		t.Fatal("expected error when no interface name is valid")
	}
}

func TestWriteDexIsReadOnlyAndReused(t *testing.T) {
	client := &Client{dir: t.TempDir()}
	path, err := client.writeDex([]byte("dex\n035"))
	if err != nil {
		t.Fatalf("writeDex: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o444 || filepath.Dir(path) != client.dir {
		t.Fatalf("unexpected dex file %q: %v %v", path, info, err)
	}
	again, err := client.writeDex([]byte("dex\n035"))
	if err != nil || again != path {
		t.Fatalf("expected reuse of %q, got %q, %v", path, again, err)
	}
}

func TestFilteredEnvDropsPreload(t *testing.T) {
	t.Setenv("LD_PRELOAD", "/data/data/com.termux/files/usr/lib/libtermux-exec.so")
	t.Setenv("CLASSPATH", "/tmp/other.dex")
	for _, kv := range filteredEnv() {
		if len(kv) >= 11 && (kv[:11] == "LD_PRELOAD=" || kv[:10] == "CLASSPATH=") {
			t.Fatalf("variable not filtered: %s", kv)
		}
	}
}

func TestInterfaceCountersOnAndroid(t *testing.T) {
	if _, err := os.Stat("/system/bin/app_process"); err != nil {
		t.Skip("not running on Android")
	}
	if _, err := dex.ReadFile("dex/" + dexName); err != nil {
		t.Skip("helper dex not embedded in this build")
	}
	client := &Client{dir: t.TempDir()}
	defer client.Stop()

	first, err := client.InterfaceCounters([]string{"wlan0", "lo"})
	if err != nil {
		t.Fatalf("InterfaceCounters: %v", err)
	}
	if _, ok := first.Ifaces["wlan0"]; !ok || first.IfaceSource == "" {
		t.Fatalf("expected wlan0 counters with a source, got %+v", first)
	}
	if first.Total == nil || first.TotalSource == "" {
		t.Fatalf("expected device total with a source, got %+v", first)
	}
	t.Logf("strategies %q; iface source %s, total source %s", client.proc.strategies, first.IfaceSource, first.TotalSource)
	pid := client.proc.cmd.Process.Pid
	second, err := client.InterfaceCounters([]string{"wlan0"})
	if err != nil {
		t.Fatalf("second query: %v", err)
	}
	if client.proc.cmd.Process.Pid != pid {
		t.Fatal("helper must be reused between queries")
	}
	if second.Ifaces["wlan0"].RecvBytes < first.Ifaces["wlan0"].RecvBytes {
		t.Fatalf("counters went backwards: %+v -> %+v", first, second)
	}
	client.Stop()
	if client.proc != nil {
		t.Fatal("Stop must clear the process")
	}
}

func TestParseResponse(t *testing.T) {
	result, err := parseResponse(`{"ifaces":{"wlan0":[10,20],"bad":[-1,5]},"iface_source":"netstats_iface","total":[30,40],"total_source":"netstats_total"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ifaces) != 1 || result.Ifaces["wlan0"].SentBytes != 20 || result.IfaceSource != "netstats_iface" ||
		result.Total == nil || result.Total.RecvBytes != 30 || result.TotalSource != "netstats_total" {
		t.Fatalf("unexpected result %+v", result)
	}
	result, err = parseResponse(`{"ifaces":{},"total":[-1,-1],"total_source":"trafficstats_total"}`)
	if err != nil || result.Total != nil || len(result.Ifaces) != 0 {
		t.Fatalf("negative totals must be dropped: %+v, %v", result, err)
	}
	if _, err := parseResponse("not json"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
