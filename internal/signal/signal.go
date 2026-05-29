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
	sendCh  chan Signal
	recvChs []chan Signal
}

type Bus struct {
	mu       sync.RWMutex
	sessions map[string]*sessionChannels
}

func NewBus() *Bus {
	return &Bus{sessions: make(map[string]*sessionChannels)}
}

func (b *Bus) ForSession(sessionID string) (chan<- Signal, <-chan Signal) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sc, ok := b.sessions[sessionID]
	if !ok {
		sc = &sessionChannels{
			sendCh:  make(chan Signal, 1),
			recvChs: nil,
		}
		b.sessions[sessionID] = sc
	}

	recvCh := make(chan Signal, 1)
	sc.recvChs = append(sc.recvChs, recvCh)

	// fan-out: send to all receivers
	sendCh := make(chan Signal, 1)
	go func() {
		for sig := range sendCh {
			sc.sendCh <- sig
			for _, ch := range sc.recvChs {
				select {
				case ch <- sig:
				default:
				}
			}
		}
	}()

	return sendCh, recvCh
}

// Send sends a signal to all subscribers of the given session.
func (b *Bus) Send(sessionID string, sig Signal) {
	b.mu.RLock()
	sc, ok := b.sessions[sessionID]
	b.mu.RUnlock()
	if !ok {
		return
	}

	sc.sendCh <- sig
}

// Subscribe returns a channel for receiving signals for a session.
func (b *Bus) Subscribe(sessionID string) <-chan Signal {
	b.mu.Lock()
	defer b.mu.Unlock()

	sc, ok := b.sessions[sessionID]
	if !ok {
		sc = &sessionChannels{
			sendCh:  make(chan Signal, 1),
			recvChs: nil,
		}
		b.sessions[sessionID] = sc
	}

	ch := make(chan Signal, 1)
	sc.recvChs = append(sc.recvChs, ch)
	return ch
}

// Delete removes all channels for a session.
func (b *Bus) Delete(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sc, ok := b.sessions[sessionID]; ok {
		close(sc.sendCh)
		for _, ch := range sc.recvChs {
			close(ch)
		}
		delete(b.sessions, sessionID)
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
