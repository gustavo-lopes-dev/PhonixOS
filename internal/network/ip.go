package network

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
)

// InterfaceIPv4 associa uma interface ativa a um de seus endereços IPv4.
type InterfaceIPv4 struct {
	Name     string
	IP       net.IP
	Loopback bool
}

// ipv4Source é um nível da cadeia de descoberta de endereços.
type ipv4Source struct {
	name string
	list func() ([]InterfaceIPv4, error)
}

// ipv4Sources define a ordem de fallback: netlink (stdlib) e, quando o kernel
// nega o socket netlink a apps comuns (Android 11+ sem root), o ioctl
// SIOCGIFCONF usado pelo ifconfig.
var ipv4Sources = []ipv4Source{
	{name: "netlink", list: netlinkIPv4},
	{name: "ioctl", list: ioctlIPv4},
}

// ListIPv4 devolve os IPv4 das interfaces ativas (inclusive loopback) usando a
// primeira fonte que responder sem erro.
func ListIPv4() ([]InterfaceIPv4, error) {
	return listIPv4With(ipv4Sources)
}

func listIPv4With(sources []ipv4Source) ([]InterfaceIPv4, error) {
	var errs []error
	for _, source := range sources {
		entries, err := source.list()
		if err == nil {
			return entries, nil
		}
		slog.Debug("network: ipv4 source failed, falling back", "source", source.name, "error", err)
		errs = append(errs, fmt.Errorf("%s: %w", source.name, err))
	}
	return nil, errors.Join(errs...)
}

// LocalIPv4 lista os endereços IPv4 literais de interfaces ativas para acesso LAN.
func LocalIPv4() ([]string, error) {
	entries, err := ListIPv4()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	seen := make(map[string]bool)
	addresses := []string{}
	for _, entry := range entries {
		ipv4 := entry.IP.To4()
		if entry.Loopback || ipv4 == nil || !ipv4.IsGlobalUnicast() || seen[ipv4.String()] {
			continue
		}
		addresses = append(addresses, ipv4.String())
		seen[ipv4.String()] = true
	}
	sort.Strings(addresses)
	return addresses, nil
}

// netlinkIPv4 enumera interfaces e endereços via stdlib (netlink no Linux).
func netlinkIPv4() ([]InterfaceIPv4, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	entries := []InterfaceIPv4{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
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
			if ipv4 := ip.To4(); ipv4 != nil {
				entries = append(entries, InterfaceIPv4{
					Name:     iface.Name,
					IP:       ipv4,
					Loopback: iface.Flags&net.FlagLoopback != 0,
				})
			}
		}
	}
	return entries, nil
}
