package ws

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	websocket "github.com/gofiber/contrib/websocket"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/collector"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/profile"
)

type fakeConn struct {
	read       chan []byte
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.Mutex
	writes     [][]byte
	closeFrame bool
}

func newFakeConn() *fakeConn {
	return &fakeConn{read: make(chan []byte, 16), done: make(chan struct{})}
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	select {
	case data := <-f.read:
		return websocket.TextMessage, data, nil
	case <-f.done:
		return 0, nil, errors.New("closed")
	}
}

func (f *fakeConn) WriteMessage(_ int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.done:
		return errors.New("closed")
	default:
	}
	f.writes = append(f.writes, append([]byte(nil), data...))
	return nil
}

func (f *fakeConn) SetReadLimit(int64) {}

func (f *fakeConn) WriteControl(messageType int, _ []byte, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if messageType == websocket.CloseMessage {
		f.closeFrame = true
	}
	return nil
}

func (f *fakeConn) Close() error {
	f.closeOnce.Do(func() { close(f.done) })
	return nil
}

func waitForEvent(t *testing.T, f *fakeConn, event string) map[string]json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for i, raw := range f.writes {
			var envelope map[string]json.RawMessage
			if json.Unmarshal(raw, &envelope) == nil && string(envelope["event"]) == `"`+event+`"` {
				f.writes = append(f.writes[:i], f.writes[i+1:]...)
				f.mu.Unlock()
				return envelope
			}
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("event %q not received", event)
	return nil
}

func TestHandleConnSendsSystemProfileFirst(t *testing.T) {
	hub := NewHub(&profile.SystemProfile{
		Profile:                  profile.ProfileLite,
		IsLite:                   true,
		RecommendedPollIntervalS: 5,
		StorageLockMode:          "wal",
	})
	conn := newFakeConn()
	go hub.handleConn(conn)

	envelope := waitForEvent(t, conn, eventSystemProfile)
	var payload profile.SystemProfile
	if err := json.Unmarshal(envelope["payload"], &payload); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if payload.Profile != profile.ProfileLite || payload.StorageLockMode != "wal" {
		t.Fatalf("unexpected profile payload: %+v", payload)
	}
	if len(hub.Clients()) != 1 {
		t.Fatalf("expected connection registered, got %d", len(hub.Clients()))
	}

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(hub.Clients()) != 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if len(hub.Clients()) != 0 {
		t.Fatalf("connection not removed from hub")
	}
}

func TestClientActions(t *testing.T) {
	hub := NewHub(&profile.SystemProfile{Profile: profile.ProfilePerformance, RecommendedPollIntervalS: 2})
	c := newClient(hub, newFakeConn())
	hub.SetSnapshot(&collector.HardwareMetrics{Timestamp: time.Now().UTC()})

	c.handleAction([]byte(`{"action":"pause"}`))
	if !c.pausedOrClosed() {
		t.Fatal("pause did not suspend the connection")
	}

	c.handleAction([]byte(`{"action":"resume"}`))
	select {
	case raw := <-c.send:
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil || string(envelope["event"]) != `"`+eventTelemetryTick+`"` {
			t.Fatalf("resume did not emit immediate snapshot: %s (%v)", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("resume did not emit immediate snapshot")
	}
	if c.pausedOrClosed() {
		t.Fatal("resume did not reactivate the connection")
	}

	c.handleAction([]byte(`{"action":"set_rate","rate_ms":1500}`))
	if got := c.currentCadence(); got != 1500*time.Millisecond {
		t.Fatalf("set_rate cadence = %v, want 1.5s", got)
	}
	if remaining := time.Until(c.nextSend); remaining < time.Second || remaining > 2*time.Second {
		t.Fatalf("new rate was not applied to the next send: %v", remaining)
	}

	c.handleAction([]byte(`{"action":"set_rate"}`))
	if got := c.currentCadence(); got != 1500*time.Millisecond {
		t.Fatalf("missing rate_ms changed cadence to %v", got)
	}

	c.handleAction([]byte(`{"action":"set_rate","rate_ms":-5}`))
	if got := c.currentCadence(); got != 1500*time.Millisecond {
		t.Fatalf("negative rate_ms changed cadence to %v", got)
	}

	c.handleAction([]byte(`{"action":"set_rate","rate_ms":0}`))
	if got := c.currentCadence(); got != 2*time.Second {
		t.Fatalf("set_rate 0 did not restore profile cadence: %v", got)
	}

	c.handleAction([]byte(`not-json`))
	c.handleAction([]byte(`{"action":"unknown"}`))
	if c.pausedOrClosed() {
		t.Fatal("invalid upstream payload dropped the connection")
	}
}

func TestBroadcasterDistributesByCadence(t *testing.T) {
	hub := NewHub(&profile.SystemProfile{Profile: profile.ProfilePerformance, RecommendedPollIntervalS: 2})
	due := newClient(hub, newFakeConn())
	pending := newClient(hub, newFakeConn())
	hub.add(due)
	hub.add(pending)

	pending.mu.Lock()
	pending.nextSend = time.Now().Add(time.Hour)
	pending.mu.Unlock()

	b := newBroadcasterWith(hub, func() (*collector.HardwareMetrics, error) {
		return &collector.HardwareMetrics{Timestamp: time.Now().UTC()}, nil
	})
	b.tick()

	if hub.Snapshot() == nil {
		t.Fatal("broadcaster did not store the collected snapshot")
	}
	select {
	case raw := <-due.send:
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil || string(envelope["event"]) != `"`+eventTelemetryTick+`"` {
			t.Fatalf("unexpected due message: %s (%v)", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("due client did not receive telemetry")
	}
	select {
	case <-pending.send:
		t.Fatal("client whose cadence has not elapsed received telemetry")
	default:
	}
}

func TestBroadcasterIntervalAdapts(t *testing.T) {
	hub := NewHub(&profile.SystemProfile{Profile: profile.ProfileLite, RecommendedPollIntervalS: 5})
	b := NewBroadcaster(hub)

	if got := b.interval(); got != 0 {
		t.Fatalf("idle interval = %v, want 0", got)
	}

	c := newClient(hub, newFakeConn())
	hub.add(c)
	if got := b.interval(); got != 5*time.Second {
		t.Fatalf("lite interval = %v, want 5s", got)
	}

	c.setRate(200 * time.Millisecond)
	if got := b.interval(); got != minBroadcastInterval {
		t.Fatalf("interval = %v, want floor %v", got, minBroadcastInterval)
	}

	c.setRate(time.Hour)
	if got := b.interval(); got != 5*time.Second {
		t.Fatalf("interval = %v, want profile cap 5s", got)
	}

	c.mu.Lock()
	c.paused = true
	c.mu.Unlock()
	if got := b.interval(); got != 0 {
		t.Fatalf("paused-only interval = %v, want 0", got)
	}
}

func TestBroadcasterDoesNotStarveOnFrequentNotifications(t *testing.T) {
	hub := NewHub(&profile.SystemProfile{RecommendedPollIntervalS: 1})
	c := newClient(hub, newFakeConn())
	hub.add(c)
	b := newBroadcasterWith(hub, func() (*collector.HardwareMetrics, error) {
		return &collector.HardwareMetrics{Timestamp: time.Now().UTC()}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	until := time.After(1500 * time.Millisecond)
	notify := time.NewTicker(10 * time.Millisecond)
	defer notify.Stop()
	for {
		select {
		case <-notify.C:
			hub.Notify()
		case <-c.send:
			return
		case <-until:
			t.Fatal("frequent hub notifications starved telemetry")
		}
	}
}

func TestResumeWithoutFreshSnapshotRequestsSharedCollection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stale bool
	}{{name: "no snapshot"}, {name: "stale snapshot", stale: true}} {
		t.Run(tc.name, func(t *testing.T) {
			hub := NewHub(&profile.SystemProfile{RecommendedPollIntervalS: 5})
			c := newClient(hub, newFakeConn())
			hub.add(c)
			c.handleAction([]byte(`{"action":"pause"}`))
			if tc.stale {
				hub.SetSnapshot(&collector.HardwareMetrics{Timestamp: time.Now().Add(-time.Hour).UTC()})
			}
			b := newBroadcasterWith(hub, func() (*collector.HardwareMetrics, error) {
				return &collector.HardwareMetrics{Timestamp: time.Now().UTC()}, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { b.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()

			c.handleAction([]byte(`{"action":"resume"}`))
			select {
			case raw := <-c.send:
				var envelope struct {
					Payload collector.HardwareMetrics `json:"payload"`
				}
				if err := json.Unmarshal(raw, &envelope); err != nil || time.Since(envelope.Payload.Timestamp) > time.Second {
					t.Fatalf("resume sent a stale snapshot: %s (%v)", raw, err)
				}
			case <-time.After(time.Second):
				t.Fatal("resume did not request immediate collection")
			}
		})
	}
}

func TestCloseAllClosesWebSocketSessions(t *testing.T) {
	hub := NewHub(&profile.SystemProfile{RecommendedPollIntervalS: 5})
	conn := newFakeConn()
	hub.add(newClient(hub, conn))
	hub.CloseAll()
	if len(hub.Clients()) != 0 {
		t.Fatal("hub kept sessions after shutdown")
	}
	conn.mu.Lock()
	closeFrame := conn.closeFrame
	conn.mu.Unlock()
	if !closeFrame {
		t.Fatal("shutdown did not send a normal close frame")
	}
	select {
	case <-conn.done:
	default:
		t.Fatal("shutdown did not close the connection")
	}
	late := newFakeConn()
	if hub.add(newClient(hub, late)) {
		t.Fatal("hub accepted a session after shutdown began")
	}
	select {
	case <-late.done:
	default:
		t.Fatal("hub did not close a late session")
	}
}
