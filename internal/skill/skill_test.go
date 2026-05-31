package skill

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewFileStore(t *testing.T) {
	Convey("NewFileStore", t, func() {
		store := NewFileStore(t.TempDir())
		So(store, ShouldNotBeNil)
	})
}

func TestFileStoreSaveAndGet(t *testing.T) {
	Convey("FileStore Save and Get", t, func() {
		store := NewFileStore(t.TempDir())
		ctx := context.Background()

		Convey("saves and retrieves a skill", func() {
			sk := Skill{
				Name:        "test-skill",
				Description: "A test skill",
				Prompt:      "You are a test skill",
				Enabled:     true,
			}
			err := store.Save(ctx, sk)
			So(err, ShouldBeNil)

			got, err := store.Get(ctx, "test-skill")
			So(err, ShouldBeNil)
			So(got, ShouldNotBeNil)
			So(got.Name, ShouldEqual, "test-skill")
			So(got.Description, ShouldEqual, "A test skill")
			So(got.Enabled, ShouldBeTrue)
		})

		Convey("returns error for nonexistent skill", func() {
			_, err := store.Get(ctx, "nonexistent")
			So(err, ShouldNotBeNil)
		})

		Convey("refuses to save empty name", func() {
			err := store.Save(ctx, Skill{Name: ""})
			So(err, ShouldNotBeNil)
		})
	})
}

func TestFileStoreList(t *testing.T) {
	Convey("FileStore List", t, func() {
		store := NewFileStore(t.TempDir())
		ctx := context.Background()

		Convey("returns empty list when no skills", func() {
			skills, err := store.List(ctx)
			So(err, ShouldBeNil)
			So(skills, ShouldBeEmpty)
		})

		Convey("lists all saved skills", func() {
			store.Save(ctx, Skill{Name: "alpha", Prompt: "a"})
			store.Save(ctx, Skill{Name: "beta", Prompt: "b"})

			skills, err := store.List(ctx)
			So(err, ShouldBeNil)
			So(len(skills), ShouldEqual, 2)
		})
	})
}

func TestFileStoreDelete(t *testing.T) {
	Convey("FileStore Delete", t, func() {
		store := NewFileStore(t.TempDir())
		ctx := context.Background()

		Convey("deletes an existing skill", func() {
			store.Save(ctx, Skill{Name: "to-delete", Prompt: "x"})
			err := store.Delete(ctx, "to-delete")
			So(err, ShouldBeNil)

			_, err = store.Get(ctx, "to-delete")
			So(err, ShouldNotBeNil)
		})
	})
}

func TestFileStoreSearch(t *testing.T) {
	Convey("FileStore Search", t, func() {
		store := NewFileStore(t.TempDir())
		ctx := context.Background()

		Convey("finds skills matching query in name", func() {
			store.Save(ctx, Skill{Name: "code-review", Description: "Review code", Prompt: "x"})
			store.Save(ctx, Skill{Name: "commit-message", Description: "Write commits", Prompt: "x"})

			results, err := store.Search(ctx, "code")
			So(err, ShouldBeNil)
			So(len(results), ShouldEqual, 1)
			So(results[0].Name, ShouldEqual, "code-review")
		})

		Convey("finds skills matching query in description", func() {
			store.Save(ctx, Skill{Name: "helper", Description: "helper tool", Prompt: "x"})

			results, err := store.Search(ctx, "helper")
			So(err, ShouldBeNil)
			So(len(results), ShouldEqual, 1)
		})

		Convey("returns empty when no match", func() {
			store.Save(ctx, Skill{Name: "test", Prompt: "x"})
			results, _ := store.Search(ctx, "zzz")
			So(results, ShouldBeEmpty)
		})
	})
}

func TestSkillStruct(t *testing.T) {
	Convey("Skill struct", t, func() {
		sk := Skill{
			Name:        "my-skill",
			Description: "does stuff",
			Prompt:      "prompt text",
			Tools:       []string{"tool1"},
			Enabled:     true,
			Commands:    []string{"cmd1"},
		}
		So(sk.Name, ShouldEqual, "my-skill")
		So(sk.Description, ShouldEqual, "does stuff")
		So(sk.Prompt, ShouldEqual, "prompt text")
		So(sk.Tools, ShouldResemble, []string{"tool1"})
		So(sk.Enabled, ShouldBeTrue)
		So(sk.Commands, ShouldResemble, []string{"cmd1"})
	})
}
