package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// termuxBatteryProvider é o nível 3 do pipeline: consulta a Termux:API via
// utilitário `termux-battery-status`.
type termuxBatteryProvider struct {
	// command existe para permitir injeção em testes; quando nil, executa
	// `termux-battery-status`.
	command func(context.Context) ([]byte, error)
}

// termuxBatteryStatus espelha o JSON emitido pela Termux:API.
type termuxBatteryStatus struct {
	Health      string  `json:"health"`
	Percentage  *int    `json:"percentage"`
	Plugged     string  `json:"plugged"`
	Status      string  `json:"status"`
	Temperature float64 `json:"temperature"`
}

// GetMetrics executa e interpreta a saída JSON de `termux-battery-status`.
func (p termuxBatteryProvider) GetMetrics() (*BatteryMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), batteryCommandTimeout)
	defer cancel()

	run := p.command
	if run == nil {
		run = func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "termux-battery-status").Output()
		}
	}

	output, err := run(ctx)
	if err != nil {
		return nil, fmt.Errorf("collector: termux-battery-status: %w", err)
	}

	return parseTermuxBattery(output)
}

// parseTermuxBattery converte o payload JSON da Termux:API em métricas.
func parseTermuxBattery(output []byte) (*BatteryMetrics, error) {
	var status termuxBatteryStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("collector: termux battery json: %w", err)
	}
	if status.Percentage == nil || *status.Percentage < 0 || *status.Percentage > 100 || strings.TrimSpace(status.Status) == "" {
		return nil, fmt.Errorf("collector: termux battery percentage or status missing or invalid")
	}

	return &BatteryMetrics{
		LevelPercent: *status.Percentage,
		Status:       termuxStatus(status.Status),
		Health:       termuxHealth(status.Health),
		TemperatureC: status.Temperature,
		VoltageMV:    0,
		Source:       BatterySourceTermuxAPI,
		IsPlugged:    termuxPlugged(status.Plugged),
	}, nil
}

// termuxStatus traduz os estados da Termux:API para o vocabulário do contrato.
func termuxStatus(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "CHARGING":
		return "Charging"
	case "DISCHARGING":
		return "Discharging"
	case "FULL":
		return "Full"
	case "NOT_CHARGING":
		return "Not charging"
	default:
		return "Unknown"
	}
}

// termuxHealth traduz a saúde da Termux:API para o vocabulário do contrato.
func termuxHealth(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "GOOD":
		return "Good"
	case "OVERHEAT":
		return "Overheat"
	case "DEAD":
		return "Dead"
	case "OVER_VOLTAGE":
		return "Over voltage"
	default:
		return "Unspecified"
	}
}

// termuxPlugged indica conexão a uma fonte externa.
func termuxPlugged(raw string) bool {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "PLUGGED_AC", "PLUGGED_USB", "PLUGGED_WIRELESS":
		return true
	default:
		return false
	}
}
