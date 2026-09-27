package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const netDevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000    10    0    0    0     0          0         0     1000    10    0    0    0     0       0          0
  eth0: 1000    10    0    0    0     0          0         0      500     5    0    0    0     0       0          0
 wlan0:    0     0    0    0    0     0          0         0        0     0    0    0    0     0       0          0
`

const netRouteFixture = `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 0001A8C0 00000000 0001 0 0 0 00FFFFFF 0 0 0
eth0 00000000 0101A8C0 0003 0 0 0 00000000 0 0 0
`

const fibTrieFixture = `Main:
  +-- 0.0.0.0/0 3 0 4
     |-- 0.0.0.0
        /0 universe UNICAST
     +-- 192.168.1.0/24 2 0 2
        +-- 192.168.1.0/26 2 0 2
           |-- 192.168.1.0
              /24 link UNICAST
           |-- 192.168.1.42
              /32 host LOCAL
Local:
  +-- 192.168.1.42
     /32 host LOCAL
`

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", name, err)
	}
	return path
}

func TestReadNetDev(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "dev", netDevFixture)

	counters, err := readNetDev(path)
	if err != nil {
		t.Fatalf("readNetDev: %v", err)
	}

	if got := len(counters); got != 3 {
		t.Fatalf("expected 3 interfaces, got %d", got)
	}
	eth0 := counters["eth0"]
	if eth0.recvBytes != 1000 || eth0.sentBytes != 500 {
		t.Fatalf("unexpected eth0 counters: %+v", eth0)
	}
	lo := counters["lo"]
	if lo.recvBytes != 1000 || lo.sentBytes != 1000 {
		t.Fatalf("unexpected lo counters: %+v", lo)
	}
}

func TestReadNetDevMissingFile(t *testing.T) {
	if _, err := readNetDev(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected error for absent /proc/net/dev fixture")
	}
}

func TestReadConnectedNetworks(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "route", netRouteFixture)

	networks, err := readConnectedNetworks(path)
	if err != nil {
		t.Fatalf("readConnectedNetworks: %v", err)
	}

	subnets := networks["eth0"]
	if len(subnets) != 1 {
		t.Fatalf("expected 1 connected network for eth0, got %d", len(subnets))
	}
	if !subnets[0].Contains(littleEndianIPv4(0x2A01A8C0)) { // 192.168.1.42
		t.Fatalf("expected connected network to contain 192.168.1.42, got %v", subnets[0])
	}
}

func TestReadLocalIPv4(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "fib_trie", fibTrieFixture)

	locals, err := readLocalIPv4(path)
	if err != nil {
		t.Fatalf("readLocalIPv4: %v", err)
	}

	found := false
	for _, ip := range locals {
		if ip.String() == "192.168.1.42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 192.168.1.42 in local addresses, got %v", locals)
	}
}

func TestNetworkCollectorRates(t *testing.T) {
	dir := t.TempDir()
	devPath := writeFixture(t, dir, "dev", netDevFixture)
	routePath := writeFixture(t, dir, "route", netRouteFixture)
	fibPath := writeFixture(t, dir, "fib_trie", fibTrieFixture)

	base := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	current := base
	collector := &networkCollector{
		source: networkSource{devPath: devPath, routePath: routePath, fibTriePath: fibPath},
		now:    func() time.Time { return current },
	}

	first, err := collector.collect()
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected only eth0 active, got %d entries: %+v", len(first), first)
	}
	if first[0].InterfaceName != "eth0" || first[0].IPv4Address != "192.168.1.42" {
		t.Fatalf("unexpected interface resolution: %+v", first[0])
	}
	if first[0].BytesRecvSec != 0 || first[0].BytesSentSec != 0 {
		t.Fatalf("first sample must have zero rates, got %+v", first[0])
	}

	updated := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000    10    0    0    0     0          0         0     1000    10    0    0    0     0       0          0
  eth0: 5000    20    0    0    0     0          0         0     1500     8    0    0    0     0       0          0
 wlan0:    0     0    0    0    0     0          0         0        0     0    0    0    0     0       0          0
`
	if err := os.WriteFile(devPath, []byte(updated), 0o600); err != nil {
		t.Fatalf("update dev fixture: %v", err)
	}

	current = base.Add(2 * time.Second)
	second, err := collector.collect()
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 active interface, got %d", len(second))
	}
	if second[0].BytesRecvSec != 2000 || second[0].BytesSentSec != 500 {
		t.Fatalf("unexpected rates: %+v", second[0])
	}
}

func TestPerSecondRateWraparound(t *testing.T) {
	if got := perSecondRate(100, 50, 1); got != 0 {
		t.Fatalf("expected 0 on counter reset, got %d", got)
	}
	if got := perSecondRate(0, 100, 0); got != 0 {
		t.Fatalf("expected 0 on zero elapsed, got %d", got)
	}
	if got := perSecondRate(0, 1000, 4); got != 250 {
		t.Fatalf("expected 250, got %d", got)
	}
}

func TestNetworkCollectorWithoutRoute(t *testing.T) {
	dir := t.TempDir()
	collector := &networkCollector{
		source: networkSource{
			devPath:     writeFixture(t, dir, "dev", netDevFixture),
			routePath:   filepath.Join(dir, "missing-route"),
			fibTriePath: filepath.Join(dir, "missing-fib"),
		},
		now: func() time.Time { return time.Now() },
		lookupIPv4: func(name string) string {
			if name == "eth0" {
				return "192.168.1.42"
			}
			return ""
		},
	}

	metrics, err := collector.collect()
	if err != nil {
		t.Fatalf("collect without route: %v", err)
	}
	if len(metrics) != 1 || metrics[0].InterfaceName != "eth0" || metrics[0].IPv4Address != "192.168.1.42" {
		t.Fatalf("expected fallback IPv4 from interface lookup, got %+v", metrics)
	}
}

func TestNetworkCollectorWithEmptyRoute(t *testing.T) {
	dir := t.TempDir()
	collector := &networkCollector{
		source: networkSource{
			devPath:     writeFixture(t, dir, "dev", netDevFixture),
			routePath:   writeFixture(t, dir, "route", "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n"),
			fibTriePath: filepath.Join(dir, "missing-fib"),
		},
		now: time.Now,
		lookupIPv4: func(name string) string {
			if name == "eth0" {
				return "192.168.1.42"
			}
			return ""
		},
	}
	metrics, err := collector.collect()
	if err != nil || len(metrics) != 1 || metrics[0].IPv4Address != "192.168.1.42" {
		t.Fatalf("expected interface lookup on empty routes, got %+v, %v", metrics, err)
	}
}

func TestResolveInterfaceIPv4OverlappingNetworks(t *testing.T) {
	dir := t.TempDir()
	route := writeFixture(t, dir, "route", netRouteFixture+"wlan0 0001A8C0 00000000 0001 0 0 0 00FFFFFF 0 0 0\n")
	fib := writeFixture(t, dir, "fib_trie", fibTrieFixture)
	counters := map[string]netDevCounters{"eth0": {}, "wlan0": {}}
	addresses := resolveInterfaceIPv4(route, fib, counters, func(name string) string {
		if name == "eth0" {
			return "192.168.1.42"
		}
		if name == "wlan0" {
			return "192.168.1.43"
		}
		return ""
	})
	if addresses["eth0"] != "192.168.1.42" || addresses["wlan0"] != "192.168.1.43" {
		t.Fatalf("ambiguous fib_trie address attributed to wrong interface: %+v", addresses)
	}
}

func TestNetworkCollectorSerializesSampling(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	collector := &networkCollector{
		source: networkSource{routePath: filepath.Join(t.TempDir(), "missing-route")},
		now:    time.Now,
		readDev: func(string) (map[string]netDevCounters, error) {
			close(started)
			<-release
			return map[string]netDevCounters{}, nil
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := collector.collect()
		done <- err
	}()
	<-started
	if collector.mu.TryLock() {
		collector.mu.Unlock()
		close(release)
		<-done
		t.Fatal("sampling must hold the mutex before reading counters")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("collect: %v", err)
	}
}
