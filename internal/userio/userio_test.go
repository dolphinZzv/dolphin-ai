package userio

import (
	"context"
	"sync"
	"testing"

	"dolphin/internal/agentio"
	"dolphin/internal/command"
	"dolphin/internal/common"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

type interactiveTransport struct {
	id      string
	readCh  chan string
	writeCh chan string
	mu      sync.Mutex
}

func newInteractiveTransport(id string) *interactiveTransport {
	return &interactiveTransport{
		id:      id,
		readCh:  make(chan string, 10),
		writeCh: make(chan string, 10),
	}
}

func (t *interactiveTransport) ID() string               { return t.id }
func (t *interactiveTransport) Context() string          { return "" }
func (t *interactiveTransport) Tools() []common.ToolDesc { return nil }
func (t *interactiveTransport) Flush() error             { return nil }
func (t *interactiveTransport) Close() error             { return nil }
func (t *interactiveTransport) Capability() transport.Capability {
	return transport.Capability{Interactive: true, Streamable: true, NestRead: true}
}

func (t *interactiveTransport) Read(ctx context.Context) (string, error) {
	select {
	case msg := <-t.readCh:
		return msg, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (t *interactiveTransport) Write(ctx context.Context, text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writeCh <- text
	return nil
}

func (t *interactiveTransport) queueRead(msg string) {
	t.readCh <- msg
}

func (t *interactiveTransport) lastWrite(ctx context.Context) string {
	select {
	case msg := <-t.writeCh:
		return msg
	case <-ctx.Done():
		return ""
	}
}

var _ transport.IO = (*interactiveTransport)(nil)

func TestUserIO(t *testing.T) {
	Convey("UserIO", t, func() {
		logger, _ := zap.NewDevelopment()
		sb := signal.NewBus()
		store := session.NewFileStore(t.TempDir())
		mgr := session.NewManager(store)
		cmdReg := command.NewRegistry(mgr, sb)
		aio := agentio.NewAgentIO(10, mgr, sb, logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, mgr)

		Convey("NewUserIO creates instance", func() {
			So(uio, ShouldNotBeNil)
		})

		Convey("Handle routes / commands to cobra", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()

			uio.Handle(ctx, tio, "/version")
			uio.Handle(ctx, tio, "/session new")

			sess := mgr.Active()
			So(sess, ShouldNotBeNil)
			So(sess.Active, ShouldBeTrue)
		})

		Convey("Handle sends normal input to agent IO", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test", Type: "stdio"})

			uio.Handle(ctx, tio, "/session new")
			uio.Handle(ctx, tio, "hello world")

			So(len(aio.Queue()), ShouldEqual, 1)
		})

		Convey("Handle creates detached session without affecting active", func() {
			tio := transport.NewNullTransport("test")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test", Type: "null"})

			So(mgr.Active(), ShouldBeNil)

			uio.Handle(ctx, tio, "create detached session")

			So(mgr.Active(), ShouldBeNil)
		})

		Convey("Confirm returns error for non-interactive transport", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()

			_, err := uio.Confirm(ctx, tio, "confirm?")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "does not support interactive")
		})

		Convey("Select returns error for non-interactive transport", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()

			_, err := uio.Select(ctx, tio, []string{"a", "b"})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "does not support interactive")
		})

		Convey("Confirm interactive returns true for y/yes", func() {
			tio := newInteractiveTransport("interactive")
			ctx := context.Background()

			Convey("returns true for 'y'", func() {
				tio.queueRead("y")
				result, err := uio.Confirm(ctx, tio, "proceed?")
				So(err, ShouldBeNil)
				So(result, ShouldBeTrue)

				prompt := tio.lastWrite(ctx)
				So(prompt, ShouldContainSubstring, "proceed?")
				So(prompt, ShouldContainSubstring, "(y/n)")
			})

			Convey("returns true for 'yes'", func() {
				tio.queueRead("yes")
				result, err := uio.Confirm(ctx, tio, "proceed?")
				So(err, ShouldBeNil)
				So(result, ShouldBeTrue)
			})

			Convey("returns false for 'n'", func() {
				tio.queueRead("n")
				result, err := uio.Confirm(ctx, tio, "proceed?")
				So(err, ShouldBeNil)
				So(result, ShouldBeFalse)
			})
		})

		Convey("Select interactive returns correct index", func() {
			tio := newInteractiveTransport("interactive")
			ctx := context.Background()

			tio.queueRead("2")
			idx, err := uio.Select(ctx, tio, []string{"first", "second", "third"})
			So(err, ShouldBeNil)
			So(idx, ShouldEqual, 2)

			So(tio.lastWrite(ctx), ShouldEqual, "1. first")
			So(tio.lastWrite(ctx), ShouldEqual, "2. second")
			So(tio.lastWrite(ctx), ShouldEqual, "3. third")
		})
	})
}
