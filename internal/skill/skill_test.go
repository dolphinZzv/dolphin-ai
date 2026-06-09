package skill

import (
	"context"
	"os"
	"path/filepath"
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

func TestSetAutoCommitter(t *testing.T) {
	store := NewFileStore(t.TempDir())
	store.SetAutoCommitter(&mockCommitter{})
	if store.committer == nil {
		t.Error("expected committer to be set")
	}
}

func TestSeedDefaults(t *testing.T) {
	Convey("SeedDefaults", t, func() {
		ctx := context.Background()
		store := NewFileStore(t.TempDir())

		SeedDefaults(ctx, store)

		Convey("creates skills-creator skill", func() {
			sk, err := store.Get(ctx, "skills-creator")
			So(err, ShouldBeNil)
			So(sk, ShouldNotBeNil)
			So(sk.Name, ShouldEqual, "skills-creator")
			So(sk.Enabled, ShouldBeTrue)
		})

		Convey("does not overwrite existing skill", func() {
			sk, _ := store.Get(ctx, "skills-creator")
			sk.Enabled = false
			store.Save(ctx, *sk)

			SeedDefaults(ctx, store)

			sk2, _ := store.Get(ctx, "skills-creator")
			So(sk2.Enabled, ShouldBeFalse)
		})
	})
}

type mockCommitter struct{}

func (m *mockCommitter) AutoCommit(_ context.Context, _ string) {}

func TestMigrateFlatFiles(t *testing.T) {
	Convey("migrateFlatFiles", t, func() {
		Convey("converts flat .md file to directory layout", func() {
			dir := t.TempDir()
			content := "---\nname: my-skill\ndescription: test\nenabled: true\n---\nHello world\n"
			os.WriteFile(filepath.Join(dir, "my-skill.md"), []byte(content), 0644)

			migrateFlatFiles(dir)

			_, err := os.Stat(filepath.Join(dir, "my-skill.md"))
			So(os.IsNotExist(err), ShouldBeTrue)

			skillDir := filepath.Join(dir, "my-skill")
			_, err = os.Stat(skillDir)
			So(err, ShouldBeNil)

			sk, err := readFile(filepath.Join(skillDir, "SKILL.md"))
			So(err, ShouldBeNil)
			So(sk.Name, ShouldEqual, "my-skill")
			So(sk.Description, ShouldEqual, "test")

			_, err = os.Stat(filepath.Join(skillDir, "metadata.json"))
			So(err, ShouldBeNil)
		})

		Convey("removes index.md during migration", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "index.md"), []byte("# old index"), 0644)
			os.WriteFile(filepath.Join(dir, "test.md"), []byte("---\nname: test\n---\nbody"), 0644)

			migrateFlatFiles(dir)

			_, err := os.Stat(filepath.Join(dir, "index.md"))
			So(os.IsNotExist(err), ShouldBeTrue)
		})

		Convey("skips files when subdirectory already exists", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "existing.md"), []byte("---\nname: existing\n---\nbody"), 0644)
			os.MkdirAll(filepath.Join(dir, "existing"), 0755)
			os.WriteFile(filepath.Join(dir, "existing", "SENTINEL"), []byte("keep"), 0644)

			migrateFlatFiles(dir)

			_, err := os.Stat(filepath.Join(dir, "existing.md"))
			So(os.IsNotExist(err), ShouldBeTrue)
			_, err = os.Stat(filepath.Join(dir, "existing", "SENTINEL"))
			So(err, ShouldBeNil)
		})

		Convey("does nothing when dir does not exist", func() {
			migrateFlatFiles("/tmp/nonexistent-skill-dir-12345")
		})
	})
}

func TestFileStoreDirNotExist(t *testing.T) {
	Convey("FileStore with non-existent dir", t, func() {
		store := &FileStore{dir: "/tmp/nonexistent-filestore-12345"}

		Convey("List returns nil, nil", func() {
			skills, err := store.List(context.Background())
			So(err, ShouldBeNil)
			So(skills, ShouldBeNil)
		})
	})
}

func TestReadFile(t *testing.T) {
	Convey("readFile", t, func() {
		Convey("parses YAML frontmatter and body", func() {
			dir := t.TempDir()
			content := "---\nname: my-skill\ndescription: a test\nenabled: true\n---\nThis is the prompt\n"
			path := filepath.Join(dir, "SKILL.md")
			os.WriteFile(path, []byte(content), 0644)

			sk, err := readFile(path)
			So(err, ShouldBeNil)
			So(sk.Name, ShouldEqual, "my-skill")
			So(sk.Description, ShouldEqual, "a test")
			So(sk.Enabled, ShouldBeTrue)
			So(sk.Prompt, ShouldEqual, "This is the prompt")
		})

		Convey("infers name from parent dir when frontmatter has no name", func() {
			dir := t.TempDir()
			skillDir := filepath.Join(dir, "inferred-skill")
			os.MkdirAll(skillDir, 0755)
			content := "---\ndescription: no name in frontmatter\n---\nbody\n"
			path := filepath.Join(skillDir, "SKILL.md")
			os.WriteFile(path, []byte(content), 0644)

			sk, err := readFile(path)
			So(err, ShouldBeNil)
			So(sk.Name, ShouldEqual, "inferred-skill")
		})

		Convey("treats file without frontmatter as prompt-only", func() {
			dir := t.TempDir()
			skillDir := filepath.Join(dir, "prompt-only")
			os.MkdirAll(skillDir, 0755)
			path := filepath.Join(skillDir, "SKILL.md")
			os.WriteFile(path, []byte("Just a prompt"), 0644)

			sk, err := readFile(path)
			So(err, ShouldBeNil)
			So(sk.Name, ShouldEqual, "prompt-only")
			So(sk.Prompt, ShouldEqual, "Just a prompt")
		})

		Convey("parses frontmatter-only file (no body)", func() {
			dir := t.TempDir()
			skillDir := filepath.Join(dir, "empty-body")
			os.MkdirAll(skillDir, 0755)
			path := filepath.Join(skillDir, "SKILL.md")
			os.WriteFile(path, []byte("---\nname: empty-body\ndescription: no body\n---\n"), 0644)

			sk, err := readFile(path)
			So(err, ShouldBeNil)
			So(sk.Name, ShouldEqual, "empty-body")
			So(sk.Prompt, ShouldEqual, "")
		})
	})
}

func TestWriteFile(t *testing.T) {
	Convey("writeFile", t, func() {
		Convey("writes YAML frontmatter with prompt body", func() {
			dir := t.TempDir()
			path := filepath.Join(dir, "SKILL.md")
			sk := &Skill{Name: "test", Description: "desc", Prompt: "body text"}

			err := writeFile(path, sk)
			So(err, ShouldBeNil)

			data, _ := os.ReadFile(path)
			content := string(data)
			So(content, ShouldContainSubstring, "---\n")
			So(content, ShouldContainSubstring, "name: test")
			So(content, ShouldContainSubstring, "body text")
		})
	})
}

func TestWriteMetaFile(t *testing.T) {
	Convey("writeMetaFile", t, func() {
		Convey("writes JSON metadata", func() {
			dir := t.TempDir()
			path := filepath.Join(dir, "meta.json")
			sk := &Skill{Name: "meta-test", Description: "meta desc"}

			err := writeMetaFile(path, sk)
			So(err, ShouldBeNil)

			data, _ := os.ReadFile(path)
			So(string(data), ShouldContainSubstring, "meta-test")
			So(string(data), ShouldContainSubstring, "meta desc")
		})
	})
}
