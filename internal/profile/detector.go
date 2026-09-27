package profile

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	// liteMemoryThresholdBytes é o limiar de 2 GB (2147483648 bytes) que separa
	// o perfil lite do perfil performance.
	liteMemoryThresholdBytes uint64 = 2147483648

	litePollIntervalS        = 5
	performancePollIntervalS = 2

	memInfoPath = "/proc/meminfo"
)

// DetectProfile inspeciona a RAM total do host via /proc/meminfo e devolve o
// perfil adaptativo correspondente, propagando o modo de trava de armazenamento
// retornado por database.InitDB.
func DetectProfile(storageLockMode string) (*SystemProfile, error) {
	total, err := totalMemoryBytes(memInfoPath)
	if err != nil {
		return nil, err
	}
	return classifyProfile(total, storageLockMode), nil
}

// classifyProfile aplica a regra de negócio de chaveamento entre lite e
// performance a partir da memória total em bytes.
func classifyProfile(totalMemoryBytes uint64, storageLockMode string) *SystemProfile {
	p := &SystemProfile{
		TotalMemoryBytes: totalMemoryBytes,
		StorageLockMode:  storageLockMode,
	}

	if totalMemoryBytes < liteMemoryThresholdBytes {
		p.Profile = ProfileLite
		p.IsLite = true
		p.RecommendedPollIntervalS = litePollIntervalS
		return p
	}

	p.Profile = ProfilePerformance
	p.IsLite = false
	p.RecommendedPollIntervalS = performancePollIntervalS
	return p
}

// totalMemoryBytes lê o campo MemTotal de um arquivo no formato /proc/meminfo e
// converte o valor de kB para bytes.
func totalMemoryBytes(path string) (total uint64, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %q: %w", path, closeErr)
		}
	}()

	total, err = parseMemTotal(file)
	if err != nil {
		return 0, fmt.Errorf("read %q: %w", path, err)
	}
	return total, nil
}

// parseMemTotal extrai e converte (kB -> bytes) o valor de MemTotal.
func parseMemTotal(r io.Reader) (uint64, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("malformed MemTotal line %q", line)
		}

		kib, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse MemTotal value %q: %w", fields[1], err)
		}
		return kib * 1024, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("MemTotal not found")
}
