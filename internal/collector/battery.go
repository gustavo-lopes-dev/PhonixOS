package collector

import (
	"log/slog"
	"time"
)

// Limita cada fallback externo para que um serviço Android sem resposta não
// bloqueie indefinidamente a telemetria nem impeça o fallback mock.
const batteryCommandTimeout = time.Second

// batteryProvider é o contrato interno de cada nível do pipeline de bateria.
// Cada provedor devolve as métricas já com sua própria BatterySource ou um erro
// que aciona o próximo nível.
type batteryProvider interface {
	GetMetrics() (*BatteryMetrics, error)
}

// CollectBattery orquestra o pipeline de 4 níveis na ordem SysFS -> dumpsys ->
// Termux:API -> Mock. Falhas dos níveis reais são registradas em nível Debug
// antes do fallback. Como o Mock é terminal e nunca falha, o retorno é sempre
// não nulo.
func CollectBattery() *BatteryMetrics {
	return collectBatteryWith(defaultBatteryProviders())
}

// defaultBatteryProviders monta a cadeia canônica de provedores.
func defaultBatteryProviders() []batteryProvider {
	return []batteryProvider{
		sysfsBatteryProvider{root: powerSupplyDir},
		dumpsysBatteryProvider{},
		termuxBatteryProvider{},
		mockBatteryProvider{},
	}
}

// collectBatteryWith percorre a cadeia em ordem e devolve a primeira leitura
// bem-sucedida. Recebe os provedores por parâmetro para permitir testes.
func collectBatteryWith(providers []batteryProvider) *BatteryMetrics {
	for _, provider := range providers {
		metrics, err := provider.GetMetrics()
		if err != nil {
			slog.Debug("collector: battery provider failed, falling back", "error", err)
			continue
		}
		if metrics != nil {
			return metrics
		}
		slog.Debug("collector: battery provider returned nil metrics, falling back")
	}

	fallback, err := (mockBatteryProvider{}).GetMetrics()
	if err != nil || fallback == nil {
		slog.Warn("collector: mock battery provider failed", "error", err)
		return &BatteryMetrics{
			Status: "Unknown",
			Health: "Unspecified",
			Source: BatterySourceMock,
		}
	}
	return fallback
}
