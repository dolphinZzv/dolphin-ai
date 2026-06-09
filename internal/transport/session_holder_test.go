package transport

import (
	"context"
	"testing"

	"dolphin/internal/session"
	. "github.com/smartystreets/goconvey/convey"
)

func TestSessionHolderPerTransport(t *testing.T) {
	Convey("SessionHolder in per_transport mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		h := NewSessionHolder(mgr)
		ctx := context.Background()

		Convey("NewSession creates a new session on first call", func() {
			s := h.NewSession(ctx)
			So(s, ShouldNotBeNil)
			So(s.ID, ShouldNotBeEmpty)
		})

		Convey("NewSession returns cached session on subsequent calls", func() {
			s1 := h.NewSession(ctx)
			s2 := h.NewSession(ctx)
			So(s1, ShouldEqual, s2)
		})

		Convey("NewSession does not use the global active session", func() {
			global := mgr.Create(ctx)
			s := h.NewSession(ctx)
			So(s.ID, ShouldNotEqual, global.ID)
		})
	})
}

func TestSessionHolderShared(t *testing.T) {
	Convey("SessionHolder in shared mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		h := NewSessionHolder(mgr)
		h.SetSessionMode(true)
		ctx := context.Background()

		Convey("NewSession returns the global active session", func() {
			global := mgr.Create(ctx)
			s := h.NewSession(ctx)
			So(s, ShouldEqual, global)
			So(s.Active, ShouldBeTrue)
		})

		Convey("NewSession does not cache per-transport", func() {
			s1 := h.NewSession(ctx)
			// Create a new global session
			mgr.Create(ctx)
			s2 := h.NewSession(ctx)
			So(s1.ID, ShouldNotEqual, s2.ID)
		})

		Convey("NewSession creates active session when none exists", func() {
			So(mgr.Active(), ShouldBeNil)
			s := h.NewSession(ctx)
			So(s, ShouldNotBeNil)
			So(s.Active, ShouldBeTrue)
			So(mgr.Active(), ShouldEqual, s)
		})
	})
}

func TestSessionHolderModeSwitch(t *testing.T) {
	Convey("SessionHolder mode switch", t, func() {
		mgr := session.NewManager(t.TempDir())
		h := NewSessionHolder(mgr)
		ctx := context.Background()

		Convey("switching from per_transport to shared after creation", func() {
			// Create a per-transport session first
			s1 := h.NewSession(ctx)

			// Create a global session
			global := mgr.Create(ctx)

			// Switch to shared mode
			h.SetSessionMode(true)
			s2 := h.NewSession(ctx)

			So(s2, ShouldEqual, global)
			So(s2.ID, ShouldNotEqual, s1.ID)
		})

		Convey("switching from shared to per_transport", func() {
			h.SetSessionMode(true)
			mgr.Create(ctx)

			// In shared mode
			h.NewSession(ctx)

			// Switch back to per_transport
			h.SetSessionMode(false)
			s2 := h.NewSession(ctx)

			// per_transport caches after first call
			s3 := h.NewSession(ctx)
			So(s2, ShouldEqual, s3)
		})
	})
}

func TestSessionHolderNoManager(t *testing.T) {
	Convey("SessionHolder without manager", t, func() {
		h := NewSessionHolder(nil)
		ctx := context.Background()

		Convey("per_transport creates standalone session", func() {
			s := h.NewSession(ctx)
			So(s, ShouldNotBeNil)
			So(s.Active, ShouldBeFalse)
		})

		Convey("shared falls through to standalone when no manager", func() {
			h.SetSessionMode(true)
			s := h.NewSession(ctx)
			So(s, ShouldNotBeNil)
		})
	})
}

func TestSessionHolderSetSessionManager(t *testing.T) {
	Convey("SessionHolder.SetSessionManager", t, func() {
		mgr := session.NewManager(t.TempDir())
		ctx := context.Background()

		Convey("replaces the manager", func() {
			h := NewSessionHolder(nil)
			h.SetSessionManager(mgr)
			s := h.NewSession(ctx)
			So(s, ShouldNotBeNil)
			So(s.Active, ShouldBeFalse) // per-transport, not active
		})
	})
}

func TestSessionHolderSession(t *testing.T) {
	Convey("SessionHolder.Session", t, func() {
		Convey("returns nil when no session created", func() {
			h := NewSessionHolder(nil)
			So(h.Session(), ShouldBeNil)
		})

		Convey("returns current session after NewSession", func() {
			h := NewSessionHolder(nil)
			ctx := context.Background()
			s := h.NewSession(ctx)
			So(h.Session(), ShouldEqual, s)
		})
	})
}

func TestSessionHolderSetSession(t *testing.T) {
	Convey("SessionHolder.SetSession", t, func() {
		mgr := session.NewManager(t.TempDir())

		Convey("sets a custom session", func() {
			h := NewSessionHolder(mgr)
			mySession := &session.Session{ID: "custom-id", Active: true}
			h.SetSession(mySession)
			So(h.Session(), ShouldEqual, mySession)
		})

		Convey("can override existing session", func() {
			h := NewSessionHolder(mgr)
			ctx := context.Background()
			h.NewSession(ctx)

			newSession := &session.Session{ID: "replaced"}
			h.SetSession(newSession)
			So(h.Session(), ShouldEqual, newSession)
		})
	})
}
