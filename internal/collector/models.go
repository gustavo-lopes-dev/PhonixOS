package collector

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

// MemoryMetrics detalha o consumo de RAM em bytes e percentual.
type MemoryMetrics struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
	CachedBytes    uint64  `json:"cached_bytes"`
	BuffersBytes   uint64  `json:"buffers_bytes"`
}
