package ws

import (
	"context"
	"log/slog"
	"time"

	"github.com/gustavo-lopes-dev/PhonixOS/internal/collector"
)

// minBroadcastInterval é o piso de cadência do ticker global. set_rate aceita
// qualquer inteiro positivo, mas a coleta compartilhada nunca roda mais rápido
// que 1s para preservar o orçamento de recursos.
const minBroadcastInterval = time.Second

// Broadcaster executa uma única coleta de hardware por tick e distribui o
// snapshot aos clientes ativos cuja cadência individual venceu, sem criar uma
// coleta por conexão.
type Broadcaster struct {
	hub     *Hub
	collect func() (*collector.HardwareMetrics, error)
}

// NewBroadcaster cria o ticker adaptativo apoiado no coletor real de hardware.
func NewBroadcaster(hub *Hub) *Broadcaster {
	return &Broadcaster{hub: hub, collect: collector.CollectHardware}
}

func newBroadcasterWith(hub *Hub, collect func() (*collector.HardwareMetrics, error)) *Broadcaster {
	return &Broadcaster{hub: hub, collect: collect}
}

// Run coleta até o contexto ser cancelado, acelerando ou desacelerando o
// ticker conforme a menor cadência ativa (1s a 5s) e entrando em repouso total
// quando não há conexões ativas.
func (b *Broadcaster) Run(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for {
		if interval := b.interval(); interval > 0 {
			timer.Reset(interval)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			b.tick()
		case <-b.hub.Wake():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

// interval devolve a cadência do próximo tick: a menor cadência ativa, limitada
// ao intervalo recomendado do perfil e ao piso de 1s. Zero significa repouso.
func (b *Broadcaster) interval() time.Duration {
	interval := time.Duration(0)
	for _, c := range b.hub.Clients() {
		if c.pausedOrClosed() {
			continue
		}
		if cadence := c.currentCadence(); interval == 0 || cadence < interval {
			interval = cadence
		}
	}
	if interval == 0 {
		return 0
	}
	if interval > b.hub.defaultCadence {
		interval = b.hub.defaultCadence
	}
	if interval < minBroadcastInterval {
		interval = minBroadcastInterval
	}
	return interval
}

func (b *Broadcaster) tick() {
	snapshot, err := b.collect()
	if err != nil {
		slog.Debug("ws: telemetry collection failed", "error", err)
		return
	}
	b.hub.SetSnapshot(snapshot)
	now := time.Now()
	for _, c := range b.hub.Clients() {
		if !c.due(now) {
			continue
		}
		c.sendTelemetry(snapshot, now)
	}
}
