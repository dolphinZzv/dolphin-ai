// Package actor provides a named-actor group for concurrent lifecycle management.
// An ActorGroup runs multiple actors concurrently and stops all when any exits.
package actor

import "sync"

// Actor is a named runnable with an interrupt function.
// Execute runs the actor; it must return when Interrupt is called.
// Interrupt must be safe to call even after Execute has returned.
type Actor struct {
	Name      string
	Execute   func() error
	Interrupt func(error)
}

// ActorGroup manages a set of named actors with thread-safe add/remove and
// automatic shutdown of all actors when any one exits.
type ActorGroup struct {
	mu     sync.Mutex
	actors []Actor
}

// Add appends an actor to the group. Duplicate names are silently ignored.
// Safe to call before or during Run (though during-Run add is a no-op in the
// current implementation; the actor will run on the next call to Run).
func (g *ActorGroup) Add(a Actor) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, existing := range g.actors {
		if existing.Name == a.Name {
			return
		}
	}
	g.actors = append(g.actors, a)
}

// Remove interrupts and removes an actor by name. Returns false if not found.
func (g *ActorGroup) Remove(name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, a := range g.actors {
		if a.Name == name {
			a.Interrupt(nil)
			g.actors = append(g.actors[:i], g.actors[i+1:]...)
			return true
		}
	}
	return false
}

// Run starts all actors concurrently and blocks until one exits.
// All remaining actors are interrupted and Run waits for them to finish.
// Returns the error from the first actor that exited.
// Safe to call only once; subsequent calls return nil.
func (g *ActorGroup) Run() error {
	g.mu.Lock()
	if len(g.actors) == 0 {
		g.mu.Unlock()
		return nil
	}
	actors := make([]Actor, len(g.actors))
	copy(actors, g.actors)
	// Prevent re-entry by clearing the list.
	g.actors = nil
	g.mu.Unlock()

	errCh := make(chan error, len(actors))
	for _, a := range actors {
		a := a
		go func() {
			errCh <- a.Execute()
		}()
	}

	firstErr := <-errCh

	// Interrupt all remaining actors.
	for _, a := range actors {
		a.Interrupt(firstErr)
	}

	// Drain remaining results.
	for i := 1; i < len(actors); i++ {
		<-errCh
	}

	return firstErr
}
