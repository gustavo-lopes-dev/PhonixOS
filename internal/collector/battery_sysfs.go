package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	powerSupplyDir   = "/sys/class/power_supply"
	thermalZoneDir   = "/sys/class/thermal"
	batteryTypeValue = "Battery"
)

// sysfsBatteryProvider é o nível 1 do pipeline: leitura direta de
// /sys/class/power_supply/ (e /sys/class/thermal/ como fallback de temperatura),
// sem acionar processos externos. root existe para permitir testes com raízes
// controladas.
type sysfsBatteryProvider struct {
	root string
}

// GetMetrics localiza a bateria exposta pelo kernel e extrai nível, status,
// saúde, temperatura, tensão e estado de carga.
func (p sysfsBatteryProvider) GetMetrics() (*BatteryMetrics, error) {
	dir, err := findBatterySupply(p.root)
	if err != nil {
		return nil, err
	}

	capacity, err := readIntFile(filepath.Join(dir, "capacity"))
	if err != nil {
		return nil, fmt.Errorf("collector: sysfs battery capacity: %w", err)
	}

	status := normalizeBatteryStatus(readTextFile(filepath.Join(dir, "status")))
	health := normalizeBatteryHealth(readTextFile(filepath.Join(dir, "health")))
	temperature := batteryTemperatureC(dir, p.root)

	voltageMV := 0
	if microVolts, voltErr := readIntFile(filepath.Join(dir, "voltage_now")); voltErr == nil {
		voltageMV = microVolts / 1000
	}

	plugged := status == "Charging" || status == "Full"
	if !plugged {
		plugged = externalPowerOnline(p.root)
	}

	return &BatteryMetrics{
		LevelPercent: clampPercent(capacity),
		Status:       status,
		Health:       health,
		TemperatureC: temperature,
		VoltageMV:    voltageMV,
		Source:       BatterySourceSysFS,
		IsPlugged:    plugged,
	}, nil
}

// findBatterySupply percorre os dispositivos de energia e devolve o diretório
// cujo campo type é "Battery".
func findBatterySupply(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("collector: read %q: %w", root, err)
	}

	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if readTextFile(filepath.Join(dir, "type")) == batteryTypeValue {
			return dir, nil
		}
	}

	return "", fmt.Errorf("collector: no battery supply found in %q", root)
}

// externalPowerOnline indica se alguma fonte externa (Mains/USB/Wireless/AC)
// está conectada, considerando online == 1.
func externalPowerOnline(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		kind := readTextFile(filepath.Join(dir, "type"))
		if kind == "" || kind == batteryTypeValue || kind == "UPS" {
			continue
		}
		if online, onlineErr := readIntFile(filepath.Join(dir, "online")); onlineErr == nil && online == 1 {
			return true
		}
	}

	return false
}

// batteryTemperatureC lê o campo temp do power_supply (décimos de °C) e, se
// ausente, recorre às zonas térmicas cujo type referencia a bateria.
func batteryTemperatureC(supplyDir, supplyRoot string) float64 {
	if tenths, err := readIntFile(filepath.Join(supplyDir, "temp")); err == nil {
		return float64(tenths) / 10
	}

	thermalRoot := filepath.Join(filepath.Dir(supplyRoot), "thermal")
	if _, err := os.Stat(thermalRoot); err != nil {
		thermalRoot = thermalZoneDir
	}

	zones, err := os.ReadDir(thermalRoot)
	if err != nil {
		return 0
	}

	for _, zone := range zones {
		dir := filepath.Join(thermalRoot, zone.Name())
		kind := strings.ToLower(readTextFile(filepath.Join(dir, "type")))
		if !strings.Contains(kind, "batt") {
			continue
		}
		if milliDegrees, milliErr := readIntFile(filepath.Join(dir, "temp")); milliErr == nil {
			return float64(milliDegrees) / 1000
		}
	}

	return 0
}

// normalizeBatteryStatus converte o status cru do kernel para o vocabulário do
// contrato.
func normalizeBatteryStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "charging":
		return "Charging"
	case "discharging":
		return "Discharging"
	case "full":
		return "Full"
	case "not charging":
		return "Not charging"
	default:
		return "Unknown"
	}
}

// normalizeBatteryHealth converte a saúde crua do kernel para o vocabulário do
// contrato, mapeando variações não previstas para "Unspecified".
func normalizeBatteryHealth(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "good":
		return "Good"
	case "overheat":
		return "Overheat"
	case "dead":
		return "Dead"
	case "over voltage", "overvoltage":
		return "Over voltage"
	default:
		return "Unspecified"
	}
}

// clampPercent limita o nível de carga ao intervalo válido [0, 100].
func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

// readIntFile lê um arquivo com um inteiro decimal e devolve erro em falha.
func readIntFile(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	value, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return 0, fmt.Errorf("collector: parse %q: %w", path, err)
	}
	return value, nil
}

// readTextFile lê um arquivo de texto simples, devolvendo string vazia em falha
// para preservar o fallback gracioso de campos opcionais.
func readTextFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}
