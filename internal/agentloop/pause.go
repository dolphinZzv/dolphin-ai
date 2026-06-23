package agentloop

import (
	"time"

	"dolphin/internal/signal"
)

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

// backoffSleep waits for d or an interrupt signal, whichever comes first. It
// is used by the LLM retry loop to back off after a 429/5xx.
//
// During the backoff, a Pause signal does NOT abort the wait — it suspends
// it: the function blocks on pauseOnSignal until Resume (so the user can
// pause a rate-limited retry just like any other step). After Resume the
// backoff is treated as elapsed and the retry proceeds. Interrupt/Cancel
// abort immediately and are reported via the returned bool.
//
// Returns true if the sleep was aborted by an Interrupt/Cancel signal (the
// caller should propagate ErrInterrupted); false if the full duration
// elapsed or was resumed after a Pause.
//
// sigCh may be nil (no signal bus wired); the sleep then just waits on the
// timer.
func backoffSleep(sigCh <-chan signal.Signal, d time.Duration) bool {
	if d <= 0 {
		return false
	}
	t := time.NewTimer(d)
	defer t.Stop()
	if sigCh == nil {
		<-t.C
		return false
	}
	for {
		select {
		case <-t.C:
			return false
		case sig, ok := <-sigCh:
			if !ok {
				return false
			}
			switch sig { //nolint:exhaustive // only Interrupt/Cancel/Pause need action; other signals ignored.
			case signal.Interrupt, signal.Cancel:
				return true
			case signal.Pause:
				// Suspend the backoff until Resume/Interrupt. The pause
				// already extended the wall-clock delay, so after Resume we
				// proceed to the retry rather than resuming the remaining
				// backoff — that's the intent of a user-initiated pause.
				if r := pauseOnSignal(sigCh); r == signal.Interrupt || r == signal.Cancel {
					return true
				}
				return false
			}
		}
	}
}
