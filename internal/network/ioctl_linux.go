package network

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	ifNameSize   = 16
	maxIfreqs    = 64
	sockaddrSize = 16
)

// ifreqSize reproduz sizeof(struct ifreq): nome + união cujo maior membro é
// struct ifmap (2 unsigned long + 5 bytes, alinhado): 40 bytes em 64 bits e
// 32 bytes em 32 bits (armv7).
var ifreqSize = ifNameSize + max(sockaddrSize, int(2*unsafe.Sizeof(uintptr(0))+8))

// ifconf espelha struct ifconf; o alinhamento natural do Go coincide com o do C
// em 32 e 64 bits.
type ifconf struct {
	length int32
	buffer uintptr
}

// ioctlIPv4 lista os IPv4 configurados via SIOCGIFCONF, que não usa netlink nem
// arquivos de /proc e permanece acessível a apps sem root no Android.
func ioctlIPv4() ([]InterfaceIPv4, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open socket: %w", err)
	}
	defer syscall.Close(fd)

	buf := make([]byte, ifreqSize*maxIfreqs)
	conf := ifconf{length: int32(len(buf)), buffer: uintptr(unsafe.Pointer(&buf[0]))}
	if err := ioctl(fd, syscall.SIOCGIFCONF, unsafe.Pointer(&conf)); err != nil {
		return nil, fmt.Errorf("SIOCGIFCONF: %w", err)
	}
	runtime.KeepAlive(buf)

	entries := []InterfaceIPv4{}
	for off := 0; off+ifreqSize <= int(conf.length); off += ifreqSize {
		req := buf[off : off+ifreqSize]
		name := strings.TrimRight(string(req[:ifNameSize]), "\x00")
		family := *(*uint16)(unsafe.Pointer(&req[ifNameSize]))
		if name == "" || family != syscall.AF_INET {
			continue
		}
		flags, err := interfaceFlags(fd, name)
		if err != nil || flags&syscall.IFF_UP == 0 {
			continue
		}
		// sockaddr_in: família (2), porta (2), endereço (4).
		addr := req[ifNameSize+4 : ifNameSize+8]
		entries = append(entries, InterfaceIPv4{
			Name:     name,
			IP:       net.IPv4(addr[0], addr[1], addr[2], addr[3]).To4(),
			Loopback: flags&syscall.IFF_LOOPBACK != 0,
		})
	}
	return entries, nil
}

// interfaceFlags consulta SIOCGIFFLAGS de uma interface pelo nome.
func interfaceFlags(fd int, name string) (uint16, error) {
	req := make([]byte, ifreqSize)
	copy(req[:ifNameSize-1], name)
	if err := ioctl(fd, syscall.SIOCGIFFLAGS, unsafe.Pointer(&req[0])); err != nil {
		return 0, err
	}
	return *(*uint16)(unsafe.Pointer(&req[ifNameSize])), nil
}

func ioctl(fd int, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}
