package network

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type fakePacket struct {
	data    []byte
	ifIndex int
	src     *net.UDPAddr
}

type fakeWrite struct {
	msg     *dns.Msg
	ifIndex int
	dst     *net.UDPAddr
}

// fakeMDNSTransport simula um socket 5353; onWrite permite injetar respostas
// de outros hosts.
type fakeMDNSTransport struct {
	in        chan fakePacket
	done      chan struct{}
	closeOnce sync.Once
	joinErr   error
	onWrite   func(*fakeMDNSTransport, *dns.Msg)

	mu     sync.Mutex
	writes []fakeWrite
	closed bool
}

func newFakeTransport() *fakeMDNSTransport {
	return &fakeMDNSTransport{in: make(chan fakePacket, 16), done: make(chan struct{})}
}

func (f *fakeMDNSTransport) read(b []byte) (int, int, *net.UDPAddr, error) {
	select {
	case p := <-f.in:
		return copy(b, p.data), p.ifIndex, p.src, nil
	case <-f.done:
		return 0, 0, nil, net.ErrClosed
	}
}

func (f *fakeMDNSTransport) write(b []byte, ifIndex int, dst *net.UDPAddr) error {
	msg := new(dns.Msg)
	if err := msg.Unpack(b); err != nil {
		return err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return net.ErrClosed
	}
	f.writes = append(f.writes, fakeWrite{msg, ifIndex, dst})
	f.mu.Unlock()
	if f.onWrite != nil {
		f.onWrite(f, msg)
	}
	return nil
}

func (f *fakeMDNSTransport) join(int, string) error { return f.joinErr }

func (f *fakeMDNSTransport) group(int) *net.UDPAddr {
	return &net.UDPAddr{IP: mdnsGroupIPv4, Port: mdnsPort}
}

func (f *fakeMDNSTransport) close() error {
	f.closeOnce.Do(func() {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
		close(f.done)
	})
	return nil
}

func (f *fakeMDNSTransport) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeMDNSTransport) sent() []fakeWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeWrite{}, f.writes...)
}

func (f *fakeMDNSTransport) inject(t *testing.T, msg *dns.Msg, src *net.UDPAddr) {
	t.Helper()
	data, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	f.in <- fakePacket{data: data, ifIndex: 3, src: src}
}

var testWlan = mdnsInterface{
	index: 3, name: "wlan0",
	ipv4: []net.IP{net.IPv4(192, 168, 1, 10).To4()},
	ipv6: []net.IP{net.ParseIP("fd00::10")}, hasIPv6: true,
}

func fakeSources(ifaces ...mdnsInterface) []mdnsInterfaceSource {
	return []mdnsInterfaceSource{{name: "fake", list: func() ([]mdnsInterface, error) { return ifaces, nil }}}
}

func fakeFamily(name string, transport mdnsTransport, err error) mdnsFamily {
	return mdnsFamily{
		name:  name,
		wants: func(mdnsInterface) bool { return true },
		listen: func() (mdnsTransport, error) {
			if err != nil {
				return nil, err
			}
			return transport, nil
		},
	}
}

func TestRegisterMDNSRejectsInvalidPort(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		if _, err := RegisterMDNS(port); err == nil {
			t.Fatalf("port %d accepted", port)
		}
	}
}

func TestRegisterMDNSWithoutInterfaces(t *testing.T) {
	if _, err := registerMDNS(8080, fakeSources(), nil); err == nil {
		t.Fatal("expected error without interfaces")
	}
	failing := []mdnsInterfaceSource{{name: "netlink", list: func() ([]mdnsInterface, error) {
		return nil, errors.New("permission denied")
	}}}
	if _, err := registerMDNS(8080, failing, nil); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRegisterMDNSReturnsBindAndJoinFailures(t *testing.T) {
	_, err := registerMDNS(8080, fakeSources(testWlan), []mdnsFamily{
		fakeFamily("ipv4", nil, errors.New("address already in use")),
	})
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("bind failure not returned: %v", err)
	}

	transport := newFakeTransport()
	transport.joinErr = errors.New("no such device")
	_, err = registerMDNS(8080, fakeSources(testWlan), []mdnsFamily{fakeFamily("ipv4", transport, nil)})
	if err == nil || !strings.Contains(err.Error(), "no such device") {
		t.Fatalf("join failure not returned: %v", err)
	}
	if !transport.isClosed() {
		t.Fatal("socket left open after join failure")
	}
}

func TestRegisterMDNSAnnouncesAnswersAndWithdraws(t *testing.T) {
	ipv4 := newFakeTransport()
	shutdown, err := registerMDNS(8080, fakeSources(testWlan), []mdnsFamily{
		fakeFamily("ipv4", ipv4, nil),
		fakeFamily("ipv6", nil, errors.New("ipv6 disabled")),
	})
	if err != nil {
		t.Fatal(err)
	}

	writes := ipv4.sent()
	if len(writes) != mdnsProbeCount+1 {
		t.Fatalf("expected %d probes + 1 announcement, got %d writes", mdnsProbeCount, len(writes))
	}
	for _, w := range writes[:mdnsProbeCount] {
		if w.msg.Response || len(w.msg.Question) != 2 || len(w.msg.Ns) == 0 {
			t.Fatalf("bad probe %v", w.msg)
		}
	}
	announcement := writes[mdnsProbeCount].msg
	if !announcement.Response || !hasRecord(announcement.Answer, mdnsHostname, dns.TypeA, "192.168.1.10") ||
		!hasRecord(announcement.Answer, mdnsHostname, dns.TypeAAAA, "fd00::10") ||
		!hasRecord(announcement.Answer, mdnsService, dns.TypePTR, mdnsInstanceName) ||
		!hasRecord(announcement.Answer, mdnsInstanceName, dns.TypeSRV, "8080 "+mdnsHostname) {
		t.Fatalf("incomplete announcement %v", announcement)
	}

	// Consulta multicast A por phonix.local.
	query := new(dns.Msg)
	query.SetQuestion(mdnsHostname, dns.TypeA)
	query.Id = 0
	before := len(ipv4.sent())
	ipv4.inject(t, query, &net.UDPAddr{IP: net.IPv4(192, 168, 1, 20), Port: mdnsPort})
	reply := waitWrite(t, ipv4, before)
	if !reply.dst.IP.Equal(mdnsGroupIPv4) || reply.ifIndex != 3 ||
		!hasRecord(reply.msg.Answer, mdnsHostname, dns.TypeA, "192.168.1.10") {
		t.Fatalf("bad multicast reply %+v %v", reply, reply.msg)
	}

	shutdown()
	shutdown() // idempotente
	if !ipv4.isClosed() {
		t.Fatal("socket not closed by shutdown")
	}
	writes = ipv4.sent()
	goodbye := writes[len(writes)-1].msg
	for _, rr := range goodbye.Answer {
		if rr.Header().Ttl != 0 {
			t.Fatalf("goodbye with ttl %d: %v", rr.Header().Ttl, rr)
		}
	}
	if !hasRecord(goodbye.Answer, mdnsHostname, dns.TypeA, "192.168.1.10") {
		t.Fatalf("goodbye misses address %v", goodbye)
	}
}

func TestMDNSRefreshTracksInterfaces(t *testing.T) {
	transport := newFakeTransport()
	eth := mdnsInterface{index: 4, name: "eth0", ipv4: []net.IP{net.IPv4(10, 0, 0, 5).To4()}}
	current := []mdnsInterface{testWlan}
	r := activeResponder(testWlan)
	r.discover = func() ([]mdnsInterface, string, error) { return current, "fake", nil }
	r.conns = []*mdnsConn{{family: "ipv4", t: transport, wants: func(mdnsInterface) bool { return true },
		joined: map[int]bool{testWlan.index: true}}}

	// Nova interface: adesão e anúncio só nela.
	current = []mdnsInterface{testWlan, eth}
	r.refresh()
	writes := transport.sent()
	if len(writes) != 1 || writes[0].ifIndex != 4 || !hasRecord(writes[0].msg.Answer, mdnsHostname, dns.TypeA, "10.0.0.5") {
		t.Fatalf("new interface announcement: %+v", writes)
	}

	// Endereço trocado no wlan0 (DHCP): despedida do antigo e anúncio do novo.
	renewed := testWlan
	renewed.ipv4 = []net.IP{net.IPv4(192, 168, 1, 11).To4()}
	current = []mdnsInterface{renewed, eth}
	r.refresh()
	writes = transport.sent()[1:]
	if len(writes) != 2 || writes[0].msg.Answer[0].Header().Ttl != 0 ||
		!hasRecord(writes[0].msg.Answer, mdnsHostname, dns.TypeA, "192.168.1.10") ||
		!hasRecord(writes[1].msg.Answer, mdnsHostname, dns.TypeA, "192.168.1.11") {
		t.Fatalf("address change: %+v", writes)
	}
	if !r.ownsLocked(net.IPv4(192, 168, 1, 10)) {
		t.Fatal("retired address no longer recognized")
	}

	// eth0 sem portadora: despedida nela e fim da adesão registrada.
	current = []mdnsInterface{renewed}
	r.refresh()
	writes = transport.sent()[3:]
	if len(writes) != 1 || writes[0].ifIndex != 4 || writes[0].msg.Answer[0].Header().Ttl != 0 {
		t.Fatalf("departed interface goodbye: %+v", writes)
	}
	if r.conns[0].joined[4] {
		t.Fatal("departed interface still marked as joined")
	}
}

func TestRegisterMDNSNameConflictDuringProbe(t *testing.T) {
	transport := newFakeTransport()
	transport.onWrite = func(f *fakeMDNSTransport, msg *dns.Msg) {
		if msg.Response {
			return
		}
		other := new(dns.Msg)
		other.Response = true
		other.Answer = []dns.RR{&dns.A{Hdr: rrHeader(mdnsHostname, dns.TypeA, 120), A: net.IPv4(192, 168, 1, 99)}}
		data, _ := other.Pack()
		select {
		case f.in <- fakePacket{data: data, ifIndex: 3, src: &net.UDPAddr{IP: net.IPv4(192, 168, 1, 99), Port: mdnsPort}}:
		default:
		}
	}
	_, err := registerMDNS(8080, fakeSources(testWlan), []mdnsFamily{fakeFamily("ipv4", transport, nil)})
	if !errors.Is(err, ErrMDNSNameConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if !transport.isClosed() {
		t.Fatal("socket left open after conflict")
	}
	for _, w := range transport.sent() {
		if w.msg.Response {
			t.Fatalf("name announced despite conflict: %v", w.msg)
		}
	}
}

func TestIoctlMDNSInterfacesMatchNetlink(t *testing.T) {
	viaNetlink, err := stdlibMDNSInterfaces()
	if err != nil {
		t.Skip("netlink unavailable:", err)
	}
	viaIoctl, err := ioctlMDNSInterfaces()
	if err != nil {
		t.Skip("ioctl fallback unavailable:", err)
	}
	byIndex := map[int]mdnsInterface{}
	for _, iface := range viaIoctl {
		byIndex[iface.index] = iface
	}
	for _, want := range viaNetlink {
		if len(want.ipv4) == 0 {
			continue
		}
		got, ok := byIndex[want.index]
		if !ok || got.name != want.name || len(got.ipv4) != len(want.ipv4) {
			t.Errorf("interface %s(%d): ioctl gave %+v", want.name, want.index, got)
		}
	}
}

func activeResponder(ifaces ...mdnsInterface) *mdnsResponder {
	r := &mdnsResponder{port: 8080, state: mdnsActive, ifaces: map[int]mdnsInterface{}, retired: map[string]time.Time{}}
	for _, iface := range ifaces {
		r.ifaces[iface.index] = iface
	}
	return r
}

func TestMDNSAnswer(t *testing.T) {
	eth := mdnsInterface{index: 4, name: "eth0", ipv4: []net.IP{net.IPv4(10, 0, 0, 5).To4()}}
	r := activeResponder(testWlan, eth)
	peer := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 20), Port: mdnsPort}

	query := new(dns.Msg)
	query.SetQuestion(mdnsHostname, dns.TypeA)
	resp, unicast := r.answer(query, peer, 3)
	if unicast || len(resp.Answer) != 1 || !hasRecord(resp.Answer, mdnsHostname, dns.TypeA, "192.168.1.10") ||
		!hasRecord(resp.Extra, mdnsHostname, dns.TypeAAAA, "fd00::10") {
		t.Fatalf("A answer on wlan0: %v", resp)
	}
	if resp.Answer[0].Header().Class != dns.ClassINET|mdnsClassTopBit {
		t.Fatal("unique record without cache-flush")
	}

	// Interface só IPv4: AAAA recebe NSEC negativo.
	query.SetQuestion(mdnsHostname, dns.TypeAAAA)
	resp, _ = r.answer(query, peer, 4)
	if len(resp.Answer) != 1 || resp.Answer[0].Header().Rrtype != dns.TypeNSEC ||
		!hasRecord(resp.Extra, mdnsHostname, dns.TypeA, "10.0.0.5") {
		t.Fatalf("negative AAAA answer: %v", resp)
	}

	// Navegação DNS-SD: PTR com SRV/TXT/endereços adicionais.
	query.SetQuestion(mdnsService, dns.TypePTR)
	resp, _ = r.answer(query, peer, 3)
	if !hasRecord(resp.Answer, mdnsService, dns.TypePTR, mdnsInstanceName) ||
		!hasRecord(resp.Extra, mdnsInstanceName, dns.TypeSRV, "8080 "+mdnsHostname) ||
		!hasRecord(resp.Extra, mdnsInstanceName, dns.TypeTXT, mdnsTXTPath) ||
		!hasRecord(resp.Extra, mdnsHostname, dns.TypeA, "192.168.1.10") {
		t.Fatalf("PTR answer: %v", resp)
	}
	if resp.Answer[0].Header().Class != dns.ClassINET {
		t.Fatal("shared PTR record with cache-flush")
	}

	// Consulta legada (dig): unicast, mesmo ID, pergunta repetida, TTL <= 10.
	query.SetQuestion(mdnsInstanceName, dns.TypeSRV)
	query.Id = 4242
	resp, unicast = r.answer(query, &net.UDPAddr{IP: net.IPv4(192, 168, 1, 20), Port: 40000}, 3)
	if !unicast || resp.Id != 4242 || len(resp.Question) != 1 {
		t.Fatalf("legacy reply: %v", resp)
	}
	for _, rr := range append(resp.Answer, resp.Extra...) {
		if rr.Header().Ttl > mdnsLegacyMaxTTL || rr.Header().Class != dns.ClassINET {
			t.Fatalf("legacy record %v", rr)
		}
	}

	// Pergunta QU: resposta unicast para a porta 5353.
	query = new(dns.Msg)
	query.Question = []dns.Question{{Name: mdnsHostname, Qtype: dns.TypeA, Qclass: dns.ClassINET | mdnsClassTopBit}}
	if _, unicast = r.answer(query, peer, 3); !unicast {
		t.Fatal("QU question answered by multicast")
	}

	// Defesa contra sondagem alheia (QU + autoridade): multicast, com os
	// nossos endereços, para alcançar o sondador mesmo com a porta compartilhada.
	probe := new(dns.Msg)
	probe.Question = []dns.Question{{Name: mdnsHostname, Qtype: dns.TypeANY, Qclass: dns.ClassINET | mdnsClassTopBit}}
	probe.Ns = []dns.RR{&dns.A{Hdr: rrHeader(mdnsHostname, dns.TypeA, 120), A: net.IPv4(192, 168, 1, 99)}}
	resp, unicast = r.answer(probe, peer, 3)
	if unicast || !hasRecord(resp.Answer, mdnsHostname, dns.TypeA, "192.168.1.10") {
		t.Fatalf("probe defense: unicast=%v %v", unicast, resp)
	}

	// Supressão de resposta conhecida com TTL suficiente.
	query.SetQuestion(mdnsHostname, dns.TypeA)
	query.Answer = []dns.RR{&dns.A{Hdr: rrHeader(mdnsHostname, dns.TypeA, 100), A: net.IPv4(192, 168, 1, 10)}}
	if resp, _ = r.answer(query, peer, 3); resp != nil {
		t.Fatalf("known answer not suppressed: %v", resp)
	}
	query.Answer[0].Header().Ttl = 30
	if resp, _ = r.answer(query, peer, 3); resp == nil {
		t.Fatal("stale known answer suppressed")
	}

	// Interface não publicada (docker0, lo): sem multicast; unicast responde
	// com os endereços de todas as interfaces.
	query = new(dns.Msg)
	query.SetQuestion(mdnsHostname, dns.TypeA)
	if resp, _ = r.answer(query, peer, 99); resp != nil {
		t.Fatalf("multicast reply on unserved interface: %v", resp)
	}
	resp, unicast = r.answer(query, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}, 1)
	if !unicast || !hasRecord(resp.Answer, mdnsHostname, dns.TypeA, "192.168.1.10") ||
		!hasRecord(resp.Answer, mdnsHostname, dns.TypeA, "10.0.0.5") {
		t.Fatalf("unicast reply on unserved interface: %v", resp)
	}

	// Nomes alheios e respostas não são respondidos; inativo não responde.
	query = new(dns.Msg)
	query.SetQuestion("other.local.", dns.TypeA)
	if resp, _ = r.answer(query, peer, 3); resp != nil {
		t.Fatalf("answered foreign name: %v", resp)
	}
	r.state = mdnsConflicted
	query.SetQuestion(mdnsHostname, dns.TypeA)
	if resp, _ = r.answer(query, peer, 3); resp != nil {
		t.Fatal("answered while conflicted")
	}
}

func TestMDNSConflictDetection(t *testing.T) {
	r := activeResponder(testWlan)
	response := func(rrs ...dns.RR) *dns.Msg {
		msg := new(dns.Msg)
		msg.Response = true
		msg.Answer = rrs
		return msg
	}
	own := &dns.A{Hdr: rrHeader(mdnsHostname, dns.TypeA, 120), A: net.IPv4(192, 168, 1, 10)}
	foreign := &dns.A{Hdr: rrHeader(mdnsHostname, dns.TypeA, 120), A: net.IPv4(192, 168, 1, 99)}
	foreignGoodbye := &dns.A{Hdr: rrHeader(mdnsHostname, dns.TypeA, 0), A: net.IPv4(192, 168, 1, 99)}
	otherPort := &dns.SRV{Hdr: rrHeader(mdnsInstanceName, dns.TypeSRV, 120), Port: 9000, Target: mdnsHostname}
	otherHost := &dns.A{Hdr: rrHeader("tv.local.", dns.TypeA, 120), A: net.IPv4(192, 168, 1, 99)}

	cases := []struct {
		name string
		msg  *dns.Msg
		want bool
	}{
		{"own loopback", response(own), false},
		{"foreign address", response(foreign), true},
		{"foreign goodbye", response(foreignGoodbye), false},
		{"srv other port", response(otherPort), true},
		{"unrelated name", response(otherHost), false},
		{"foreign probe after announcement (defended)", &dns.Msg{Ns: []dns.RR{foreign}}, false},
		{"own probe", &dns.Msg{Ns: []dns.RR{own}}, false},
	}
	for _, tc := range cases {
		if got := r.conflicts(tc.msg); got != tc.want {
			t.Errorf("%s: conflicts = %v, want %v", tc.name, got, tc.want)
		}
	}

	r.state = mdnsProbing
	if !r.conflicts(&dns.Msg{Ns: []dns.RR{foreign}}) {
		t.Error("foreign probe while probing not treated as conflict")
	}
	r.state = mdnsActive

	r.retired["192.168.1.99"] = time.Now().Add(time.Minute)
	if r.conflicts(response(foreign)) {
		t.Error("recently retired own address treated as conflict")
	}
}

func hasRecord(rrs []dns.RR, name string, rrtype uint16, value string) bool {
	for _, rr := range rrs {
		header := rr.Header()
		if !strings.EqualFold(header.Name, name) || header.Rrtype != rrtype {
			continue
		}
		var data string
		switch record := rr.(type) {
		case *dns.A:
			data = record.A.String()
		case *dns.AAAA:
			data = record.AAAA.String()
		case *dns.PTR:
			data = record.Ptr
		case *dns.SRV:
			data = strconv.Itoa(int(record.Port)) + " " + record.Target
		case *dns.TXT:
			data = strings.Join(record.Txt, " ")
		}
		if data == value {
			return true
		}
	}
	return false
}

func waitWrite(t *testing.T, f *fakeMDNSTransport, after int) fakeWrite {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if writes := f.sent(); len(writes) > after {
			return writes[after]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no reply written")
	return fakeWrite{}
}

// TestRegisterMDNSOnNetwork exercita sockets reais na porta 5353 (exige rede com
// multicast e coexistência com outros respondedores): PHONIX_MDNS_NETWORK_TEST=1.
func TestRegisterMDNSOnNetwork(t *testing.T) {
	if os.Getenv("PHONIX_MDNS_NETWORK_TEST") != "1" {
		t.Skip("set PHONIX_MDNS_NETWORK_TEST=1 to bind UDP 5353 on the real network")
	}
	fdsBefore := openFDs(t)
	shutdown, err := RegisterMDNS(8080)
	if err != nil {
		t.Fatal(err)
	}

	for _, q := range []struct {
		name  string
		qtype uint16
		want  uint16
	}{
		{mdnsHostname, dns.TypeA, dns.TypeA},
		{mdnsService, dns.TypePTR, dns.TypePTR},
		{mdnsInstanceName, dns.TypeSRV, dns.TypeSRV},
		{mdnsInstanceName, dns.TypeTXT, dns.TypeTXT},
	} {
		query := new(dns.Msg)
		query.SetQuestion(q.name, q.qtype)
		resp, err := legacyQuery(query)
		if err != nil {
			shutdown()
			t.Fatalf("%s %s: %v", q.name, dns.TypeToString[q.qtype], err)
		}
		found := false
		for _, rr := range resp.Answer {
			if rr.Header().Rrtype == q.want && strings.EqualFold(rr.Header().Name, q.name) {
				found = true
				t.Logf("%s", rr)
			}
		}
		if !found {
			shutdown()
			t.Fatalf("%s %s: no answer in %v", q.name, dns.TypeToString[q.qtype], resp)
		}
	}

	shutdown()
	if fdsAfter := openFDs(t); fdsAfter > fdsBefore {
		t.Fatalf("file descriptors leaked: %d before, %d after", fdsBefore, fdsAfter)
	}
	// Reabrir após o shutdown confirma que os sockets foram liberados.
	shutdown, err = RegisterMDNS(8080)
	if err != nil {
		t.Fatalf("re-register after shutdown: %v", err)
	}
	shutdown()
}

// legacyQuery envia uma consulta legada (porta efêmera, como o dig) ao grupo
// multicast por socket não conectado: a resposta unicast vem do IP do host.
func legacyQuery(query *dns.Msg) (*dns.Msg, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	data, err := query.Pack()
	if err != nil {
		return nil, err
	}
	if _, err := conn.WriteTo(data, &net.UDPAddr{IP: mdnsGroupIPv4, Port: mdnsPort}); err != nil {
		return nil, err
	}
	buf := make([]byte, mdnsMaxPacket)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return nil, err
		}
		resp := new(dns.Msg)
		if resp.Unpack(buf[:n]) == nil && resp.Id == query.Id {
			return resp, nil
		}
	}
}

func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("fd count unavailable:", err)
	}
	return len(entries)
}
