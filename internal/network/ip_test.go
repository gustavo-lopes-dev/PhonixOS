package network

import (
	"errors"
	"net"
	"testing"
)

func TestListIPv4FallsBackInOrder(t *testing.T) {
	var calls []string
	sources := []ipv4Source{
		{name: "netlink", list: func() ([]InterfaceIPv4, error) {
			calls = append(calls, "netlink")
			return nil, errors.New("netlinkrib: permission denied")
		}},
		{name: "ioctl", list: func() ([]InterfaceIPv4, error) {
			calls = append(calls, "ioctl")
			return []InterfaceIPv4{{Name: "wlan0", IP: net.IPv4(192, 168, 0, 9).To4()}}, nil
		}},
	}
	entries, err := listIPv4With(sources)
	if err != nil || len(entries) != 1 || entries[0].Name != "wlan0" || len(calls) != 2 {
		t.Fatalf("unexpected result %+v, %v, calls %v", entries, err, calls)
	}

	_, err = listIPv4With(sources[:1])
	if err == nil {
		t.Fatal("expected error when every source fails")
	}
}

func TestIoctlIPv4ListsLoopback(t *testing.T) {
	entries, err := ioctlIPv4()
	if err != nil {
		t.Fatalf("ioctlIPv4: %v", err)
	}
	for _, entry := range entries {
		if entry.IP.Equal(net.IPv4(127, 0, 0, 1)) {
			if !entry.Loopback || entry.Name == "" {
				t.Fatalf("loopback flags/name not decoded: %+v", entry)
			}
			return
		}
	}
	t.Fatalf("127.0.0.1 not reported by SIOCGIFCONF: %+v", entries)
}

func TestIoctlMatchesNetlink(t *testing.T) {
	viaNetlink, err := netlinkIPv4()
	if err != nil {
		t.Skipf("netlink unavailable: %v", err)
	}
	viaIoctl, err := ioctlIPv4()
	if err != nil {
		t.Fatalf("ioctlIPv4: %v", err)
	}
	want := map[string]bool{}
	for _, e := range viaNetlink {
		want[e.Name+"="+e.IP.String()] = true
	}
	for _, e := range viaIoctl {
		if !want[e.Name+"="+e.IP.String()] {
			t.Fatalf("ioctl entry %s=%s not reported by netlink %+v", e.Name, e.IP, viaNetlink)
		}
	}
}
