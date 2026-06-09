package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewManager(t *testing.T) {
	Convey("NewManager", t, func() {
		mgr := NewManager(t.TempDir())
		So(mgr, ShouldNotBeNil)
	})
}

func TestManagerCreate(t *testing.T) {
	Convey("Manager.Create", t, func() {
		mgr := NewManager(t.TempDir())
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
			So(s1.Active, ShouldBeFalse)
		})
	})
}

func TestManagerActive(t *testing.T) {
	Convey("Manager.Active", t, func() {
		mgr := NewManager(t.TempDir())
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
		dir := t.TempDir()
		mgr := NewManager(dir)
		ctx := context.Background()

		Convey("returns sessions from .md files on disk", func() {
			s1 := mgr.Create(ctx)
			mgr.Create(ctx)

			// Create .md files on disk (simulating first message write).
			for _, id := range []string{s1.ID, mgr.Active().ID} {
				f, _ := os.Create(filepath.Join(dir, id+".md"))
				_ = f.Close()
			}

			sessions, err := mgr.List(ctx)
			So(err, ShouldBeNil)
			So(len(sessions), ShouldEqual, 2)
		})
	})
}

func TestManagerSwitchTo(t *testing.T) {
	Convey("Manager.SwitchTo", t, func() {
		dir := t.TempDir()
		mgr := NewManager(dir)
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
		mgr := NewManager(t.TempDir())
		ctx := context.Background()

		Convey("calls callback on session switch", func() {
			var flippedID string
			mgr.OnFliped(func(ctx context.Context, sessionID string) {
				flippedID = sessionID
			})

			s1 := mgr.Create(ctx)
			mgr.Create(ctx)
			_, _ = mgr.SwitchTo(ctx, s1.ID)
			So(flippedID, ShouldEqual, s1.ID)
		})
	})
}

func TestSessionStruct(t *testing.T) {
	Convey("Session struct fields", t, func() {
		Convey("zero value has correct defaults", func() {
			var s Session
			So(s.ID, ShouldEqual, "")
			So(s.Active, ShouldBeFalse)
			So(s.CreatedAt.IsZero(), ShouldBeTrue)
		})

		Convey("Set and Get store and retrieve values", func() {
			s := Session{ID: "test-session"}
			s.Set("rounds", 5)
			s.Set("name", "hello")
			s.Set("enabled", true)

			So(s.Get("rounds"), ShouldEqual, 5)
			So(s.Get("name"), ShouldEqual, "hello")
			So(s.Get("enabled"), ShouldEqual, true)
			So(s.Get("nonexistent"), ShouldBeNil)
		})

		Convey("Get returns nil for unset key", func() {
			s := Session{ID: "empty-session"}
			So(s.Get("anything"), ShouldBeNil)
		})
	})
}

func TestManagerNewSession(t *testing.T) {
	Convey("Manager.NewSession", t, func() {
		mgr := NewManager(t.TempDir())

		Convey("creates session with correct defaults", func() {
			s := mgr.NewSession(context.Background())
			So(s, ShouldNotBeNil)
			So(s.ID, ShouldNotBeEmpty)
			So(s.Active, ShouldBeFalse)
			So(s.CreatedAt.IsZero(), ShouldBeFalse)
		})
	})
}

func TestManagerLoadActive(t *testing.T) {
	Convey("Manager.LoadActive", t, func() {
		Convey("loads session from disk", func() {
			dir := t.TempDir()
			mgr := NewManager(dir)
			s := mgr.Create(context.Background())
			s.Set("version", 2)

			f, _ := os.Create(filepath.Join(dir, s.ID+".md"))
			_ = f.Close()

			mgr2 := NewManager(dir)
			mgr2.LoadActive(context.Background())
			So(mgr2.Active(), ShouldNotBeNil)
			So(mgr2.Active().ID, ShouldEqual, s.ID)
		})

		Convey("does nothing when no .md files exist", func() {
			mgr := NewManager(t.TempDir())
			mgr.LoadActive(context.Background())
			So(mgr.Active(), ShouldBeNil)
		})
	})
}

func TestManagerGet(t *testing.T) {
	Convey("Manager.Get", t, func() {
		Convey("retrieves session by ID", func() {
			mgr := NewManager(t.TempDir())
			created := mgr.Create(context.Background())
			created.Set("key", "val")

			found := mgr.Get(created.ID)
			So(found, ShouldNotBeNil)
			So(found.ID, ShouldEqual, created.ID)
			So(found.Get("key"), ShouldEqual, "val")
		})

		Convey("returns nil for unknown ID", func() {
			mgr := NewManager(t.TempDir())
			found := mgr.Get("nonexistent-id")
			So(found, ShouldBeNil)
		})
	})
}

func TestManagerDelete(t *testing.T) {
	Convey("Manager.Delete", t, func() {
		Convey("deletes session", func() {
			mgr := NewManager(t.TempDir())
			s := mgr.Create(context.Background())

			f, _ := os.Create(filepath.Join(mgr.dir, s.ID+".md"))
			_ = f.Close()

			err := mgr.Delete(context.Background(), s.ID)
			So(err, ShouldBeNil)
			So(mgr.Get(s.ID), ShouldBeNil)
		})

		Convey("returns error for nonexistent session", func() {
			mgr := NewManager(t.TempDir())
			err := mgr.Delete(context.Background(), "nonexistent")
			So(err, ShouldNotBeNil)
		})
	})
}

func TestManagerSwitchTo_DiskSession(t *testing.T) {
	Convey("Manager.SwitchTo from disk", t, func() {
		dir := t.TempDir()
		mgr := NewManager(dir)
		s := mgr.Create(context.Background())

		f, _ := os.Create(filepath.Join(dir, s.ID+".md"))
		_ = f.Close()

		mgr2 := NewManager(dir)
		switched, err := mgr2.SwitchTo(context.Background(), s.ID)
		So(err, ShouldBeNil)
		So(switched.ID, ShouldEqual, s.ID)
		So(switched.Active, ShouldBeTrue)
		So(mgr2.Active().ID, ShouldEqual, s.ID)
	})
}
