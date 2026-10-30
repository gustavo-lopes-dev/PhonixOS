package collector

import (
	"log/slog"
	"sync"
)

var (
	activeSourcesMu sync.Mutex
	activeSources   = map[string]string{}
)

// reportSource registra em nível Info a fonte que respondeu para um tipo de
// métrica apenas quando ela muda, evitando um log por coleta.
func reportSource(kind, source string) {
	activeSourcesMu.Lock()
	defer activeSourcesMu.Unlock()
	if activeSources[kind] == source {
		return
	}
	activeSources[kind] = source
	slog.Info("collector: telemetry source selected", "metric", kind, "source", source)
}
