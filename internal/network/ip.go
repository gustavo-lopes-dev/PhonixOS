package network

import (
	"fmt"
	"net"
	"sort"
)

// LocalIPv4 lista os endereços IPv4 literais de interfaces ativas para acesso LAN.
func LocalIPv4() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	seen := make(map[string]bool)
	addresses := []string{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses for %s: %w", iface.Name, err)
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ipv4 := ip.To4()
			if ipv4 == nil || !ipv4.IsGlobalUnicast() || seen[ipv4.String()] {
				continue
			}
			addresses = append(addresses, ipv4.String())
			seen[ipv4.String()] = true
		}
	}
	sort.Strings(addresses)
	return addresses, nil
}
