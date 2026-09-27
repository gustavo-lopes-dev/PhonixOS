package ws

import (
	"sync"
	"time"

	"github.com/gustavo-lopes-dev/PhonixOS/internal/collector"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/profile"
)

// defaultPollIntervalS é aplicado quando o perfil não informa uma cadência
// recomendada válida; espelha o intervalo do perfil lite.
const defaultPollIntervalS = 5

// Hub mantém o registro seguro das conexões WebSocket ativas, o perfil de
// sistema anunciado no handshake e o último snapshot de telemetria coletado.
type Hub struct {
	profile        *profile.SystemProfile
	defaultCadence time.Duration

	mu      sync.RWMutex
	clients map[*client]struct{}

	snapMu   sync.RWMutex
	snapshot *collector.HardwareMetrics

	wake chan struct{}
}

// NewHub cria o registro de conexões para o perfil detectado no boot.
func NewHub(p *profile.SystemProfile) *Hub {
	h := &Hub{
		profile: p,
		clients: make(map[*client]struct{}),
		wake:    make(chan struct{}, 1),
	}
	seconds := defaultPollIntervalS
	if p != nil && p.RecommendedPollIntervalS > 0 {
		seconds = p.RecommendedPollIntervalS
	}
	h.defaultCadence = time.Duration(seconds) * time.Second
	return h
}

// Profile devolve uma cópia do perfil para o handshake, evitando mutação
// concorrente do estado compartilhado.
func (h *Hub) Profile() *profile.SystemProfile {
	if h.profile == nil {
		return nil
	}
	copied := *h.profile
	return &copied
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.Notify()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	h.Notify()
}

// Clients devolve uma cópia da lista de conexões ativas para iteração segura
// durante a distribuição de telemetria.
func (h *Hub) Clients() []*client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	return clients
}

// SetSnapshot registra o último snapshot agregado pelo broadcaster.
func (h *Hub) SetSnapshot(snapshot *collector.HardwareMetrics) {
	h.snapMu.Lock()
	h.snapshot = snapshot
	h.snapMu.Unlock()
}

// Snapshot devolve o último snapshot coletado, usado no resume imediato.
func (h *Hub) Snapshot() *collector.HardwareMetrics {
	h.snapMu.RLock()
	defer h.snapMu.RUnlock()
	return h.snapshot
}

// Notify sinaliza ao broadcaster que a composição de clientes ou cadências
// mudou, sem bloquear caso não haja um sinal pendente.
func (h *Hub) Notify() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Wake expõe o canal de sinalização consumido pelo broadcaster.
func (h *Hub) Wake() <-chan struct{} {
	return h.wake
}
