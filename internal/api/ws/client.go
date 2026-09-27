package ws

import (
	"encoding/json"
	"log/slog"
	"math"
	"sync"
	"time"

	websocket "github.com/gofiber/contrib/websocket"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/collector"
)

const (
	eventSystemProfile = "system_profile"
	eventTelemetryTick = "telemetry_tick"

	actionPause   = "pause"
	actionResume  = "resume"
	actionSetRate = "set_rate"

	clientSendBuffer       = 8
	maxUpstreamMessageSize = 1024
)

// messageConn abstrai a conexão WebSocket para permitir testes determinísticos
// sem um socket real; *websocket.Conn satisfaz esta interface.
type messageConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetReadLimit(limit int64)
	WriteControl(messageType int, data []byte, deadline time.Time) error
	Close() error
}

// serverMessage é o envelope downstream canônico (contracts.md §6.2).
type serverMessage struct {
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

// clientMessage é o envelope upstream canônico (contracts.md §6.3).
type clientMessage struct {
	Action string `json:"action"`
	RateMS *int   `json:"rate_ms"`
}

// client representa uma sessão WebSocket com cadência e pausa individuais.
type client struct {
	hub  *Hub
	conn messageConn
	send chan []byte

	closeOnce sync.Once
	mu        sync.Mutex
	paused    bool
	closed    bool
	override  time.Duration
	nextSend  time.Time
}

func newClient(h *Hub, conn messageConn) *client {
	return &client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, clientSendBuffer),
		nextSend: time.Now(),
	}
}

// Handle é o adaptador registrado na rota /ws/telemetry. Emite o
// system_profile imediatamente após o handshake e então processa as ações
// upstream até o encerramento da conexão.
func (h *Hub) Handle(conn *websocket.Conn) {
	h.handleConn(conn)
}

func (h *Hub) handleConn(conn messageConn) {
	c := newClient(h, conn)
	go c.writePump()
	c.enqueue(eventSystemProfile, h.Profile())
	if !h.add(c) {
		return
	}
	c.readPump()
	h.remove(c)
	c.close()
}

// readPump é o leitor upstream de ações; é o único leitor concorrente da
// conexão, conforme a restrição de leitura/escrita do fasthttp/websocket.
func (c *client) readPump() {
	c.conn.SetReadLimit(maxUpstreamMessageSize)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Debug("ws: read loop ended", "error", err)
			}
			return
		}
		c.handleAction(data)
	}
}

func (c *client) handleAction(data []byte) {
	var message clientMessage
	if err := json.Unmarshal(data, &message); err != nil {
		slog.Debug("ws: invalid upstream payload", "error", err)
		return
	}
	switch message.Action {
	case actionPause:
		c.mu.Lock()
		c.paused = true
		c.mu.Unlock()
		c.hub.Notify()
	case actionResume:
		c.resume()
	case actionSetRate:
		if message.RateMS == nil || *message.RateMS < 0 || int64(*message.RateMS) > math.MaxInt64/int64(time.Millisecond) {
			slog.Debug("ws: rejected set_rate", "rate_ms", message.RateMS)
			return
		}
		c.setRate(time.Duration(*message.RateMS) * time.Millisecond)
	default:
		slog.Debug("ws: unknown upstream action", "action", message.Action)
	}
}

// resume restaura a emissão: envia um snapshot imediato do último estado
// conhecido e recalcula a próxima janela de envio pela cadência corrente.
func (c *client) resume() {
	now := time.Now()
	c.mu.Lock()
	c.paused = false
	cadence := c.cadenceLocked()
	snapshot := c.hub.Snapshot()
	fresh := snapshot != nil && !snapshot.Timestamp.IsZero() && now.Sub(snapshot.Timestamp) < cadence
	if fresh {
		c.nextSend = now.Add(cadence)
	} else {
		c.nextSend = now
	}
	c.mu.Unlock()
	if fresh {
		c.enqueue(eventTelemetryTick, snapshot)
	} else {
		c.hub.RequestSnapshot()
	}
	c.hub.Notify()
}

// setRate substitui a cadência apenas desta conexão; rate_ms igual a zero
// remove a substituição e restaura a cadência recomendada do perfil.
func (c *client) setRate(rate time.Duration) {
	c.mu.Lock()
	if rate <= 0 {
		c.override = 0
	} else {
		c.override = rate
	}
	c.nextSend = time.Now().Add(c.cadenceLocked())
	c.mu.Unlock()
	c.hub.Notify()
}

func (c *client) closeGracefully(deadline time.Time) {
	if err := c.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		deadline); err != nil {
		slog.Debug("ws: send close frame", "error", err)
	}
	c.close()
}

func (c *client) cadenceLocked() time.Duration {
	if c.override > 0 {
		return c.override
	}
	return c.hub.defaultCadence
}

func (c *client) currentCadence() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cadenceLocked()
}

func (c *client) pausedOrClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused || c.closed
}

func (c *client) due(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && !c.paused && !now.Before(c.nextSend)
}

func (c *client) sendTelemetry(snapshot *collector.HardwareMetrics, now time.Time) {
	c.mu.Lock()
	if c.closed || c.paused {
		c.mu.Unlock()
		return
	}
	c.nextSend = now.Add(c.cadenceLocked())
	c.mu.Unlock()
	c.enqueue(eventTelemetryTick, snapshot)
}

func (c *client) enqueue(event string, payload any) {
	message, err := json.Marshal(serverMessage{Event: event, Payload: payload})
	if err != nil {
		slog.Error("ws: encode downstream message", "error", err)
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	select {
	case c.send <- message:
		c.mu.Unlock()
	default:
		c.mu.Unlock()
		slog.Debug("ws: dropping slow websocket client")
		c.close()
	}
}

func (c *client) writePump() {
	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			slog.Debug("ws: downstream write failed", "error", err)
			c.close()
			return
		}
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.send)
		c.mu.Unlock()
		if err := c.conn.Close(); err != nil {
			slog.Debug("ws: close connection", "error", err)
		}
	})
}
