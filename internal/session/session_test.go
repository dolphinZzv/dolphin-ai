package session

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewManager(t *testing.T) {
	Convey("NewManager", t, func() {
		store := NewFileStore(t.TempDir())
		mgr := NewManager(store)
		So(mgr, ShouldNotBeNil)
	})
}

func TestManagerCreate(t *testing.T) {
	Convey("Manager.Create", t, func() {
		store := NewFileStore(t.TempDir())
		mgr := NewManager(store)
		ctx := context.Background()

		Convey("creates a new session", func() {
			sess := mgr.Create(ctx)
			So(sess, ShouldNotBeNil)
			So(sess.ID, ShouldNotBeEmpty)
			So(sess.Active, ShouldBeTrue)
			So(sess.CreatedAt.IsZero(), ShouldBeFalse)
		})

		Convey("sets session as active", func() {
			sess := mgr.Create(ctx)
			So(mgr.Active(), ShouldEqual, sess)
		})

		Convey("replaces previous active session", func() {
			s1 := mgr.Create(ctx)
			So(s1.Active, ShouldBeTrue)

			mgr.Create(ctx)
			So(mgr.Active().ID, ShouldNotEqual, s1.ID)

			s1Reloaded, _ := store.Get(ctx, s1.ID)
			So(s1Reloaded.Active, ShouldBeFalse)
		})
	})
}

func TestManagerActive(t *testing.T) {
	Convey("Manager.Active", t, func() {
		store := NewFileStore(t.TempDir())
		mgr := NewManager(store)
		ctx := context.Background()

		Convey("returns nil when no session created", func() {
			So(mgr.Active(), ShouldBeNil)
		})

		Convey("returns the active session", func() {
			sess := mgr.Create(ctx)
			So(mgr.Active(), ShouldEqual, sess)
		})
	})
}

func TestManagerList(t *testing.T) {
	Convey("Manager.List", t, func() {
		store := NewFileStore(t.TempDir())
		mgr := NewManager(store)
		ctx := context.Background()

		Convey("lists all sessions", func() {
			s1 := mgr.Create(ctx)
			mgr.Create(ctx)

			sessions, err := mgr.List(ctx)
			So(err, ShouldBeNil)
			So(len(sessions), ShouldEqual, 2)
			So(sessions[0].ID, ShouldEqual, s1.ID)
		})
	})
}

func TestManagerSwitchTo(t *testing.T) {
	Convey("Manager.SwitchTo", t, func() {
		store := NewFileStore(t.TempDir())
		mgr := NewManager(store)
		ctx := context.Background()

		Convey("switches to an existing session", func() {
			s1 := mgr.Create(ctx)
			mgr.Create(ctx)

			switched, err := mgr.SwitchTo(ctx, s1.ID)
			So(err, ShouldBeNil)
			So(switched.ID, ShouldEqual, s1.ID)
			So(switched.Active, ShouldBeTrue)
			So(mgr.Active().ID, ShouldEqual, s1.ID)
		})

		Convey("returns error for nonexistent session", func() {
			_, err := mgr.SwitchTo(ctx, "nonexistent")
			So(err, ShouldNotBeNil)
		})
	})
}

func TestManagerOnFliped(t *testing.T) {
	Convey("Manager.OnFliped", t, func() {
		store := NewFileStore(t.TempDir())
		mgr := NewManager(store)
		ctx := context.Background()

		Convey("calls callback on session switch", func() {
			var flippedID string
			mgr.OnFliped(func(ctx context.Context, sessionID string) {
				flippedID = sessionID
			})

			s1 := mgr.Create(ctx)
			mgr.Create(ctx)
			mgr.SwitchTo(ctx, s1.ID)
			So(flippedID, ShouldEqual, s1.ID)
		})
	})
}

func TestSessionStruct(t *testing.T) {
	Convey("Session struct fields", t, func() {
		Convey("zero value has correct defaults", func() {
			var s Session
			So(s.ID, ShouldEqual, "")
			So(s.Title, ShouldEqual, "")
			So(s.Active, ShouldBeFalse)
			So(s.CreatedAt.IsZero(), ShouldBeTrue)
		})
	})
}
