package session

import (
	"context"
	"encoding/json"
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

		Convey("persists session as JSON file on create", func() {
			s := mgr.Create(ctx)
			jsonPath := filepath.Join(mgr.dir, s.ID+".json")
			_, err := os.Stat(jsonPath)
			So(err, ShouldBeNil)
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

		Convey("returns sessions from .json files on disk", func() {
			s1 := mgr.Create(ctx)
			mgr.Create(ctx)

			mgr2 := NewManager(dir)
			sessions, err := mgr2.List(ctx)
			So(err, ShouldBeNil)
			So(len(sessions), ShouldEqual, 2)
			ids := make(map[string]bool)
			for _, s := range sessions {
				ids[s.ID] = true
			}
			So(ids[s1.ID], ShouldBeTrue)
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
			s1.Set("rounds", 3)
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

		Convey("saves current session before switching", func() {
			s1 := mgr.Create(ctx)
			s1.Set("rounds", 5)
			mgr.Create(ctx)

			// Load from disk to verify s1 was saved with its data.
			mgr2 := NewManager(dir)
			sess := mgr2.Get(s1.ID)
			// If Get returns nil, try loading from json.
			if sess == nil {
				raw, err := os.ReadFile(filepath.Join(dir, s1.ID+".json"))
				So(err, ShouldBeNil)
				err = json.Unmarshal(raw, &sess)
				So(err, ShouldBeNil)
			}
			So(sess.Get("rounds"), ShouldEqual, 5)
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

		Convey("persists session as JSON file", func() {
			s := mgr.NewSession(context.Background())
			jsonPath := filepath.Join(mgr.dir, s.ID+".json")
			_, err := os.Stat(jsonPath)
			So(err, ShouldBeNil)
		})
	})
}

func TestManagerLoadActive(t *testing.T) {
	Convey("Manager.LoadActive", t, func() {
		Convey("loads session from .json file on disk", func() {
			dir := t.TempDir()
			mgr := NewManager(dir)
			s := mgr.Create(context.Background())
			s.Set("version", 2)

			mgr2 := NewManager(dir)
			mgr2.LoadActive(context.Background())
			So(mgr2.Active(), ShouldNotBeNil)
			So(mgr2.Active().ID, ShouldEqual, s.ID)
			So(mgr2.Active().Get("version"), ShouldEqual, 2)
		})

		Convey("does nothing when no files exist", func() {
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
		Convey("deletes session and removes .json file", func() {
			mgr := NewManager(t.TempDir())
			s := mgr.Create(context.Background())

			err := mgr.Delete(context.Background(), s.ID)
			So(err, ShouldBeNil)
			So(mgr.Get(s.ID), ShouldBeNil)

			_, err = os.Stat(filepath.Join(mgr.dir, s.ID+".json"))
			So(os.IsNotExist(err), ShouldBeTrue)
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

		mgr2 := NewManager(dir)
		switched, err := mgr2.SwitchTo(context.Background(), s.ID)
		So(err, ShouldBeNil)
		So(switched.ID, ShouldEqual, s.ID)
		So(switched.Active, ShouldBeTrue)
		So(mgr2.Active().ID, ShouldEqual, s.ID)
	})
}

func TestSessionJSONPersistence(t *testing.T) {
	Convey("Session JSON persistence", t, func() {
		Convey("data survives Manager restart via JSON files", func() {
			dir := t.TempDir()
			mgr1 := NewManager(dir)
			s1 := mgr1.Create(context.Background())
			s1.Set("rounds", 10)
			s1.Set("input_tokens", 500)
			s1.Set("output_tokens", 300)

			// Simulate restart: new Manager with same dir.
			mgr2 := NewManager(dir)
			mgr2.LoadActive(context.Background())
			active := mgr2.Active()
			So(active, ShouldNotBeNil)
			So(active.ID, ShouldEqual, s1.ID)
			So(active.Get("rounds"), ShouldEqual, 10)
			So(active.Get("input_tokens"), ShouldEqual, 500)
			So(active.Get("output_tokens"), ShouldEqual, 300)
		})

		Convey("Set triggers JSON file update", func() {
			dir := t.TempDir()
			mgr := NewManager(dir)
			s := mgr.Create(context.Background())
			s.Set("rounds", 1)

			raw, err := os.ReadFile(filepath.Join(dir, s.ID+".json"))
			So(err, ShouldBeNil)
			var loaded Session
			err = json.Unmarshal(raw, &loaded)
			So(err, ShouldBeNil)
			So(loaded.Get("rounds"), ShouldEqual, 1)
		})

		Convey("SaveActive persists current session", func() {
			dir := t.TempDir()
			mgr := NewManager(dir)
			s := mgr.Create(context.Background())
			s.Set("status", "active")

			mgr.SaveActive()

			raw, err := os.ReadFile(filepath.Join(dir, s.ID+".json"))
			So(err, ShouldBeNil)
			var loaded Session
			err = json.Unmarshal(raw, &loaded)
			So(err, ShouldBeNil)
			So(loaded.Get("status"), ShouldEqual, "active")
		})
	})
}
