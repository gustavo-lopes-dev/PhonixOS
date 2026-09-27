package collector

import (
	"bufio"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	procNetDevPath     = "/proc/net/dev"
	procNetRoutePath   = "/proc/net/route"
	procNetFibTriePath = "/proc/net/fib_trie"

	// loopbackIface é desconsiderada na telemetria de throughput.
	loopbackIface = "lo"
)

// netDevCounters agrega os contadores acumulados de bytes de uma interface.
type netDevCounters struct {
	recvBytes uint64
	sentBytes uint64
}

// networkSource reúne os caminhos de /proc usados na coleta de rede. Existe
// para permitir a injeção de fixtures em testes.
type networkSource struct {
	devPath     string
	routePath   string
	fibTriePath string
}

// networkCollector mantém o estado entre coletas consecutivas para o cálculo
// das taxas por segundo. O acesso ao estado é serializado por mu.
type networkCollector struct {
	source     networkSource
	now        func() time.Time
	lookupIPv4 func(string) string
	readDev    func(string) (map[string]netDevCounters, error)

	mu     sync.Mutex
	prev   map[string]netDevCounters
	prevAt time.Time
}

// defaultNetworkCollector é a instância de produção, apoiada em /proc.
var defaultNetworkCollector = &networkCollector{
	source: networkSource{
		devPath:     procNetDevPath,
		routePath:   procNetRoutePath,
		fibTriePath: procNetFibTriePath,
	},
	now: time.Now,
}

// CollectNetwork devolve as taxas de rede por interface ativa, calculadas a
// partir do delta de bytes entre coletas consecutivas desta mesma instância.
// A primeira chamada reporta taxas zeradas por não haver amostra anterior.
func CollectNetwork() ([]NetworkMetrics, error) {
	return defaultNetworkCollector.collect()
}

// collect lê /proc/net/dev, resolve o IPv4 de cada interface com rota ativa e
// calcula bytes enviados/recebidos por segundo desde a coleta anterior.
func (c *networkCollector) collect() ([]NetworkMetrics, error) {
	// Serializa a amostragem inteira: duas chamadas não podem publicar amostras
	// em ordem diferente daquela em que leram os contadores.
	c.mu.Lock()
	defer c.mu.Unlock()

	read := c.readDev
	if read == nil {
		read = readNetDev
	}
	counters, err := read(c.source.devPath)
	if err != nil {
		return nil, err
	}

	lookup := c.lookupIPv4
	if lookup == nil {
		lookup = kernelInterfaceIPv4
	}
	addresses := resolveInterfaceIPv4(c.source.routePath, c.source.fibTriePath, counters, lookup)
	now := c.now()

	elapsed := now.Sub(c.prevAt).Seconds()
	metrics := make([]NetworkMetrics, 0, len(counters))
	for name, current := range counters {
		if name == loopbackIface {
			continue
		}
		ipv4, ok := addresses[name]
		if !ok || ipv4 == "" {
			continue
		}

		var sentRate, recvRate uint64
		if prev, found := c.prev[name]; found && elapsed > 0 {
			sentRate = perSecondRate(prev.sentBytes, current.sentBytes, elapsed)
			recvRate = perSecondRate(prev.recvBytes, current.recvBytes, elapsed)
		}

		metrics = append(metrics, NetworkMetrics{
			InterfaceName: name,
			IPv4Address:   ipv4,
			BytesSentSec:  sentRate,
			BytesRecvSec:  recvRate,
		})
	}
	c.prev = counters
	c.prevAt = now

	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].InterfaceName < metrics[j].InterfaceName
	})

	return metrics, nil
}

// perSecondRate calcula a taxa por segundo a partir dos contadores acumulados,
// tratando reset/wraparound do contador (curr < prev) como taxa zero.
func perSecondRate(prev, curr uint64, elapsed float64) uint64 {
	if elapsed <= 0 || curr < prev {
		return 0
	}
	return uint64(math.Round(float64(curr-prev) / elapsed))
}

// readNetDev faz o parse de /proc/net/dev. Linhas malformadas ou de cabeçalho
// são ignoradas sem abortar a leitura das demais interfaces.
func readNetDev(path string) (map[string]netDevCounters, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("collector: open %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Debug("collector: close net dev failed", "error", closeErr)
		}
	}()

	counters := make(map[string]netDevCounters)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, rest, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}

		recv, recvErr := strconv.ParseUint(fields[0], 10, 64)
		if recvErr != nil {
			slog.Debug("collector: parse net dev recv bytes failed", "interface", name, "error", recvErr)
			continue
		}
		sent, sentErr := strconv.ParseUint(fields[8], 10, 64)
		if sentErr != nil {
			slog.Debug("collector: parse net dev sent bytes failed", "interface", name, "error", sentErr)
			continue
		}

		counters[name] = netDevCounters{recvBytes: recv, sentBytes: sent}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("collector: scan %q: %w", path, scanErr)
	}
	return counters, nil
}

// resolveInterfaceIPv4 determina o IPv4 das interfaces com rota ativa. A
// resolução primária usa /proc/net/fib_trie (endereços locais) cruzado com as
// redes diretamente conectadas de /proc/net/route. Interfaces não resolvidas
// por /proc recorrem ao netlink via stdlib (Zero Root), cobrindo kernels que
// não expõem fib_trie.
func resolveInterfaceIPv4(routePath, fibTriePath string, counters map[string]netDevCounters, lookup func(string) string) map[string]string {
	addresses := make(map[string]string)

	networks, err := readConnectedNetworks(routePath)
	if err != nil {
		slog.Debug("collector: network route unavailable", "error", err)
	}
	if err != nil || len(networks) == 0 {
		// Sem rotas utilizáveis, consulta diretamente as interfaces observadas
		// em /proc/net/dev para evitar um snapshot de rede vazio.
		for iface := range counters {
			if iface != loopbackIface {
				if ip := lookup(iface); ip != "" {
					addresses[iface] = ip
				}
			}
		}
		return addresses
	}

	locals, err := readLocalIPv4(fibTriePath)
	if err != nil {
		slog.Debug("collector: network fib_trie unavailable", "error", err)
	}

	// fib_trie lista IPs locais sem identificar a interface. Uma sub-rede
	// compartilhada por interfaces diferentes não permite associação segura.
	candidates := make(map[string]string)
	for iface, subnets := range networks {
		if _, observed := counters[iface]; !observed || iface == loopbackIface {
			continue
		}
		for _, ip := range locals {
			for _, subnet := range subnets {
				if subnet.Contains(ip) {
					key := ip.String()
					if previous, exists := candidates[key]; !exists {
						candidates[key] = iface
					} else if previous != iface {
						candidates[key] = ""
					}
					break
				}
			}
		}
	}
	for _, ip := range locals {
		iface := candidates[ip.String()]
		if iface != "" && addresses[iface] == "" {
			addresses[iface] = ip.String()
		}
	}

	for iface := range networks {
		if _, observed := counters[iface]; !observed || iface == loopbackIface {
			continue
		}
		if addresses[iface] != "" {
			continue
		}
		if ip := lookup(iface); ip != "" {
			addresses[iface] = ip
		}
	}

	return addresses
}

// readConnectedNetworks extrai de /proc/net/route as redes diretamente
// conectadas (gateway nulo e máscara não nula) por interface. Os campos de
// endereço são hexadecimais em ordem de bytes little-endian.
func readConnectedNetworks(path string) (map[string][]net.IPNet, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("collector: open %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Debug("collector: close net route failed", "error", closeErr)
		}
	}()

	networks := make(map[string][]net.IPNet)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 {
			continue
		}

		destination, destOK := parseHexIPv4(fields[1])
		gateway, gwOK := parseHexIPv4(fields[2])
		mask, maskOK := parseHexIPv4(fields[7])
		if !destOK || !gwOK || !maskOK {
			continue
		}
		if gateway != 0 || mask == 0 {
			continue
		}

		networks[fields[0]] = append(networks[fields[0]], net.IPNet{
			IP:   littleEndianIPv4(destination),
			Mask: net.IPMask(littleEndianIPv4(mask)),
		})
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("collector: scan %q: %w", path, scanErr)
	}
	return networks, nil
}

// readLocalIPv4 extrai de /proc/net/fib_trie os endereços locais de host
// (linhas "host LOCAL"), associando cada endereço à sua qualificação.
func readLocalIPv4(path string) ([]net.IP, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("collector: open %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Debug("collector: close fib trie failed", "error", closeErr)
		}
	}()

	var addresses []net.IP
	var pending net.IP
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "|-- ") || strings.HasPrefix(line, "+-- ") {
			if parsed := net.ParseIP(strings.TrimSpace(line[4:])); parsed != nil {
				pending = parsed.To4()
			} else {
				pending = nil
			}
			continue
		}

		if pending != nil && strings.HasPrefix(line, "/") &&
			strings.Contains(line, "host") && strings.Contains(line, "LOCAL") {
			addresses = append(addresses, pending)
			pending = nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("collector: scan %q: %w", path, scanErr)
	}
	return addresses, nil
}

// parseHexIPv4 converte um campo hexadecimal de /proc/net/route em um inteiro
// de 32 bits em ordem little-endian.
func parseHexIPv4(value string) (uint32, bool) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}

// littleEndianIPv4 converte o inteiro little-endian de /proc/net/route para o
// formato de endereço de rede do Go.
func littleEndianIPv4(value uint32) net.IP {
	return net.IPv4(byte(value), byte(value>>8), byte(value>>16), byte(value>>24)).To4()
}

// kernelInterfaceIPv4 resolve o IPv4 de uma interface via netlink (stdlib),
// sem privilégios. É o fallback para hosts sem /proc/net/fib_trie exposto.
func kernelInterfaceIPv4(name string) string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		slog.Debug("collector: interface lookup failed", "interface", name, "error", err)
		return ""
	}
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return ""
	}

	addrs, err := iface.Addrs()
	if err != nil {
		slog.Debug("collector: interface address lookup failed", "interface", name, "error", err)
		return ""
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipv4 := ipNet.IP.To4(); ipv4 != nil {
			return ipv4.String()
		}
	}
	return ""
}
