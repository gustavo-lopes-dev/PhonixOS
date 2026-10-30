package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sysCPUPath é a raiz sysfs dos núcleos lógicos (online, cpuN/cpuidle, cpufreq).
const sysCPUPath = "/sys/devices/system/cpu"

const (
	// O kernel só credita o tempo ocioso ao contador ao sair do estado de sono:
	// um núcleo grande em sono profundo pode passar centenas de milissegundos
	// sem atualizá-lo e pareceria 100% ocupado numa janela curta. Por isso o uso
	// é calculado sobre a janela alvo (amostras guardadas entre coletas), nunca
	// abaixo da mínima; amostras mais antigas que o limite são descartadas.
	cpuidleMinWindow    = time.Second
	cpuidleTargetWindow = 3 * time.Second
	cpuidleMaxAge       = 10 * time.Second
	cpuidleMaxHistory   = 16
)

type cpuidleSample struct {
	at   time.Time
	idle map[int]uint64
}

// cpuidleSampler estima o uso de cada núcleo online pelo tempo ocioso acumulado
// em cpuN/cpuidle/state*/time (microssegundos): uso = 1 - Δocioso/Δtempo.
// Núcleos offline não avançam esse contador e por isso só entram os listados
// em "online"; um núcleo ausente da amostra base reporta 0%, como no cálculo
// por /proc/stat. O agregado é a média dos núcleos.
type cpuidleSampler struct {
	root  string
	now   func() time.Time
	sleep func(time.Duration)

	mu      sync.Mutex
	history []cpuidleSample
}

var defaultCPUIdleSampler = &cpuidleSampler{root: sysCPUPath, now: time.Now, sleep: time.Sleep}

func (s *cpuidleSampler) usage(frequency func(int) float64) (float64, []CPUCoreMetrics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := readCPUIdleSample(s.root, s.now)
	if err != nil {
		return 0, nil, err
	}
	s.prune(current.at)

	base, ok := s.base(current.at)
	if !ok {
		// Sem histórico utilizável (primeira coleta ou longo repouso): espera a
		// janela mínima uma única vez.
		s.history = append(s.history, current)
		s.sleep(cpuidleMinWindow - current.at.Sub(s.history[0].at))
		base = s.history[0]
		if current, err = readCPUIdleSample(s.root, s.now); err != nil {
			return 0, nil, err
		}
	}
	s.history = append(s.history, current)
	if len(s.history) > cpuidleMaxHistory {
		s.history = s.history[len(s.history)-cpuidleMaxHistory:]
	}

	elapsed := float64(current.at.Sub(base.at).Microseconds())
	if elapsed <= 0 {
		return 0, nil, fmt.Errorf("collector: cpuidle samples without elapsed time")
	}

	ids := make([]int, 0, len(current.idle))
	for id := range current.idle {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	cores := make([]CPUCoreMetrics, 0, len(ids))
	var total float64
	for _, id := range ids {
		usage := 0.0
		if prev, ok := base.idle[id]; ok {
			usage = idleUsagePercent(prev, current.idle[id], elapsed)
		}
		total += usage
		cores = append(cores, CPUCoreMetrics{
			CoreID:       id,
			FrequencyMHz: frequency(id),
			UsagePercent: usage,
		})
	}
	return total / float64(len(cores)), cores, nil
}

// prune descarta amostras velhas demais para representar o uso atual.
func (s *cpuidleSampler) prune(now time.Time) {
	keep := s.history[:0]
	for _, sample := range s.history {
		if age := now.Sub(sample.at); age >= 0 && age <= cpuidleMaxAge {
			keep = append(keep, sample)
		}
	}
	s.history = keep
}

// base escolhe a amostra mais recente com idade >= janela alvo ou, na falta
// dela, a mais antiga com idade >= janela mínima.
func (s *cpuidleSampler) base(now time.Time) (cpuidleSample, bool) {
	for i := len(s.history) - 1; i >= 0; i-- {
		if now.Sub(s.history[i].at) >= cpuidleTargetWindow {
			return s.history[i], true
		}
	}
	if len(s.history) > 0 && now.Sub(s.history[0].at) >= cpuidleMinWindow {
		return s.history[0], true
	}
	return cpuidleSample{}, false
}

// idleUsagePercent converte o delta ocioso em percentual de uso, limitado a
// 0–100 (contadores reiniciados ou arredondamentos do kernel).
func idleUsagePercent(prev, curr uint64, elapsed float64) float64 {
	if curr < prev {
		return 0
	}
	usage := (1 - float64(curr-prev)/elapsed) * 100
	switch {
	case usage < 0:
		return 0
	case usage > 100:
		return 100
	}
	return usage
}

// readCPUIdleSample lê o tempo ocioso total de cada núcleo online.
func readCPUIdleSample(root string, now func() time.Time) (cpuidleSample, error) {
	content, err := readTrimmedFile(filepath.Join(root, "online"))
	if err != nil {
		return cpuidleSample{}, err
	}
	online, err := parseCPUList(content)
	if err != nil {
		return cpuidleSample{}, err
	}
	at := now()

	idle := make(map[int]uint64, len(online))
	for _, id := range online {
		value, err := readCoreIdle(root, id)
		if err != nil {
			return cpuidleSample{}, err
		}
		idle[id] = value
	}
	return cpuidleSample{at: at, idle: idle}, nil
}

// readCoreIdle soma o tempo de todos os estados cpuidle de um núcleo.
func readCoreIdle(root string, id int) (uint64, error) {
	files, err := filepath.Glob(filepath.Join(root, "cpu"+strconv.Itoa(id), "cpuidle", "state*", "time"))
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, fmt.Errorf("collector: no cpuidle states for cpu%d", id)
	}
	var sum uint64
	for _, file := range files {
		value, err := readUintFile(file)
		if err != nil {
			return 0, err
		}
		sum += value
	}
	return sum, nil
}

// parseCPUList interpreta o formato de lista de CPUs do kernel ("0-3,5,7-8").
func parseCPUList(list string) ([]int, error) {
	var ids []int
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		low, high, isRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(low)
		if err != nil {
			return nil, fmt.Errorf("collector: parse cpu list %q: %w", list, err)
		}
		end := start
		if isRange {
			if end, err = strconv.Atoi(high); err != nil {
				return nil, fmt.Errorf("collector: parse cpu list %q: %w", list, err)
			}
		}
		if start < 0 || end < start {
			return nil, fmt.Errorf("collector: invalid cpu range %q", part)
		}
		for id := start; id <= end; id++ {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("collector: empty cpu list %q", list)
	}
	return ids, nil
}

func readTrimmedFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}
