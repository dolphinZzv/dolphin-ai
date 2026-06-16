package agentloop

import "dolphin/internal/signal"

// pauseOnSignal blocks until Resume, Interrupt, or Cancel is received.
// Returns the received signal so callers can propagate Interrupt.
func pauseOnSignal(sigCh <-chan signal.Signal) signal.Signal {
	for {
		sig, ok := <-sigCh
		if !ok {
			return 0
		}
		if sig == signal.Resume || sig == signal.Interrupt || sig == signal.Cancel {
			return sig
		}
	}
}
