package collector

// BatterySource identifica o nível de fallback acionado na coleta de energia.
type BatterySource string

const (
	BatterySourceSysFS     BatterySource = "sysfs"
	BatterySourceDumpsys   BatterySource = "dumpsys"
	BatterySourceTermuxAPI BatterySource = "termux_api"
	BatterySourceMock      BatterySource = "mock"
)

// CPUCoreMetrics mapeia as métricas individuais de cada núcleo lógico.
type CPUCoreMetrics struct {
	CoreID       int     `json:"core_id"`
	FrequencyMHz float64 `json:"frequency_mhz"`
	UsagePercent float64 `json:"usage_percent"`
}

// CPUMetrics consolida o estado do processador.
type CPUMetrics struct {
	UsagePercent float64          `json:"usage_percent"`
	LoadAvg1Min  float64          `json:"load_avg_1m"`
	LoadAvg5Min  float64          `json:"load_avg_5m"`
	LoadAvg15Min float64          `json:"load_avg_15m"`
	CoreCount    int              `json:"core_count"`
	Cores        []CPUCoreMetrics `json:"cores"`
}

// BatteryMetrics detalha o suprimento de energia do dispositivo.
type BatteryMetrics struct {
	LevelPercent int           `json:"level_percent"`
	Status       string        `json:"status"`
	Health       string        `json:"health"`
	TemperatureC float64       `json:"temperature_c"`
	VoltageMV    int           `json:"voltage_mv"`
	Source       BatterySource `json:"source"`
	IsPlugged    bool          `json:"is_plugged"`
}

// MemoryMetrics detalha o consumo de RAM em bytes e percentual.
type MemoryMetrics struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
	CachedBytes    uint64  `json:"cached_bytes"`
	BuffersBytes   uint64  `json:"buffers_bytes"`
}
