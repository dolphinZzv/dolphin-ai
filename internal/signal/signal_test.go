package signal

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewBus(t *testing.T) {
	bus := NewBus()
	if bus == nil {
		t.Fatal("NewBus() returned nil")
	}
}

func TestForSessionReturnsChannels(t *testing.T) {
	bus := NewBus()
	sendCh, recvCh := bus.ForSession("sess1")
	if sendCh == nil {
		t.Error("ForSession returned nil send channel")
	}
	if recvCh == nil {
		t.Error("ForSession returned nil recv channel")
	}
}

func TestForSessionSendAndRecv(t *testing.T) {
	bus := NewBus()
	sendCh, recvCh := bus.ForSession("sess1")

	sendCh <- Interrupt

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sig, err := Recv(ctx, recvCh)
	if err != nil {
		t.Fatal(err)
	}
	if sig != Interrupt {
		t.Errorf("got %v, want %v", sig, Interrupt)
	}
}

func TestForSessionSingleSignal(t *testing.T) {
	bus := NewBus()
	sendCh, recvCh := bus.ForSession("sess1")

	sendCh <- Continue

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sig, err := Recv(ctx, recvCh)
	if err != nil {
		t.Fatal(err)
	}
	if sig != Continue {
		t.Errorf("got %v, want %v", sig, Continue)
	}
}

func TestAllSignalTypesDelivered(t *testing.T) {
	signals := []Signal{Interrupt, Continue, Cancel, Pause, Resume}
	for _, sig := range signals {
		t.Run("", func(t *testing.T) {
			bus := NewBus()
			sendCh, recvCh := bus.ForSession("sigtest")
			sendCh <- sig

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			got, err := Recv(ctx, recvCh)
			if err != nil {
				t.Fatal(err)
			}
			if got != sig {
				t.Errorf("got %v, want %v", got, sig)
			}
		})
	}
}

func TestSubscribe(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe("sess1")
	if ch == nil {
		t.Error("Subscribe returned nil channel")
	}
}

func TestSendDeliversToAllSubscribers(t *testing.T) {
	bus := NewBus()
	sendCh, recvCh1 := bus.ForSession("sess1")
	recvCh2 := bus.Subscribe("sess1")

	sendCh <- Cancel

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sig1, err := Recv(ctx, recvCh1)
	if err != nil {
		t.Fatal(err)
	}
	if sig1 != Cancel {
		t.Errorf("subscriber 1 got %v, want %v", sig1, Cancel)
	}

	sig2, err := Recv(ctx, recvCh2)
	if err != nil {
		t.Fatal(err)
	}
	if sig2 != Cancel {
		t.Errorf("subscriber 2 got %v, want %v", sig2, Cancel)
	}
}

func TestBusSend(t *testing.T) {
	bus := NewBus()
	bus.Subscribe("sess1")

	// Bus.Send writes to sc.sendCh (buffered channel of 1).
	// The first send should not block.
	done := make(chan struct{})
	go func() {
		bus.Send("sess1", Pause)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Bus.Send blocked unexpectedly")
	}
}

func TestBusSendToNonexistentSession(t *testing.T) {
	bus := NewBus()
	// Should not panic
	bus.Send("nonexistent", Interrupt)
}

func TestDelete(t *testing.T) {
	bus := NewBus()
	_, recvCh := bus.ForSession("sess1")

	bus.Delete("sess1")

	// recvCh should be closed; Recv should return context.Canceled
	_, err := Recv(context.Background(), recvCh)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled on deleted session, got %v", err)
	}
}

func TestDeleteUnknownSession(t *testing.T) {
	bus := NewBus()
	// Should not panic
	bus.Delete("unknown")
}

func TestRecvContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan Signal, 1)
	_, err := Recv(ctx, ch)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRecvClosedChannel(t *testing.T) {
	ch := make(chan Signal, 1)
	close(ch)

	_, err := Recv(context.Background(), ch)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled for closed channel, got %v", err)
	}
}

func TestForSessionMultipleSessions(t *testing.T) {
	bus := NewBus()
	_, recvCh1 := bus.ForSession("sess1")
	_, recvCh2 := bus.ForSession("sess2")

	sendCh1, _ := bus.ForSession("sess1")
	sendCh2, _ := bus.ForSession("sess2")

	sendCh1 <- Continue
	sendCh2 <- Pause

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sig1, _ := Recv(ctx, recvCh1)
	sig2, _ := Recv(ctx, recvCh2)

	if sig1 != Continue {
		t.Errorf("sess1 got %v, want %v", sig1, Continue)
	}
	if sig2 != Pause {
		t.Errorf("sess2 got %v, want %v", sig2, Pause)
	}
}

func TestSignalValues(t *testing.T) {
	if Interrupt != Signal(0) {
		t.Errorf("Interrupt = %v, want 0", Interrupt)
	}
	if Continue != Signal(1) {
		t.Errorf("Continue = %v, want 1", Continue)
	}
	if Cancel != Signal(2) {
		t.Errorf("Cancel = %v, want 2", Cancel)
	}
	if Pause != Signal(3) {
		t.Errorf("Pause = %v, want 3", Pause)
	}
	if Resume != Signal(4) {
		t.Errorf("Resume = %v, want 4", Resume)
	}
}

func TestForSessionReusesSession(t *testing.T) {
	bus := NewBus()

	// First call creates the session
	sendCh1, recvCh1 := bus.ForSession("shared")
	// Second call reuses the session
	_, recvCh2 := bus.ForSession("shared")

	// Send one signal via the first sender; both receivers should get it
	sendCh1 <- Resume

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sig1, err := Recv(ctx, recvCh1)
	if err != nil {
		t.Fatal(err)
	}
	if sig1 != Resume {
		t.Errorf("recvCh1 got %v, want %v", sig1, Resume)
	}

	sig2, err := Recv(ctx, recvCh2)
	if err != nil {
		t.Fatal(err)
	}
	if sig2 != Resume {
		t.Errorf("recvCh2 got %v, want %v", sig2, Resume)
	}
}

func TestSubscribeMultipleTimes(t *testing.T) {
	bus := NewBus()

	ch1 := bus.Subscribe("sess1")
	ch2 := bus.Subscribe("sess1")
	ch3 := bus.Subscribe("sess1")

	sendCh, _ := bus.ForSession("sess1")
	sendCh <- Cancel

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	for i, ch := range []<-chan Signal{ch1, ch2, ch3} {
		sig, err := Recv(ctx, ch)
		if err != nil {
			t.Fatalf("subscriber %d: %v", i, err)
		}
		if sig != Cancel {
			t.Errorf("subscriber %d got %v, want %v", i, sig, Cancel)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := NewBus()

	ch := bus.Subscribe("sess1")
	bus.Unsubscribe("sess1", ch)

	bus.Send("sess1", Interrupt)

	_, err := Recv(context.Background(), ch)
	if err == nil {
		t.Error("expected error on closed channel after unsubscribe")
	}
}

func TestUnsubscribeUnknownSession(t *testing.T) {
	bus := NewBus()
	// Should not panic
	bus.Unsubscribe("nonexistent", make(chan Signal))
}
