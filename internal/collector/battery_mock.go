package collector

// mockBatteryProvider é o nível 4, terminal do pipeline. Devolve sempre valores
// numericamente seguros e nunca retorna erro, garantindo que o servidor não
// quebre quando nenhuma fonte real está disponível. A indicação "N/A" pertence
// apenas à apresentação na UI.
type mockBatteryProvider struct{}

// GetMetrics devolve o payload neutro de contingência.
func (mockBatteryProvider) GetMetrics() (*BatteryMetrics, error) {
	return &BatteryMetrics{
		LevelPercent: 0,
		Status:       "Unknown",
		Health:       "Unspecified",
		TemperatureC: 0,
		VoltageMV:    0,
		Source:       BatterySourceMock,
		IsPlugged:    false,
	}, nil
}
