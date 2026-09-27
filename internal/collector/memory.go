package collector

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const memInfoPath = "/proc/meminfo"

// memInfo agrega os campos de /proc/meminfo usados no cálculo de RAM.
type memInfo struct {
	total     uint64
	available uint64
	free      uint64
	buffers   uint64
	cached    uint64
}

// CollectMemory obtém o consumo de RAM do host a partir de /proc/meminfo,
// reportando total, disponível, usado, cached e buffers em bytes.
func CollectMemory() (*MemoryMetrics, error) {
	info, err := readMemInfo(memInfoPath)
	if err != nil {
		return nil, err
	}
	if info.total == 0 {
		return nil, fmt.Errorf("collector: MemTotal not found in %q", memInfoPath)
	}

	available := info.available
	if available == 0 {
		available = info.free + info.buffers + info.cached
	}
	if available > info.total {
		available = info.total
	}

	used := info.total - available

	return &MemoryMetrics{
		TotalBytes:     info.total,
		AvailableBytes: available,
		UsedBytes:      used,
		UsagePercent:   float64(used) / float64(info.total) * 100,
		CachedBytes:    info.cached,
		BuffersBytes:   info.buffers,
	}, nil
}

// readMemInfo faz o parse de /proc/meminfo, convertendo os valores de kB para
// bytes. Apenas os campos relevantes são mantidos.
func readMemInfo(path string) (info memInfo, err error) {
	file, err := os.Open(path)
	if err != nil {
		return memInfo{}, fmt.Errorf("collector: open %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("collector: close %q: %w", path, closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}

		var target *uint64
		switch key {
		case "MemTotal":
			target = &info.total
		case "MemAvailable":
			target = &info.available
		case "MemFree":
			target = &info.free
		case "Buffers":
			target = &info.buffers
		case "Cached":
			target = &info.cached
		default:
			continue
		}

		bytes, parseErr := parseKBytes(strings.TrimSpace(value))
		if parseErr != nil {
			return memInfo{}, fmt.Errorf("collector: parse %q: %w", key, parseErr)
		}
		*target = bytes
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return memInfo{}, fmt.Errorf("collector: scan %q: %w", path, scanErr)
	}
	return info, nil
}

// parseKBytes converte um valor no formato "12345 kB" para bytes.
func parseKBytes(value string) (uint64, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty value")
	}

	kib, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", fields[0], err)
	}
	return kib * 1024, nil
}
