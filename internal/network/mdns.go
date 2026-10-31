package network

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Nomes publicados: o hostname (A/AAAA) é distinto da instância DNS-SD, que
// apenas aponta para ele via SRV (guidelines §3.3).
const (
	mdnsHostname     = "phonix.local."
	mdnsService      = "_http._tcp.local."
	mdnsInstanceName = "phonix." + mdnsService
	mdnsServiceEnum  = "_services._dns-sd._udp.local."
	mdnsTXTPath      = "path=/"
)

// Parâmetros do RFC 6762.
const (
	mdnsPort            = 5353
	mdnsHostTTL         = 120
	mdnsServiceTTL      = 4500
	mdnsLegacyMaxTTL    = 10
	mdnsClassTopBit     = 1 << 15 // cache-flush em respostas, QU em perguntas
	mdnsProbeCount      = 3
	mdnsProbeInterval   = 250 * time.Millisecond
	mdnsAnnounceDelay   = time.Second
	mdnsRefreshInterval = 30 * time.Second
	mdnsRetiredTTL      = 2 * mdnsHostTTL * time.Second
	mdnsMaxPacket       = 9000
)

// Ioctls e flags do Linux para o fallback sem netlink (valores idênticos em
// todas as arquiteturas suportadas).
const (
	linuxSIOCGIFFLAGS = 0x8913
	linuxSIOCGIFINDEX = 0x8933
	linuxIFFRunning   = 0x40
	linuxIFFMulticast = 0x1000
	linuxIfreqSize    = 40
)

var (
	mdnsGroupIPv4 = net.IPv4(224, 0, 0, 251)
	mdnsGroupIPv6 = net.ParseIP("ff02::fb")

	// ErrMDNSNameConflict indica que outro host já responde por phonix.local
	// ou pela instância DNS-SD com dados diferentes; o nome não é anunciado.
	ErrMDNSNameConflict = errors.New("mdns: phonix.local is already claimed by another host on the network")
)

// RegisterMDNS publica phonix.local (A/AAAA das interfaces LAN ativas) e a
// instância DNS-SD phonix._http._tcp.local. apontando para port. Bloqueia
// durante a sondagem de conflito (~1 s) e devolve erro se nenhum socket UDP
// 5353 puder ser aberto e associado ao grupo multicast, ou se o nome já
// pertencer a outro host. O shutdown retira o anúncio (TTL 0), fecha os sockets
// e aguarda as goroutines; é idempotente. Sucesso local não garante resolução
// nos clientes: redes que bloqueiam multicast exigem o acesso por IP literal.
func RegisterMDNS(port int) (shutdown func(), err error) {
	return registerMDNS(port, mdnsInterfaceSources, mdnsFamilies)
}

// mdnsInterface é uma interface LAN multicast com endereços publicáveis.
type mdnsInterface struct {
	index   int
	name    string
	ipv4    []net.IP
	ipv6    []net.IP // global/ULA, publicados em AAAA
	hasIPv6 bool     // qualquer IPv6 (inclusive link-local) permite o transporte IPv6
}

func (i *mdnsInterface) add(ip net.IP) {
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.IsGlobalUnicast() && !containsIP(i.ipv4, ip4) {
			i.ipv4 = append(i.ipv4, ip4)
		}
		return
	}
	if ip.To16() == nil {
		return
	}
	i.hasIPv6 = true
	if ip.IsGlobalUnicast() && !containsIP(i.ipv6, ip) {
		i.ipv6 = append(i.ipv6, ip)
	}
}

func (i mdnsInterface) addrs() []net.IP {
	return append(append([]net.IP{}, i.ipv4...), i.ipv6...)
}

func containsIP(list []net.IP, ip net.IP) bool {
	for _, item := range list {
		if item.Equal(ip) {
			return true
		}
	}
	return false
}

// mdnsInterfaceSource é um nível da cadeia de descoberta de interfaces.
type mdnsInterfaceSource struct {
	name string
	list func() ([]mdnsInterface, error)
}

// mdnsInterfaceSources: netlink (stdlib, com IPv6) e, quando negado a apps sem
// root (Android 11+), SIOCGIFCONF + SIOCGIFINDEX, somente IPv4.
var mdnsInterfaceSources = []mdnsInterfaceSource{
	{name: "netlink", list: stdlibMDNSInterfaces},
	{name: "ioctl", list: ioctlMDNSInterfaces},
}

func discoverMDNSInterfaces(sources []mdnsInterfaceSource) ([]mdnsInterface, string, error) {
	var errs []error
	for _, source := range sources {
		ifaces, err := source.list()
		if err == nil {
			return ifaces, source.name, nil
		}
		slog.Debug("mdns: interface source failed, falling back", "source", source.name, "error", err)
		errs = append(errs, fmt.Errorf("%s: %w", source.name, err))
	}
	return nil, "", errors.Join(errs...)
}

func stdlibMDNSInterfaces() ([]mdnsInterface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := []mdnsInterface{}
	for _, ifi := range interfaces {
		if ifi.Flags&(net.FlagUp|net.FlagRunning|net.FlagMulticast) != net.FlagUp|net.FlagRunning|net.FlagMulticast ||
			ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses for %s: %w", ifi.Name, err)
		}
		iface := mdnsInterface{index: ifi.Index, name: ifi.Name}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				iface.add(ipnet.IP)
			}
		}
		if len(iface.ipv4)+len(iface.ipv6) > 0 {
			result = append(result, iface)
		}
	}
	return result, nil
}

func ioctlMDNSInterfaces() ([]mdnsInterface, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		return nil, fmt.Errorf("interface ioctl unsupported on %s", runtime.GOOS)
	}
	entries, err := ioctlIPv4()
	if err != nil {
		return nil, err
	}
	byIndex := map[int]*mdnsInterface{}
	result := []mdnsInterface{}
	for _, entry := range entries {
		if entry.Loopback {
			continue
		}
		index, flags, err := linuxInterfaceIndexFlags(entry.Name)
		if err != nil {
			slog.Debug("mdns: interface ioctl failed", "interface", entry.Name, "error", err)
			continue
		}
		if flags&(linuxIFFRunning|linuxIFFMulticast) != linuxIFFRunning|linuxIFFMulticast {
			continue
		}
		iface, ok := byIndex[index]
		if !ok {
			// Aliases (wlan0:1) compartilham o índice do dispositivo base.
			iface = &mdnsInterface{index: index, name: strings.SplitN(entry.Name, ":", 2)[0]}
			byIndex[index] = iface
		}
		iface.add(entry.IP)
	}
	for _, iface := range byIndex {
		if len(iface.ipv4) > 0 {
			result = append(result, *iface)
		}
	}
	return result, nil
}

// linuxInterfaceIndexFlags lê índice e flags por SIOCGIFINDEX/SIOCGIFFLAGS,
// acessíveis sem root mesmo quando o netlink é negado.
func linuxInterfaceIndexFlags(name string) (int, uint16, error) {
	syscall.ForkLock.RLock()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return 0, 0, fmt.Errorf("open socket: %w", err)
	}
	defer syscall.Close(fd)

	var req [linuxIfreqSize]byte
	copy(req[:15], name)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), linuxSIOCGIFFLAGS, uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		return 0, 0, fmt.Errorf("SIOCGIFFLAGS: %w", errno)
	}
	flags := *(*uint16)(unsafe.Pointer(&req[16]))
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), linuxSIOCGIFINDEX, uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		return 0, 0, fmt.Errorf("SIOCGIFINDEX: %w", errno)
	}
	return int(*(*int32)(unsafe.Pointer(&req[16]))), flags, nil
}

// mdnsTransport abstrai um socket UDP 5353 de uma família de endereços.
type mdnsTransport interface {
	read(b []byte) (n int, ifIndex int, src *net.UDPAddr, err error)
	write(b []byte, ifIndex int, dst *net.UDPAddr) error
	join(ifIndex int, name string) error
	group(ifIndex int) *net.UDPAddr
	close() error
}

// mdnsFamily descreve como abrir o socket de uma família e quais interfaces
// devem integrar seu grupo multicast.
type mdnsFamily struct {
	name   string
	wants  func(mdnsInterface) bool
	listen func() (mdnsTransport, error)
}

var mdnsFamilies = []mdnsFamily{
	{name: "ipv4", wants: func(i mdnsInterface) bool { return len(i.ipv4) > 0 }, listen: listenMDNSIPv4},
	{name: "ipv6", wants: func(i mdnsInterface) bool { return i.hasIPv6 }, listen: listenMDNSIPv6},
}

// listenMDNSUDP abre o socket com SO_REUSEADDR e SO_REUSEPORT aplicados antes
// do bind, convivendo com outros respondedores (Avahi, navegadores) na porta.
func listenMDNSUDP(network, host string) (net.PacketConn, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var sockErr error
		err := raw.Control(func(fd uintptr) {
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
				sockErr = fmt.Errorf("SO_REUSEADDR: %w", err)
				return
			}
			reusePort, err := soReusePort()
			if err == nil {
				err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, reusePort, 1)
			}
			if err != nil {
				sockErr = fmt.Errorf("SO_REUSEPORT: %w", err)
			}
		})
		return errors.Join(err, sockErr)
	}}
	return config.ListenPacket(context.Background(), network, net.JoinHostPort(host, strconv.Itoa(mdnsPort)))
}

// soReusePort devolve SO_REUSEPORT, que o pacote syscall não exporta no Linux
// (golang.org/x/sys permanece apenas indireto): 15 no ABI asm-generic (arm,
// arm64, amd64, 386, riscv64...) e 0x200 em MIPS e nos BSDs/Darwin.
func soReusePort() (int, error) {
	switch runtime.GOOS {
	case "linux", "android":
		if strings.HasPrefix(runtime.GOARCH, "mips") {
			return 0x200, nil
		}
		return 15, nil
	case "darwin", "ios", "freebsd", "netbsd", "openbsd", "dragonfly":
		return 0x200, nil
	}
	return 0, fmt.Errorf("unsupported on %s", runtime.GOOS)
}

type mdnsIPv4Conn struct{ pc *ipv4.PacketConn }

func listenMDNSIPv4() (mdnsTransport, error) {
	conn, err := listenMDNSUDP("udp4", "0.0.0.0")
	if err != nil {
		return nil, err
	}
	pc := ipv4.NewPacketConn(conn)
	if err := errors.Join(
		pc.SetControlMessage(ipv4.FlagInterface, true),
		pc.SetMulticastTTL(255),
		pc.SetTTL(255),
		pc.SetMulticastLoopback(true),
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("configure socket: %w", err)
	}
	return &mdnsIPv4Conn{pc: pc}, nil
}

func (c *mdnsIPv4Conn) read(b []byte) (int, int, *net.UDPAddr, error) {
	n, cm, src, err := c.pc.ReadFrom(b)
	if err != nil {
		return 0, 0, nil, err
	}
	ifIndex := 0
	if cm != nil {
		ifIndex = cm.IfIndex
	}
	addr, _ := src.(*net.UDPAddr)
	return n, ifIndex, addr, nil
}

func (c *mdnsIPv4Conn) write(b []byte, ifIndex int, dst *net.UDPAddr) error {
	var cm *ipv4.ControlMessage
	if ifIndex > 0 {
		cm = &ipv4.ControlMessage{IfIndex: ifIndex}
	}
	_, err := c.pc.WriteTo(b, cm, dst)
	return err
}

func (c *mdnsIPv4Conn) join(ifIndex int, name string) error {
	return c.pc.JoinGroup(&net.Interface{Index: ifIndex, Name: name}, &net.UDPAddr{IP: mdnsGroupIPv4})
}

func (c *mdnsIPv4Conn) group(int) *net.UDPAddr {
	return &net.UDPAddr{IP: mdnsGroupIPv4, Port: mdnsPort}
}

func (c *mdnsIPv4Conn) close() error { return c.pc.Close() }

type mdnsIPv6Conn struct{ pc *ipv6.PacketConn }

func listenMDNSIPv6() (mdnsTransport, error) {
	conn, err := listenMDNSUDP("udp6", "::")
	if err != nil {
		return nil, err
	}
	pc := ipv6.NewPacketConn(conn)
	if err := errors.Join(
		pc.SetControlMessage(ipv6.FlagInterface, true),
		pc.SetMulticastHopLimit(255),
		pc.SetHopLimit(255),
		pc.SetMulticastLoopback(true),
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("configure socket: %w", err)
	}
	return &mdnsIPv6Conn{pc: pc}, nil
}

func (c *mdnsIPv6Conn) read(b []byte) (int, int, *net.UDPAddr, error) {
	n, cm, src, err := c.pc.ReadFrom(b)
	if err != nil {
		return 0, 0, nil, err
	}
	ifIndex := 0
	if cm != nil {
		ifIndex = cm.IfIndex
	}
	addr, _ := src.(*net.UDPAddr)
	return n, ifIndex, addr, nil
}

func (c *mdnsIPv6Conn) write(b []byte, ifIndex int, dst *net.UDPAddr) error {
	var cm *ipv6.ControlMessage
	if ifIndex > 0 {
		cm = &ipv6.ControlMessage{IfIndex: ifIndex}
	}
	_, err := c.pc.WriteTo(b, cm, dst)
	return err
}

func (c *mdnsIPv6Conn) join(ifIndex int, name string) error {
	return c.pc.JoinGroup(&net.Interface{Index: ifIndex, Name: name}, &net.UDPAddr{IP: mdnsGroupIPv6})
}

// group usa zona numérica: resolver o nome da interface exigiria netlink.
func (c *mdnsIPv6Conn) group(ifIndex int) *net.UDPAddr {
	return &net.UDPAddr{IP: mdnsGroupIPv6, Port: mdnsPort, Zone: strconv.Itoa(ifIndex)}
}

func (c *mdnsIPv6Conn) close() error { return c.pc.Close() }

type mdnsConn struct {
	family string
	t      mdnsTransport
	wants  func(mdnsInterface) bool
	joined map[int]bool // protegido por mdnsResponder.mu
}

type mdnsState int

const (
	mdnsProbing mdnsState = iota
	mdnsActive
	mdnsConflicted
	mdnsClosed
)

type mdnsResponder struct {
	port     int
	discover func() ([]mdnsInterface, string, error)
	conns    []*mdnsConn

	mu      sync.Mutex
	state   mdnsState
	ifaces  map[int]mdnsInterface
	retired map[string]time.Time // endereços recém-removidos ainda reconhecidos como nossos

	conflict     chan struct{}
	conflictOnce sync.Once
	stop         chan struct{}
	runDone      chan struct{}
	readers      sync.WaitGroup
	closeOnce    sync.Once
}

func registerMDNS(port int, sources []mdnsInterfaceSource, families []mdnsFamily) (func(), error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("mdns: invalid port %d", port)
	}
	discover := func() ([]mdnsInterface, string, error) { return discoverMDNSInterfaces(sources) }
	ifaces, source, err := discover()
	if err != nil {
		return nil, fmt.Errorf("mdns: list interfaces: %w", err)
	}
	if len(ifaces) == 0 {
		return nil, errors.New("mdns: no running multicast LAN interface with an address to publish")
	}

	r := &mdnsResponder{
		port:     port,
		discover: discover,
		ifaces:   map[int]mdnsInterface{},
		retired:  map[string]time.Time{},
		conflict: make(chan struct{}),
		stop:     make(chan struct{}),
	}
	var familyErrs []error
	for _, family := range families {
		var candidates []mdnsInterface
		for _, iface := range ifaces {
			if family.wants(iface) {
				candidates = append(candidates, iface)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		transport, err := family.listen()
		if err != nil {
			familyErrs = append(familyErrs, fmt.Errorf("%s: bind udp port %d: %w", family.name, mdnsPort, err))
			continue
		}
		conn := &mdnsConn{family: family.name, t: transport, wants: family.wants, joined: map[int]bool{}}
		var joinErrs []error
		for _, iface := range candidates {
			if err := transport.join(iface.index, iface.name); err != nil {
				joinErrs = append(joinErrs, fmt.Errorf("%s: %w", iface.name, err))
				continue
			}
			conn.joined[iface.index] = true
		}
		if len(conn.joined) == 0 {
			transport.close()
			familyErrs = append(familyErrs, fmt.Errorf("%s: join multicast group: %w", family.name, errors.Join(joinErrs...)))
			continue
		}
		if len(joinErrs) > 0 {
			slog.Warn("mdns: multicast join failed on some interfaces", "family", family.name, "error", errors.Join(joinErrs...))
		}
		r.conns = append(r.conns, conn)
	}
	if len(r.conns) == 0 {
		if len(familyErrs) == 0 {
			return nil, errors.New("mdns: no address family available")
		}
		return nil, fmt.Errorf("mdns: no usable multicast socket: %w", errors.Join(familyErrs...))
	}
	if len(familyErrs) > 0 {
		slog.Warn("mdns: address family unavailable, continuing with the others", "error", errors.Join(familyErrs...))
	}

	names := []string{}
	for _, iface := range ifaces {
		if r.joinedLocked(iface) {
			r.ifaces[iface.index] = iface
			names = append(names, iface.name)
		}
	}
	for _, conn := range r.conns {
		r.readers.Add(1)
		go r.readLoop(conn)
	}

	if err := r.probe(); err != nil {
		r.shutdown()
		return nil, err
	}
	r.mu.Lock()
	select {
	case <-r.conflict:
		// Conflito recebido entre a última sondagem e a ativação.
		r.mu.Unlock()
		r.shutdown()
		return nil, ErrMDNSNameConflict
	default:
		r.state = mdnsActive
	}
	r.mu.Unlock()
	if r.sendEach(nil, r.announcement) == 0 {
		r.shutdown()
		return nil, errors.New("mdns: could not send the announcement on any interface")
	}
	r.runDone = make(chan struct{})
	go r.run()

	familyNames := []string{}
	for _, conn := range r.conns {
		familyNames = append(familyNames, conn.family)
	}
	slog.Info("mdns: responder started",
		"hostname", strings.TrimSuffix(mdnsHostname, "."),
		"service", strings.TrimSuffix(mdnsInstanceName, "."),
		"port", port,
		"interfaces", names,
		"families", familyNames,
		"source", source,
		"note", "resolution depends on multicast reaching clients; use the literal IP address if phonix.local does not resolve")
	return r.shutdown, nil
}

func (r *mdnsResponder) joinedLocked(iface mdnsInterface) bool {
	for _, conn := range r.conns {
		if conn.joined[iface.index] {
			return true
		}
	}
	return false
}

// probe envia três sondagens (RFC 6762 §8.1) e falha se outro host defender
// os nomes no intervalo.
func (r *mdnsResponder) probe() error {
	select {
	case <-r.conflict:
		return ErrMDNSNameConflict
	case <-time.After(rand.N(mdnsProbeInterval)):
	}
	for range mdnsProbeCount {
		if r.sendEach(nil, r.probeMessage) == 0 {
			return errors.New("mdns: could not send the probe on any interface")
		}
		select {
		case <-r.conflict:
			return ErrMDNSNameConflict
		case <-time.After(mdnsProbeInterval):
		}
	}
	return nil
}

func (r *mdnsResponder) run() {
	defer close(r.runDone)
	again := time.NewTimer(mdnsAnnounceDelay)
	defer again.Stop()
	refresh := time.NewTicker(mdnsRefreshInterval)
	defer refresh.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-again.C:
			if r.isActive() {
				r.sendEach(nil, r.announcement)
			}
		case <-refresh.C:
			r.refresh()
		}
	}
}

func (r *mdnsResponder) isActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == mdnsActive
}

// refresh acompanha trocas de rede (reconexão Wi-Fi, DHCP): adere ao grupo em
// interfaces novas, retira endereços removidos e reanuncia o que mudou.
func (r *mdnsResponder) refresh() {
	ifaces, _, err := r.discover()
	if err != nil {
		slog.Debug("mdns: interface refresh failed", "error", err)
		return
	}
	now := time.Now()
	r.mu.Lock()
	if r.state != mdnsActive {
		r.mu.Unlock()
		return
	}
	current := map[int]mdnsInterface{}
	for _, iface := range ifaces {
		for _, conn := range r.conns {
			if !conn.wants(iface) || conn.joined[iface.index] {
				continue
			}
			if err := conn.t.join(iface.index, iface.name); err != nil && !errors.Is(err, syscall.EADDRINUSE) {
				slog.Warn("mdns: multicast join failed", "family", conn.family, "interface", iface.name, "error", err)
				continue
			}
			conn.joined[iface.index] = true
		}
		if r.joinedLocked(iface) {
			current[iface.index] = iface
		}
	}
	// Interfaces que saíram (sem portadora, desligadas) recebem a despedida
	// antes de serem esquecidas; o envio falha em silêncio se já não existirem.
	type departure struct {
		conn  *mdnsConn
		iface mdnsInterface
	}
	var departed []departure
	for _, conn := range r.conns {
		for index := range conn.joined {
			if _, ok := current[index]; !ok {
				if old, ok := r.ifaces[index]; ok {
					departed = append(departed, departure{conn, old})
				}
				delete(conn.joined, index)
			}
		}
	}
	removed := map[int][]net.IP{}
	changed := map[int]bool{}
	for index, old := range r.ifaces {
		next := current[index]
		for _, ip := range old.addrs() {
			if !containsIP(next.addrs(), ip) {
				r.retired[ip.String()] = now.Add(mdnsRetiredTTL)
				removed[index] = append(removed[index], ip)
			}
		}
	}
	for index, next := range current {
		old, ok := r.ifaces[index]
		if !ok || len(removed[index]) > 0 || len(old.addrs()) != len(next.addrs()) {
			changed[index] = true
		}
	}
	for ip, expiry := range r.retired {
		if now.After(expiry) {
			delete(r.retired, ip)
		}
	}
	r.ifaces = current
	r.mu.Unlock()

	for _, d := range departed {
		if packet, err := r.goodbye(d.iface).Pack(); err == nil {
			if err := d.conn.t.write(packet, d.iface.index, d.conn.t.group(d.iface.index)); err != nil {
				slog.Debug("mdns: goodbye on departed interface failed", "interface", d.iface.name, "error", err)
			}
		}
	}
	if len(departed) > 0 {
		slog.Info("mdns: interfaces left, announcement withdrawn there", "interfaces", len(departed))
	}
	if len(changed) == 0 {
		return
	}
	r.sendEach(changed, func(iface mdnsInterface) *dns.Msg {
		if len(removed[iface.index]) == 0 {
			return nil
		}
		msg := mdnsResponse()
		msg.Answer = finalizeRRs(addressRRs(removed[iface.index]), 0, false, true)
		return msg
	})
	r.sendEach(changed, r.announcement)
	slog.Info("mdns: interface addresses changed, re-announced", "interfaces", len(changed))
}

func (r *mdnsResponder) shutdown() {
	r.closeOnce.Do(func() {
		close(r.stop)
		if r.runDone != nil {
			<-r.runDone
		}
		r.mu.Lock()
		announced := r.state == mdnsActive
		r.state = mdnsClosed
		r.mu.Unlock()
		if announced {
			r.sendEach(nil, r.goodbye)
		}
		for _, conn := range r.conns {
			conn.t.close()
		}
		r.readers.Wait()
		if announced {
			slog.Info("mdns: responder stopped, announcement withdrawn")
		}
	})
}

// sendEach envia, por socket e interface associada ao grupo, a mensagem
// montada por build (nil pula); only restringe as interfaces. Devolve quantos
// envios tiveram sucesso.
func (r *mdnsResponder) sendEach(only map[int]bool, build func(mdnsInterface) *dns.Msg) int {
	type target struct {
		conn  *mdnsConn
		iface mdnsInterface
	}
	var targets []target
	r.mu.Lock()
	for _, conn := range r.conns {
		for index := range conn.joined {
			if iface, ok := r.ifaces[index]; ok && (only == nil || only[index]) {
				targets = append(targets, target{conn, iface})
			}
		}
	}
	r.mu.Unlock()

	sent := 0
	for _, target := range targets {
		msg := build(target.iface)
		if msg == nil {
			continue
		}
		packet, err := msg.Pack()
		if err != nil {
			slog.Debug("mdns: pack failed", "error", err)
			continue
		}
		if err := target.conn.t.write(packet, target.iface.index, target.conn.t.group(target.iface.index)); err != nil {
			slog.Debug("mdns: send failed", "family", target.conn.family, "interface", target.iface.name, "error", err)
			continue
		}
		sent++
	}
	return sent
}

func (r *mdnsResponder) readLoop(conn *mdnsConn) {
	defer r.readers.Done()
	buf := make([]byte, mdnsMaxPacket)
	for {
		n, ifIndex, src, err := conn.t.read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-r.stop:
				return
			default:
			}
			slog.Debug("mdns: read failed", "family", conn.family, "error", err)
			continue
		}
		if src == nil {
			continue
		}
		var msg dns.Msg
		if err := msg.Unpack(buf[:n]); err != nil {
			continue
		}
		r.handle(conn, &msg, src, ifIndex)
	}
}

func (r *mdnsResponder) handle(conn *mdnsConn, msg *dns.Msg, src *net.UDPAddr, ifIndex int) {
	if msg.Opcode != dns.OpcodeQuery || msg.Rcode != dns.RcodeSuccess {
		return
	}
	if r.conflicts(msg) {
		r.reportConflict(src)
		return
	}
	if msg.Response {
		return
	}
	resp, unicast := r.answer(msg, src, ifIndex)
	if resp == nil {
		return
	}
	packet, err := resp.Pack()
	if err != nil {
		slog.Debug("mdns: pack failed", "error", err)
		return
	}
	dst := conn.t.group(ifIndex)
	if unicast {
		dst = src
	}
	if err := conn.t.write(packet, ifIndex, dst); err != nil {
		slog.Debug("mdns: reply failed", "family", conn.family, "to", dst.String(), "error", err)
	}
}

// conflicts detecta outro host respondendo phonix.local ou a instância com
// dados diferentes dos nossos, ou sondando-os enquanto ainda sondamos. Depois
// do anúncio, a sondagem alheia não é conflito: o nome é defendido pela
// resposta (RFC 6762 §8.2). Nossos próprios pacotes voltam pelo loopback
// multicast com dados idênticos e não contam; registros TTL 0 são despedidas.
func (r *mdnsResponder) conflicts(msg *dns.Msg) bool {
	rrs := msg.Ns
	if msg.Response {
		rrs = append(append([]dns.RR{}, msg.Answer...), msg.Extra...)
	}
	if len(rrs) == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != mdnsProbing && !(r.state == mdnsActive && msg.Response) {
		return false
	}
	for _, rr := range rrs {
		header := rr.Header()
		if header.Ttl == 0 || header.Class&^mdnsClassTopBit != dns.ClassINET {
			continue
		}
		switch {
		case strings.EqualFold(header.Name, mdnsHostname):
			switch record := rr.(type) {
			case *dns.A:
				if !r.ownsLocked(record.A) {
					return true
				}
			case *dns.AAAA:
				if !r.ownsLocked(record.AAAA) {
					return true
				}
			}
		case strings.EqualFold(header.Name, mdnsInstanceName):
			if record, ok := rr.(*dns.SRV); ok &&
				(!strings.EqualFold(record.Target, mdnsHostname) || int(record.Port) != r.port) {
				return true
			}
		}
	}
	return false
}

func (r *mdnsResponder) ownsLocked(ip net.IP) bool {
	for _, iface := range r.ifaces {
		if containsIP(iface.addrs(), ip) {
			return true
		}
	}
	expiry, ok := r.retired[ip.String()]
	return ok && time.Now().Before(expiry)
}

// reportConflict interrompe a sondagem ou, se já anunciado, retira o anúncio e
// deixa de responder: o nome não é mais declarado como nosso.
func (r *mdnsResponder) reportConflict(src *net.UDPAddr) {
	r.conflictOnce.Do(func() {
		close(r.conflict)
		r.mu.Lock()
		wasActive := r.state == mdnsActive
		if wasActive {
			r.state = mdnsConflicted
		}
		r.mu.Unlock()
		if wasActive {
			slog.Error("mdns: name conflict, phonix.local is now claimed by another host; stopped answering, use the literal IP address",
				"from", src.String())
			r.sendEach(nil, r.goodbye)
		}
	})
}

// answer monta a resposta a uma consulta (RFC 6762 §6): multicast por padrão
// e na defesa contra sondagens, unicast para perguntas QU ou consultas legadas
// (porta de origem != 5353, como dig/nslookup), com supressão de respostas
// conhecidas.
func (r *mdnsResponder) answer(query *dns.Msg, src *net.UDPAddr, ifIndex int) (*dns.Msg, bool) {
	legacy := src.Port != mdnsPort
	allQU := len(query.Question) > 0
	for _, question := range query.Question {
		if question.Qclass&mdnsClassTopBit == 0 {
			allQU = false
		}
	}
	// A defesa contra uma sondagem (perguntas com seção de autoridade) vai
	// sempre por multicast: com a porta 5353 compartilhada (Avahi, navegadores)
	// o unicast pode ser entregue ao socket de outro processo do sondador.
	probe := len(query.Ns) > 0
	unicast := legacy || (allQU && !probe)

	r.mu.Lock()
	_, served := r.ifaces[ifIndex]
	if r.state != mdnsActive || (!unicast && ifIndex > 0 && !served) {
		// Multicast só nas interfaces publicadas (não em docker0, lo...).
		r.mu.Unlock()
		return nil, false
	}
	records := r.recordsLocked(ifIndex)
	r.mu.Unlock()

	var answers, extra []dns.RR
	for _, question := range query.Question {
		if class := question.Qclass &^ mdnsClassTopBit; class != dns.ClassINET && class != dns.ClassANY {
			continue
		}
		a, x := records.lookup(question.Name, question.Qtype)
		answers = append(answers, a...)
		extra = append(extra, x...)
	}
	answers = withoutRRs(uniqueRRs(answers), query.Answer, true)
	if len(answers) == 0 {
		return nil, false
	}
	extra = withoutRRs(uniqueRRs(extra), answers, false)

	resp := mdnsResponse()
	if legacy {
		resp.Id = query.Id
		resp.Question = query.Question
	}
	resp.Answer = finalizeRRs(answers, mdnsLegacyMaxTTL, legacy, false)
	resp.Extra = finalizeRRs(extra, mdnsLegacyMaxTTL, legacy, false)
	return resp, unicast
}

// recordsLocked publica os endereços da interface de chegada; se desconhecida
// (ex.: consulta unicast local via loopback), os de todas as interfaces.
func (r *mdnsResponder) recordsLocked(ifIndex int) mdnsRecords {
	if iface, ok := r.ifaces[ifIndex]; ok {
		return newMDNSRecords(r.port, iface.addrs())
	}
	var all []net.IP
	for _, iface := range r.ifaces {
		for _, ip := range iface.addrs() {
			if !containsIP(all, ip) {
				all = append(all, ip)
			}
		}
	}
	return newMDNSRecords(r.port, all)
}

func (r *mdnsResponder) interfaceRecords(iface mdnsInterface) mdnsRecords {
	return newMDNSRecords(r.port, iface.addrs())
}

func (r *mdnsResponder) probeMessage(iface mdnsInterface) *dns.Msg {
	records := r.interfaceRecords(iface)
	msg := &dns.Msg{Compress: true}
	msg.Question = []dns.Question{
		{Name: mdnsHostname, Qtype: dns.TypeANY, Qclass: dns.ClassINET | mdnsClassTopBit},
		{Name: mdnsInstanceName, Qtype: dns.TypeANY, Qclass: dns.ClassINET | mdnsClassTopBit},
	}
	msg.Ns = append([]dns.RR{records.srv, records.txt}, records.addrs...)
	return msg
}

func (r *mdnsResponder) announcement(iface mdnsInterface) *dns.Msg {
	msg := mdnsResponse()
	msg.Answer = finalizeRRs(r.interfaceRecords(iface).all(), -1, false, false)
	return msg
}

func (r *mdnsResponder) goodbye(iface mdnsInterface) *dns.Msg {
	msg := mdnsResponse()
	msg.Answer = finalizeRRs(r.interfaceRecords(iface).all(), 0, false, true)
	return msg
}

func mdnsResponse() *dns.Msg {
	msg := &dns.Msg{Compress: true}
	msg.Response = true
	msg.Authoritative = true
	return msg
}

// mdnsRecords é o conjunto publicado para um grupo de endereços.
type mdnsRecords struct {
	addrs                 []dns.RR
	srv, txt, ptr, enum   dns.RR
	hostNSEC, serviceNSEC dns.RR
}

func newMDNSRecords(port int, addrs []net.IP) mdnsRecords {
	records := mdnsRecords{
		addrs: addressRRs(addrs),
		srv: &dns.SRV{Hdr: rrHeader(mdnsInstanceName, dns.TypeSRV, mdnsHostTTL),
			Port: uint16(port), Target: mdnsHostname},
		txt:  &dns.TXT{Hdr: rrHeader(mdnsInstanceName, dns.TypeTXT, mdnsServiceTTL), Txt: []string{mdnsTXTPath}},
		ptr:  &dns.PTR{Hdr: rrHeader(mdnsService, dns.TypePTR, mdnsServiceTTL), Ptr: mdnsInstanceName},
		enum: &dns.PTR{Hdr: rrHeader(mdnsServiceEnum, dns.TypePTR, mdnsServiceTTL), Ptr: mdnsService},
		serviceNSEC: &dns.NSEC{Hdr: rrHeader(mdnsInstanceName, dns.TypeNSEC, mdnsServiceTTL),
			NextDomain: mdnsInstanceName, TypeBitMap: []uint16{dns.TypeTXT, dns.TypeSRV}},
	}
	var types []uint16
	if len(records.byType(dns.TypeA)) > 0 {
		types = append(types, dns.TypeA)
	}
	if len(records.byType(dns.TypeAAAA)) > 0 {
		types = append(types, dns.TypeAAAA)
	}
	records.hostNSEC = &dns.NSEC{Hdr: rrHeader(mdnsHostname, dns.TypeNSEC, mdnsHostTTL),
		NextDomain: mdnsHostname, TypeBitMap: types}
	return records
}

func (m mdnsRecords) all() []dns.RR {
	return append([]dns.RR{m.ptr, m.enum, m.srv, m.txt}, m.addrs...)
}

func (m mdnsRecords) byType(rrtype uint16) []dns.RR {
	var result []dns.RR
	for _, rr := range m.addrs {
		if rr.Header().Rrtype == rrtype {
			result = append(result, rr)
		}
	}
	return result
}

// lookup devolve respostas e registros adicionais (RFC 6762 §6 e RFC 6763
// §12). Tipos inexistentes de nomes nossos recebem NSEC (resposta negativa).
func (m mdnsRecords) lookup(name string, qtype uint16) (answers, extra []dns.RR) {
	switch {
	case strings.EqualFold(name, mdnsHostname):
		a, aaaa := m.byType(dns.TypeA), m.byType(dns.TypeAAAA)
		switch qtype {
		case dns.TypeA:
			return addressAnswer(a, aaaa, m.hostNSEC)
		case dns.TypeAAAA:
			return addressAnswer(aaaa, a, m.hostNSEC)
		case dns.TypeANY:
			return m.addrs, nil
		}
		return []dns.RR{m.hostNSEC}, nil
	case strings.EqualFold(name, mdnsInstanceName):
		switch qtype {
		case dns.TypeSRV:
			return []dns.RR{m.srv}, append([]dns.RR{m.txt}, m.addrs...)
		case dns.TypeTXT:
			return []dns.RR{m.txt}, nil
		case dns.TypeANY:
			return []dns.RR{m.srv, m.txt}, m.addrs
		}
		return []dns.RR{m.serviceNSEC}, nil
	case strings.EqualFold(name, mdnsService) && (qtype == dns.TypePTR || qtype == dns.TypeANY):
		return []dns.RR{m.ptr}, append([]dns.RR{m.srv, m.txt}, m.addrs...)
	case strings.EqualFold(name, mdnsServiceEnum) && (qtype == dns.TypePTR || qtype == dns.TypeANY):
		return []dns.RR{m.enum}, nil
	}
	return nil, nil
}

func addressAnswer(asked, other []dns.RR, nsec dns.RR) (answers, extra []dns.RR) {
	if len(asked) == 0 {
		return []dns.RR{nsec}, other
	}
	if len(other) == 0 {
		return asked, []dns.RR{nsec}
	}
	return asked, other
}

func addressRRs(addrs []net.IP) []dns.RR {
	var rrs []dns.RR
	for _, ip := range addrs {
		if ip4 := ip.To4(); ip4 != nil {
			rrs = append(rrs, &dns.A{Hdr: rrHeader(mdnsHostname, dns.TypeA, mdnsHostTTL), A: ip4})
		} else {
			rrs = append(rrs, &dns.AAAA{Hdr: rrHeader(mdnsHostname, dns.TypeAAAA, mdnsHostTTL), AAAA: ip})
		}
	}
	return rrs
}

func rrHeader(name string, rrtype uint16, ttl uint32) dns.RR_Header {
	return dns.RR_Header{Name: name, Rrtype: rrtype, Class: dns.ClassINET, Ttl: ttl}
}

// finalizeRRs copia os registros para envio: em respostas legadas limita o TTL
// a ttl e omite o bit cache-flush; nas multicast marca cache-flush nos
// registros únicos (todos exceto PTR). goodbye zera o TTL.
func finalizeRRs(rrs []dns.RR, ttl int, legacy, goodbye bool) []dns.RR {
	result := make([]dns.RR, 0, len(rrs))
	for _, rr := range rrs {
		rr = dns.Copy(rr)
		header := rr.Header()
		switch {
		case goodbye:
			header.Ttl = 0
		case legacy && ttl >= 0 && header.Ttl > uint32(ttl):
			header.Ttl = uint32(ttl)
		}
		if !legacy && header.Rrtype != dns.TypePTR {
			header.Class |= mdnsClassTopBit
		}
		result = append(result, rr)
	}
	return result
}

func uniqueRRs(rrs []dns.RR) []dns.RR {
	var result []dns.RR
	for _, rr := range rrs {
		if !containsRR(result, rr, false) {
			result = append(result, rr)
		}
	}
	return result
}

// withoutRRs remove de rrs os presentes em known; com knownAnswer, apenas os
// que o consultante já conhece com mais da metade do TTL (RFC 6762 §7.1).
func withoutRRs(rrs, known []dns.RR, knownAnswer bool) []dns.RR {
	var result []dns.RR
	for _, rr := range rrs {
		if !containsRR(known, rr, knownAnswer) {
			result = append(result, rr)
		}
	}
	return result
}

func containsRR(list []dns.RR, rr dns.RR, checkTTL bool) bool {
	for _, item := range list {
		candidate := dns.Copy(item)
		candidate.Header().Class &^= mdnsClassTopBit
		if dns.IsDuplicate(candidate, rr) && (!checkTTL || item.Header().Ttl >= rr.Header().Ttl/2) {
			return true
		}
	}
	return false
}
