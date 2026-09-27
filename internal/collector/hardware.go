package collector

import (
	"fmt"
	"log/slog"
	"time"
)

// CollectHardware agrega um snapshot completo de telemetria, combinando CPU,
// RAM, bateria (pipeline de 4 níveis) e rede. O Timestamp é sempre emitido em
// UTC para conformidade com o contrato ISO 8601 / RFC 3339.
func CollectHardware() (*HardwareMetrics, error) {
	return collectHardwareWithNetwork(CollectNetwork)
}

func collectHardwareWithNetwork(collectNetwork func() ([]NetworkMetrics, error)) (*HardwareMetrics, error) {
	cpu, err := CollectCPU()
	if err != nil {
		return nil, fmt.Errorf("collector: cpu metrics: %w", err)
	}

	memory, err := CollectMemory()
	if err != nil {
		return nil, fmt.Errorf("collector: memory metrics: %w", err)
	}

	network, err := collectNetwork()
	if err != nil {
		slog.Debug("collector: network metrics unavailable", "error", err)
		network = []NetworkMetrics{}
	}

	battery := CollectBattery()
	if battery == nil {
		return nil, fmt.Errorf("collector: battery metrics: no provider returned a reading")
	}

	return &HardwareMetrics{
		Timestamp: time.Now().UTC(),
		CPU:       *cpu,
		Memory:    *memory,
		Battery:   *battery,
		Network:   network,
	}, nil
}
