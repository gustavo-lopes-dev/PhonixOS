package collector

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// dumpsysBatteryProvider é o nível 2 do pipeline: aciona graciosamente o
// utilitário Android `dumpsys battery` quando o SysFS não expõe a bateria.
type dumpsysBatteryProvider struct {
	// command existe para permitir injeção em testes; quando nil, executa
	// `dumpsys battery`.
	command func() ([]byte, error)
}

// GetMetrics executa e interpreta a saída de `dumpsys battery`.
func (p dumpsysBatteryProvider) GetMetrics() (*BatteryMetrics, error) {
	run := p.command
	if run == nil {
		run = func() ([]byte, error) {
			return exec.Command("dumpsys", "battery").Output()
		}
	}

	output, err := run()
	if err != nil {
		return nil, fmt.Errorf("collector: dumpsys battery: %w", err)
	}

	return parseDumpsysBattery(string(output))
}

// parseDumpsysBattery converte o relatório chave: valor do dumpsys em métricas.
func parseDumpsysBattery(output string) (*BatteryMetrics, error) {
	fields := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(strings.ToLower(key))] = strings.TrimSpace(value)
	}

	level, err := strconv.Atoi(fields["level"])
	if err != nil {
		return nil, fmt.Errorf("collector: dumpsys invalid level: %w", err)
	}

	status := dumpsysStatus(fields["status"])
	plugged := status == "Charging" || status == "Full" ||
		dumpsysPowered(fields["ac powered"]) ||
		dumpsysPowered(fields["usb powered"]) ||
		dumpsysPowered(fields["wireless powered"])

	temperature := 0.0
	if tenths, tempErr := strconv.Atoi(fields["temperature"]); tempErr == nil {
		temperature = float64(tenths) / 10
	}

	voltage := 0
	if millivolts, voltErr := strconv.Atoi(fields["voltage"]); voltErr == nil {
		voltage = millivolts
	}

	return &BatteryMetrics{
		LevelPercent: clampPercent(level),
		Status:       status,
		Health:       dumpsysHealth(fields["health"]),
		TemperatureC: temperature,
		VoltageMV:    voltage,
		Source:       BatterySourceDumpsys,
		IsPlugged:    plugged,
	}, nil
}

// dumpsysStatus traduz os códigos numéricos de status do BatteryManager.
func dumpsysStatus(code string) string {
	switch code {
	case "2":
		return "Charging"
	case "3":
		return "Discharging"
	case "4":
		return "Not charging"
	case "5":
		return "Full"
	default:
		return "Unknown"
	}
}

// dumpsysHealth traduz os códigos numéricos de saúde do BatteryManager.
func dumpsysHealth(code string) string {
	switch code {
	case "2":
		return "Good"
	case "3":
		return "Overheat"
	case "4":
		return "Dead"
	case "5":
		return "Over voltage"
	default:
		return "Unspecified"
	}
}

// dumpsysPowered indica se um campo booleano de alimentação está ativo.
func dumpsysPowered(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}
