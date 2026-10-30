package collector

import "syscall"

// sysinfoLoadAvg obtém o load average pela syscall sysinfo(2), em ponto fixo
// com 16 bits fracionários (SI_LOAD_SHIFT). Não depende de /proc/loadavg.
func sysinfoLoadAvg() (float64, float64, float64, error) {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return 0, 0, 0, err
	}
	const scale = 1 << 16
	return float64(info.Loads[0]) / scale, float64(info.Loads[1]) / scale, float64(info.Loads[2]) / scale, nil
}
