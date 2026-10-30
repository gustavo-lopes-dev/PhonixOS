package collector

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectCPUFallsBackInOrder(t *testing.T) {
	var calls []string
	failing := func(name string) cpuUsageSource {
		return cpuUsageSource{name: name, sample: func(func(int) float64) (float64, []CPUCoreMetrics, error) {
			calls = append(calls, name)
			return 0, nil, errors.New("permission denied")
		}}
	}
	working := cpuUsageSource{name: "cpuidle", sample: func(func(int) float64) (float64, []CPUCoreMetrics, error) {
		calls = append(calls, "cpuidle")
		return 40, []CPUCoreMetrics{{CoreID: 0, UsagePercent: 40}}, nil
	}}
	unused := cpuUsageSource{name: "unused", sample: func(func(int) float64) (float64, []CPUCoreMetrics, error) {
		calls = append(calls, "unused")
		return 0, nil, nil
	}}
	loads := []loadAvgSource{
		{name: "proc_loadavg", read: func() (float64, float64, float64, error) { return 0, 0, 0, errors.New("denied") }},
		{name: "sysinfo", read: func() (float64, float64, float64, error) { return 1.5, 1, 0.5, nil }},
	}

	cpu, err := collectCPUWith([]cpuUsageSource{failing("proc_stat"), working, unused}, loads, nil)
	if err != nil {
		t.Fatalf("collectCPUWith: %v", err)
	}
	if strings.Join(calls, ",") != "proc_stat,cpuidle" {
		t.Fatalf("unexpected source order: %v", calls)
	}
	if cpu.UsagePercent != 40 || cpu.CoreCount != 1 || cpu.LoadAvg1Min != 1.5 || cpu.LoadAvg15Min != 0.5 {
		t.Fatalf("unexpected cpu metrics: %+v", cpu)
	}
}

func TestCollectCPUFailsWhenEverySourceFails(t *testing.T) {
	usage := []cpuUsageSource{
		{name: "proc_stat", sample: func(func(int) float64) (float64, []CPUCoreMetrics, error) { return 0, nil, errors.New("a") }},
		{name: "cpuidle", sample: func(func(int) float64) (float64, []CPUCoreMetrics, error) { return 0, nil, errors.New("b") }},
	}
	_, err := collectCPUWith(usage, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "proc_stat: a") || !strings.Contains(err.Error(), "cpuidle: b") {
		t.Fatalf("expected joined errors from every source, got %v", err)
	}

	ok := []cpuUsageSource{{name: "x", sample: func(func(int) float64) (float64, []CPUCoreMetrics, error) { return 1, nil, nil }}}
	loads := []loadAvgSource{{name: "proc_loadavg", read: func() (float64, float64, float64, error) { return 0, 0, 0, errors.New("c") }}}
	if _, err := collectCPUWith(ok, loads, nil); err == nil || !strings.Contains(err.Error(), "proc_loadavg: c") {
		t.Fatalf("expected load average error, got %v", err)
	}
}

// fakeClock avança apenas quando o amostrador dorme ou o teste chama advance.
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time          { return c.at }
func (c *fakeClock) advance(d time.Duration) { c.at = c.at.Add(d) }
func (c *fakeClock) sleepHook(hook func()) func(time.Duration) {
	return func(d time.Duration) { c.advance(d); hook() }
}

func TestCPUIdleSamplerFirstCallWaitsMinimumWindow(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "online"), "0-1\n")
	set := func(core, state, value string) {
		writeTestFile(t, filepath.Join(root, "cpu"+core, "cpuidle", "state"+state, "time"), value+"\n")
	}
	set("0", "0", "1000000")
	set("0", "1", "2000000")
	set("1", "0", "500")
	set("2", "0", "0") // offline: never sampled
	clock := &fakeClock{at: time.Unix(100, 0)}
	var slept time.Duration
	sampler := &cpuidleSampler{root: root, now: clock.now, sleep: clock.sleepHook(func() {
		slept = time.Second
		set("0", "0", "1250000") // +250 ms
		set("0", "1", "2500000") // +500 ms -> 75% ocioso
		writeTestFile(t, filepath.Join(root, "online"), "0-2\n")
	})}

	total, cores, err := sampler.usage(func(id int) float64 { return float64(id) * 100 })
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if slept != time.Second {
		t.Fatalf("expected a single wait of the minimum window, slept %v", slept)
	}
	if len(cores) != 3 || cores[0].UsagePercent != 25 || cores[1].UsagePercent != 100 || cores[2].UsagePercent != 0 {
		t.Fatalf("unexpected cores: %+v", cores)
	}
	if cores[1].FrequencyMHz != 100 || math.Abs(total-125.0/3) > 1e-9 {
		t.Fatalf("unexpected aggregate %v or frequency: %+v", total, cores)
	}
}

func TestCPUIdleSamplerUsesTargetWindowBetweenCalls(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "online"), "0\n")
	idle := 0
	setIdle := func(ms int) {
		idle += ms * 1000
		writeTestFile(t, filepath.Join(root, "cpu0", "cpuidle", "state0", "time"), strconv.Itoa(idle))
	}
	setIdle(0)
	clock := &fakeClock{at: time.Unix(0, 0)}
	sampler := &cpuidleSampler{root: root, now: clock.now, sleep: clock.sleepHook(func() { setIdle(500) })}
	freq := func(int) float64 { return 0 }

	if total, _, err := sampler.usage(freq); err != nil || total != 50 { // t=0..1s, 500 ms ocioso
		t.Fatalf("first window: %v, %v", total, err)
	}
	// Núcleo em sono longo: o contador não avança por 2 s e depois credita tudo.
	clock.advance(time.Second)
	if total, _, err := sampler.usage(freq); err != nil || total != 75 { // base t=0: 500 ms ocioso em 2 s
		t.Fatalf("second window: %v, %v", total, err)
	}
	clock.advance(time.Second)
	setIdle(2500)
	if total, _, err := sampler.usage(freq); err != nil || total != 0 { // base t=0 (3 s): 3000 ms ocioso
		t.Fatalf("third window should use the 3 s base: %v, %v", total, err)
	}
	clock.advance(2 * time.Second)
	setIdle(1000)
	// t=5s: base mais recente com idade >= 3 s é t=2s (idle 500 ms) -> 3500 ms ocioso em 3 s, limitado a 0%.
	if total, _, err := sampler.usage(freq); err != nil || total != 0 {
		t.Fatalf("fourth window: %v, %v", total, err)
	}

	// Após um repouso maior que cpuidleMaxAge o histórico é descartado e a
	// coleta volta a esperar a janela mínima.
	clock.advance(cpuidleMaxAge + time.Second)
	if total, _, err := sampler.usage(freq); err != nil || total != 50 {
		t.Fatalf("after long pause: %v, %v", total, err)
	}
}

func TestCPUIdleUsageRequiresIdleStates(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "online"), "0\n")
	sampler := &cpuidleSampler{root: root, now: time.Now, sleep: func(time.Duration) {}}
	if _, _, err := sampler.usage(func(int) float64 { return 0 }); err == nil {
		t.Fatal("expected error without cpuidle states")
	}
}

func TestParseCPUList(t *testing.T) {
	ids, err := parseCPUList("0-2,4,6-7")
	if err != nil || len(ids) != 6 || ids[3] != 4 || ids[5] != 7 {
		t.Fatalf("unexpected ids %v, %v", ids, err)
	}
	for _, bad := range []string{"", "a", "3-1", "1-x"} {
		if _, err := parseCPUList(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestSysinfoLoadAvg(t *testing.T) {
	load1, load5, load15, err := sysinfoLoadAvg()
	if err != nil {
		t.Fatalf("sysinfoLoadAvg: %v", err)
	}
	if load1 < 0 || load5 < 0 || load15 < 0 {
		t.Fatalf("negative load average: %v %v %v", load1, load5, load15)
	}
}

func TestReadNetCountersFallsBackToSysfs(t *testing.T) {
	sys := t.TempDir()
	writeTestFile(t, filepath.Join(sys, "wlan0", "statistics", "rx_bytes"), "1000\n")
	writeTestFile(t, filepath.Join(sys, "wlan0", "statistics", "tx_bytes"), "200\n")
	writeTestFile(t, filepath.Join(sys, "dummy0", "statistics", "rx_bytes"), "denied")
	noNames := func() ([]string, error) {
		t.Fatal("names must not be used when the directory is listable")
		return nil, nil
	}

	counters, err := readNetCounters(filepath.Join(sys, "missing"), sys, noNames)
	if err != nil {
		t.Fatalf("readNetCounters: %v", err)
	}
	if len(counters) != 1 || counters["wlan0"] != (netDevCounters{recvBytes: 1000, sentBytes: 200}) {
		t.Fatalf("unexpected counters: %+v", counters)
	}
}

func TestReadSysfsNetCountersUsesInterfaceNamesWhenDirectoryIsDenied(t *testing.T) {
	sys := t.TempDir()
	writeTestFile(t, filepath.Join(sys, "wlan0", "statistics", "rx_bytes"), "7\n")
	writeTestFile(t, filepath.Join(sys, "wlan0", "statistics", "tx_bytes"), "9\n")
	names := func() ([]string, error) { return []string{"wlan0"}, nil }

	counters, err := readSysfsNetCounters(filepath.Join(sys, "unlistable"), names)
	if err == nil {
		t.Fatalf("expected error: names do not exist under the unlistable path, got %+v", counters)
	}
	// Mesmo diretório, mas a listagem é simulada como negada pelos nomes vindos do kernel.
	counters, err = readSysfsNetCounters(sys, names)
	if err != nil || counters["wlan0"].recvBytes != 7 {
		t.Fatalf("unexpected counters %+v, %v", counters, err)
	}
}

func TestReadNetCountersWithoutAnySource(t *testing.T) {
	dir := t.TempDir()
	names := func() ([]string, error) { return nil, errors.New("netlink and ioctl denied") }
	_, err := readNetCounters(filepath.Join(dir, "dev"), filepath.Join(dir, "net"), names)
	if err == nil || !strings.Contains(err.Error(), "netlink and ioctl denied") {
		t.Fatalf("expected error from every source, got %v", err)
	}
}

func TestFindExecutableWithoutLookPath(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "plain"), "data")
	writeTestFile(t, filepath.Join(dir, "tool"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(dir, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	pathEnv := filepath.Join(dir, "missing") + string(os.PathListSeparator) + dir

	if got, err := findExecutable("tool", pathEnv); err != nil || got != filepath.Join(dir, "tool") {
		t.Fatalf("findExecutable(tool) = %q, %v", got, err)
	}
	for _, name := range []string{"plain", "subdir", "absent"} {
		if _, err := findExecutable(name, pathEnv); !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("findExecutable(%s): expected ErrNotFound, got %v", name, err)
		}
	}
}

func TestCommandContextKillsHungDescendants(t *testing.T) {
	dir := t.TempDir()
	// Simula termux-battery-status sem o app: o filho herda stdout e nunca termina.
	writeTestFile(t, filepath.Join(dir, "hang"), "#!/bin/sh\nsleep 30 &\nwait\n")
	if err := os.Chmod(filepath.Join(dir, "hang"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd, err := commandContext(ctx, "hang")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := cmd.Output(); err == nil {
		t.Fatal("expected error from a cancelled command")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Output blocked for %v after cancellation", elapsed)
	}
}
