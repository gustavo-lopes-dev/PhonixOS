package collector

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	procStatPath    = "/proc/stat"
	procLoadavgPath = "/proc/loadavg"

	// cpuSampleInterval é o intervalo entre as duas amostras de /proc/stat
	// necessárias para calcular a variação de uso desde o boot.
	cpuSampleInterval = 100 * time.Millisecond
)

// cpuTimes representa os contadores acumulados de um núcleo (ou do agregado).
type cpuTimes struct {
	total uint64
	idle  uint64
}

// cpuStatSample contém o agregado e os contadores por núcleo lógico.
type cpuStatSample struct {
	aggregate cpuTimes
	cores     []cpuTimes
}

// CollectCPU lê o uso total e por núcleo, o load average e a frequência de cada
// núcleo lógico. Falhas na frequência de um núcleo individual não abortam a
// coleta: retornam 0 MHz para aquele núcleo.
func CollectCPU() (*CPUMetrics, error) {
	first, err := readCPUStat(procStatPath)
	if err != nil {
		return nil, err
	}

	time.Sleep(cpuSampleInterval)

	second, err := readCPUStat(procStatPath)
	if err != nil {
		return nil, err
	}

	load1, load5, load15, err := readLoadAvg(procLoadavgPath)
	if err != nil {
		return nil, err
	}

	cores := make([]CPUCoreMetrics, len(second.cores))
	for id := range second.cores {
		cores[id] = CPUCoreMetrics{
			CoreID:       id,
			FrequencyMHz: coreFrequencyMHz(id),
			UsagePercent: usagePercent(first.core(id), second.cores[id]),
		}
	}

	return &CPUMetrics{
		UsagePercent: usagePercent(first.aggregate, second.aggregate),
		LoadAvg1Min:  load1,
		LoadAvg5Min:  load5,
		LoadAvg15Min: load15,
		CoreCount:    len(cores),
		Cores:        cores,
	}, nil
}

// core devolve os contadores anteriores de um núcleo, protegendo contra
// hotplug que altere a quantidade de núcleos entre as amostras.
func (s cpuStatSample) core(id int) cpuTimes {
	if id >= 0 && id < len(s.cores) {
		return s.cores[id]
	}
	return cpuTimes{}
}

// usagePercent calcula o percentual de uso a partir da variação dos
// contadores acumulados, tratando wraparound e divisão por zero.
func usagePercent(prev, curr cpuTimes) float64 {
	if curr.total <= prev.total {
		return 0
	}

	totalDelta := curr.total - prev.total
	var idleDelta uint64
	if curr.idle > prev.idle {
		idleDelta = curr.idle - prev.idle
	}
	if idleDelta > totalDelta {
		idleDelta = totalDelta
	}

	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100
}

// readCPUStat faz o parse de /proc/stat, extraindo o agregado (linha "cpu") e
// cada núcleo lógico (linhas "cpuN").
func readCPUStat(path string) (sample cpuStatSample, err error) {
	file, err := os.Open(path)
	if err != nil {
		return cpuStatSample{}, fmt.Errorf("collector: open %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("collector: close %q: %w", path, closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		times, parseErr := parseCPUTimes(fields[1:])
		if parseErr != nil {
			return cpuStatSample{}, fmt.Errorf("collector: parse %q: %w", line, parseErr)
		}

		if fields[0] == "cpu" {
			sample.aggregate = times
			continue
		}

		id, convErr := strconv.Atoi(fields[0][3:])
		if convErr != nil || id < 0 {
			continue
		}
		for len(sample.cores) <= id {
			sample.cores = append(sample.cores, cpuTimes{})
		}
		sample.cores[id] = times
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return cpuStatSample{}, fmt.Errorf("collector: scan %q: %w", path, scanErr)
	}
	if len(sample.cores) == 0 {
		return cpuStatSample{}, fmt.Errorf("collector: no cpu cores found in %q", path)
	}
	return sample, nil
}

// parseCPUTimes soma os contadores user..steal, tratando idle+iowait como
// ociosidade e ignorando guest/guest_nice para evitar dupla contagem.
func parseCPUTimes(fields []string) (cpuTimes, error) {
	limit := len(fields)
	if limit > 8 {
		limit = 8
	}

	var times cpuTimes
	for i := 0; i < limit; i++ {
		value, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuTimes{}, fmt.Errorf("invalid cpu time %q: %w", fields[i], err)
		}
		times.total += value
		if i == 3 || i == 4 {
			times.idle += value
		}
	}
	return times, nil
}

// readLoadAvg extrai os três primeiros campos de /proc/loadavg.
func readLoadAvg(path string) (float64, float64, float64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("collector: read %q: %w", path, err)
	}

	fields := strings.Fields(string(content))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("collector: malformed loadavg %q", string(content))
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("collector: parse load_avg_1m %q: %w", fields[0], err)
	}
	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("collector: parse load_avg_5m %q: %w", fields[1], err)
	}
	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("collector: parse load_avg_15m %q: %w", fields[2], err)
	}

	return load1, load5, load15, nil
}

// coreFrequencyMHz lê a frequência atual do núcleo em kHz e converte para MHz.
// Em ambientes sem cpufreq exposto (ou com falha de leitura) devolve 0 sem
// interromper a coleta.
func coreFrequencyMHz(coreID int) float64 {
	base := "/sys/devices/system/cpu/cpu" + strconv.Itoa(coreID)

	for _, name := range [...]string{
		"cpufreq/scaling_cur_freq",
		"cpufreq/cpuinfo_cur_freq",
	} {
		khz, err := readUintFile(base + "/" + name)
		if err != nil || khz == 0 {
			continue
		}
		return float64(khz) / 1000
	}

	return 0
}

// readUintFile lê um arquivo contendo um único inteiro em base decimal.
func readUintFile(path string) (uint64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("collector: parse %q: %w", path, err)
	}
	return value, nil
}
