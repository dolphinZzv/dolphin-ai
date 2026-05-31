package signal

import (
	"context"
	"sync"
)

type Signal int

const (
	Interrupt Signal = iota
	Continue
	Cancel
	Pause
	Resume
)

type sessionChannels struct {
	mu      sync.Mutex
	sendCh  chan Signal
	recvChs []chan Signal
	closed  bool
	started bool
}

// fanOutLoop forwards signals from sendCh to all receivers.
func (sc *sessionChannels) fanOutLoop() {
	for sig := range sc.sendCh {
		sc.mu.Lock()
		for _, ch := range sc.recvChs {
			select {
			case ch <- sig:
			default:
			}
		}
		sc.mu.Unlock()
	}
}

// ensureRunning starts the fan-out goroutine if not already running.
func (sc *sessionChannels) ensureRunning() {
	sc.mu.Lock()
	if sc.started {
		sc.mu.Unlock()
		return
	}
	sc.started = true
	sc.mu.Unlock()
	go sc.fanOutLoop()
}

// Bus dispatches signals to session subscribers.
type Bus struct {
	mu       sync.RWMutex
	sessions map[string]*sessionChannels
}

func NewBus() *Bus {
	return &Bus{sessions: make(map[string]*sessionChannels)}
}

// Send sends a signal to all subscribers of the given session.
func (b *Bus) Send(sessionID string, sig Signal) {
	b.mu.RLock()
	sc, ok := b.sessions[sessionID]
	b.mu.RUnlock()
	if !ok {
		return
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.closed {
		return
	}
	for _, ch := range sc.recvChs {
		select {
		case ch <- sig:
		default:
		}
	}
}

// Subscribe returns a channel for receiving signals for a session.
func (b *Bus) Subscribe(sessionID string) <-chan Signal {
	b.mu.Lock()
	sc, ok := b.sessions[sessionID]
	if !ok {
		sc = &sessionChannels{
			sendCh: make(chan Signal, 1),
		}
		b.sessions[sessionID] = sc
	}
	b.mu.Unlock()
	sc.ensureRunning()

	ch := make(chan Signal, 1)
	sc.mu.Lock()
	sc.recvChs = append(sc.recvChs, ch)
	sc.mu.Unlock()
	return ch
}

// ForSession returns a send channel and receive channel for a session.
// The send channel queues one signal; the receive channel delivers it.
// Multiple calls for the same session share the same send channel.
func (b *Bus) ForSession(sessionID string) (chan<- Signal, <-chan Signal) {
	b.mu.Lock()
	sc, ok := b.sessions[sessionID]
	if !ok {
		sc = &sessionChannels{
			sendCh: make(chan Signal, 1),
		}
		b.sessions[sessionID] = sc
	}
	b.mu.Unlock()
	sc.ensureRunning()

	recvCh := make(chan Signal, 1)
	sc.mu.Lock()
	sc.recvChs = append(sc.recvChs, recvCh)
	sc.mu.Unlock()
	return sc.sendCh, recvCh
}

// Unsubscribe removes a specific subscriber channel from the session.
// The channel is closed after removal.
func (b *Bus) Unsubscribe(sessionID string, ch <-chan Signal) {
	b.mu.RLock()
	sc, ok := b.sessions[sessionID]
	b.mu.RUnlock()
	if !ok {
		return
	}
	sc.mu.Lock()
	for i, c := range sc.recvChs {
		if c == ch {
			sc.recvChs = append(sc.recvChs[:i], sc.recvChs[i+1:]...)
			close(c)
			break
		}
	}
	sc.mu.Unlock()
}

// Delete removes all channels for a session and marks it closed.
func (b *Bus) Delete(sessionID string) {
	b.mu.Lock()
	sc, ok := b.sessions[sessionID]
	if ok {
		delete(b.sessions, sessionID)
	}
	b.mu.Unlock()

	if ok {
		close(sc.sendCh)
		sc.mu.Lock()
		sc.closed = true
		for _, ch := range sc.recvChs {
			close(ch)
		}
		sc.recvChs = nil
		sc.mu.Unlock()
	}
}

// Recv blocks until a signal is received on the channel, respecting context.
func Recv(ctx context.Context, ch <-chan Signal) (Signal, error) {
	select {
	case sig, ok := <-ch:
		if !ok {
			return 0, context.Canceled
		}
		return sig, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
