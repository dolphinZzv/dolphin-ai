package context

import (
	stdctx "context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"dolphin/internal/cli"
	"dolphin/internal/skill"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

func TestRegistry(t *testing.T) {
	Convey("Registry", t, func() {
		Convey("NewRegistry creates empty registry", func() {
			r := NewRegistry()
			So(r, ShouldNotBeNil)
			So(r.sections, ShouldBeEmpty)
		})

		Convey("Register adds a section", func() {
			r := NewRegistry()
			r.Register(&testSection{name: "a", index: 1, content: "hello"})
			So(len(r.sections), ShouldEqual, 1)
		})

		Convey("Build returns joined content sorted by Index", func() {
			r := NewRegistry()
			r.Register(&testSection{name: "b", index: 2, content: "second"})
			r.Register(&testSection{name: "a", index: 1, content: "first"})
			r.Register(&testSection{name: "c", index: 3, content: ""})

			result, err := r.Build(stdctx.Background())
			So(err, ShouldBeNil)
			So(result, ShouldEqual, "first\n---\nsecond")
		})

		Convey("Build skips empty content", func() {
			r := NewRegistry()
			r.Register(&testSection{name: "a", index: 1, content: ""})
			r.Register(&testSection{name: "b", index: 2, content: "nonempty"})

			result, err := r.Build(stdctx.Background())
			So(err, ShouldBeNil)
			So(result, ShouldEqual, "nonempty")
		})

		Convey("Build returns error from section", func() {
			r := NewRegistry()
			r.Register(&errSection{})

			_, err := r.Build(stdctx.Background())
			So(err, ShouldNotBeNil)
		})

		Convey("Build returns empty string with no sections", func() {
			r := NewRegistry()
			result, err := r.Build(stdctx.Background())
			So(err, ShouldBeNil)
			So(result, ShouldEqual, "")
		})
	})
}

func TestRegistrySections(t *testing.T) {
	Convey("Registry Sections", t, func() {
		Convey("Sections returns sorted by Index", func() {
			r := NewRegistry()
			r.Register(&testSection{name: "c", index: 3, content: "c"})
			r.Register(&testSection{name: "a", index: 1, content: "a"})
			r.Register(&testSection{name: "b", index: 2, content: "b"})

			s := r.Sections()
			So(len(s), ShouldEqual, 3)
			So(s[0].Name(), ShouldEqual, "a")
			So(s[1].Name(), ShouldEqual, "b")
			So(s[2].Name(), ShouldEqual, "c")
		})

		Convey("Sections returns empty for empty registry", func() {
			r := NewRegistry()
			So(r.Sections(), ShouldBeEmpty)
		})

		Convey("ByName finds section", func() {
			r := NewRegistry()
			r.Register(&testSection{name: "target", index: 1, content: "found"})

			s, ok := r.ByName("target")
			So(ok, ShouldBeTrue)
			So(s.Name(), ShouldEqual, "target")
		})

		Convey("ByName returns false when not found", func() {
			r := NewRegistry()
			r.Register(&testSection{name: "a", index: 1, content: "a"})

			_, ok := r.ByName("nonexistent")
			So(ok, ShouldBeFalse)
		})

		Convey("ByName returns false for empty registry", func() {
			r := NewRegistry()
			_, ok := r.ByName("anything")
			So(ok, ShouldBeFalse)
		})
	})
}

func TestBaseSection(t *testing.T) {
	Convey("Base section", t, func() {
		Convey("Name returns 'base'", func() {
			s := &Base{}
			So(s.Name(), ShouldEqual, "base")
		})

		Convey("Index returns 0", func() {
			s := &Base{}
			So(s.Index(), ShouldEqual, 0)
		})

		Convey("BuildContent returns DefaultText when set", func() {
			s := &Base{DefaultText: "custom prompt"}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "custom prompt")
		})

		Convey("BuildContent returns i18n default prompt when no workspace", func() {
			s := &Base{}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldNotBeBlank)
		})

		Convey("BuildContent reads AGENTS.md from workspace", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agent"), 0644)
			s := &Base{Workspace: dir}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "# Agent")
		})

		Convey("BuildContent reads CLAUDE.md from workspace", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644)
			s := &Base{Workspace: dir}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "# Claude")
		})

		Convey("BuildContent prefers AGENTS.md over CLAUDE.md", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agent"), 0644)
			os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644)
			s := &Base{Workspace: dir}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "# Agent")
		})
	})
}

func TestBrainSection(t *testing.T) {
	Convey("Brain section", t, func() {
		Convey("Name returns 'brain'", func() {
			s := &Brain{}
			So(s.Name(), ShouldEqual, "brain")
		})

		Convey("Index returns 5", func() {
			s := &Brain{}
			So(s.Index(), ShouldEqual, 5)
		})

		Convey("BuildContent returns empty when Reader is nil", func() {
			s := &Brain{}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent returns empty when Reader returns empty", func() {
			s := &Brain{Reader: &mockReader{index: ""}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent returns formatted index when Reader has content", func() {
			s := &Brain{Reader: &mockReader{index: "brain data"}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "brain data")
			So(content, ShouldContainSubstring, "Brain Index")
		})

		Convey("BuildContent returns empty when Reader errors", func() {
			s := &Brain{Reader: &mockReader{err: stdctx.DeadlineExceeded}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})
	})
}

func TestDesignSection(t *testing.T) {
	Convey("Design section", t, func() {
		Convey("Name returns 'design'", func() {
			s := &Design{}
			So(s.Name(), ShouldEqual, "design")
		})

		Convey("Index returns 6", func() {
			s := &Design{}
			So(s.Index(), ShouldEqual, 6)
		})

		Convey("BuildContent returns empty when DESIGN.md does not exist", func() {
			s := &Design{Workspace: t.TempDir()}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent reads DESIGN.md from workspace", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "DESIGN.md"), []byte("# Design"), 0644)
			s := &Design{Workspace: dir}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "Design Notes")
			So(content, ShouldContainSubstring, "# Design")
		})

		Convey("BuildContent reads DESIGN.md from current dir when workspace empty", func() {
			s := &Design{}
			_, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			// Should not error — just return empty if file not found
		})

		Convey("BuildContent returns empty for blank content", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "DESIGN.md"), []byte("   "), 0644)
			s := &Design{Workspace: dir}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})
	})
}

func TestSoulSection(t *testing.T) {
	Convey("Soul section", t, func() {
		Convey("Name returns 'soul'", func() {
			s := &Soul{}
			So(s.Name(), ShouldEqual, "soul")
		})

		Convey("Index returns 1", func() {
			s := &Soul{}
			So(s.Index(), ShouldEqual, 1)
		})

		Convey("BuildContent returns empty when SOUL.md does not exist", func() {
			s := &Soul{Workspace: t.TempDir()}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent reads SOUL.md from workspace", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("be excellent"), 0644)
			s := &Soul{Workspace: dir}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "Soul")
			So(content, ShouldContainSubstring, "be excellent")
		})

		Convey("BuildContent returns empty for blank content", func() {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("  \n  "), 0644)
			s := &Soul{Workspace: dir}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})
	})
}

func TestSkillsSection(t *testing.T) {
	Convey("Skills section", t, func() {
		Convey("Name returns 'skills'", func() {
			s := &Skills{}
			So(s.Name(), ShouldEqual, "skills")
		})

		Convey("Index returns 7", func() {
			s := &Skills{}
			So(s.Index(), ShouldEqual, 7)
		})

		Convey("BuildContent returns empty when Store is nil", func() {
			s := &Skills{}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent returns empty when Store has no skills", func() {
			s := &Skills{Store: &mockSkillStore{}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent lists enabled skills", func() {
			s := &Skills{Store: &mockSkillStore{
				skills: []skill.Skill{
					{Name: "helper", Description: "helps", Enabled: true},
				},
			}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "helper")
			So(content, ShouldContainSubstring, "helps")
			So(content, ShouldContainSubstring, "Skill:")
		})

		Convey("BuildContent skips disabled skills", func() {
			s := &Skills{Store: &mockSkillStore{
				skills: []skill.Skill{
					{Name: "enabled-one", Description: "works", Enabled: true},
					{Name: "disabled-one", Description: "broken", Enabled: false},
				},
			}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "enabled-one")
			So(content, ShouldNotContainSubstring, "disabled-one")
		})

		Convey("BuildContent returns empty when Store errors", func() {
			s := &Skills{Store: &mockSkillStore{err: stdctx.DeadlineExceeded}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})
	})
}

func TestTransportSection(t *testing.T) {
	Convey("Transport section", t, func() {
		Convey("Name returns 'transport'", func() {
			s := &Transport{}
			So(s.Name(), ShouldEqual, "transport")
		})

		Convey("Index returns 3", func() {
			s := &Transport{}
			So(s.Index(), ShouldEqual, 3)
		})

		Convey("BuildContent returns empty when ContextFunc returns empty", func() {
			s := &Transport{ContextFunc: func() string { return "" }}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent returns formatted transport context", func() {
			s := &Transport{ContextFunc: func() string { return "dingtalk" }}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "Transport Context")
			So(content, ShouldContainSubstring, "dingtalk")
		})
	})
}

func TestWorkmodeSection(t *testing.T) {
	Convey("Workmode section", t, func() {
		Convey("Name returns 'workmode'", func() {
			s := &Workmode{}
			So(s.Name(), ShouldEqual, "workmode")
		})

		Convey("Index returns 2", func() {
			s := &Workmode{}
			So(s.Index(), ShouldEqual, 2)
		})

		Convey("default mode returns default prompt", func() {
			s := &Workmode{Mode: "default"}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "default")
			So(content, ShouldContainSubstring, "MUST ask")
		})

		Convey("yolo mode returns yolo prompt", func() {
			s := &Workmode{Mode: "yolo"}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "yolo")
			So(content, ShouldContainSubstring, "autonomously")
		})

		Convey("unknown mode defaults to default prompt", func() {
			s := &Workmode{Mode: "unknown"}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "default")
		})
	})
}

func TestWorkspaceSection(t *testing.T) {
	Convey("Workspace section", t, func() {
		Convey("Name returns 'workspace'", func() {
			s := &Workspace{}
			So(s.Name(), ShouldEqual, "workspace")
		})

		Convey("Index returns 4", func() {
			s := &Workspace{}
			So(s.Index(), ShouldEqual, 4)
		})

		Convey("BuildContent returns empty when Dir is empty", func() {
			s := &Workspace{}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent returns formatted workspace info", func() {
			s := &Workspace{Dir: "/home/project"}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "Workspace")
			So(content, ShouldContainSubstring, "/home/project")
		})
	})
}

func TestCliSection(t *testing.T) {
	Convey("CliSection", t, func() {
		Convey("Name returns 'cli'", func() {
			s := &CliSection{}
			So(s.Name(), ShouldEqual, "cli")
		})

		Convey("Index returns 15", func() {
			s := &CliSection{}
			So(s.Index(), ShouldEqual, 15)
		})

		Convey("BuildContent returns empty when CLIs is nil", func() {
			s := &CliSection{}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent returns empty when CLIs is empty slice", func() {
			s := &CliSection{CLIs: []cli.CLI{}}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldEqual, "")
		})

		Convey("BuildContent lists CLI with help fetched", func() {
			if runtime.GOOS == "windows" {
				t.Skip("skipping on windows")
			}
			dir := t.TempDir()
			script := filepath.Join(dir, "demo")
			_ = os.WriteFile(script, []byte("#!/bin/sh\necho Usage: demo '<args>'"), 0o755)

			s := &CliSection{
				CLIs:   []cli.CLI{{Name: "demo", Path: script}},
				Logger: zap.NewNop(),
			}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "demo")
			So(content, ShouldContainSubstring, "Usage: demo <args>")
			So(content, ShouldContainSubstring, "shell")
		})

		Convey("BuildContent shows no-help placeholder when help fetch fails", func() {
			if runtime.GOOS == "windows" {
				t.Skip("skipping on windows")
			}
			dir := t.TempDir()
			script := filepath.Join(dir, "bad")
			_ = os.WriteFile(script, []byte("#!/bin/sh\ntrue"), 0o755)

			s := &CliSection{
				CLIs:   []cli.CLI{{Name: "bad", Path: script}},
				Logger: zap.NewNop(),
			}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "bad")
			So(content, ShouldContainSubstring, "no help available")
		})
	})
}

func TestFirstLine(t *testing.T) {
	Convey("firstLine", t, func() {
		Convey("returns full string when short with no newline", func() {
			So(firstLine("hello"), ShouldEqual, "hello")
		})

		Convey("truncates at newline", func() {
			So(firstLine("hello\nworld"), ShouldEqual, "hello")
		})

		Convey("truncates at carriage return", func() {
			So(firstLine("hello\rworld"), ShouldEqual, "hello")
		})

		Convey("trims surrounding whitespace", func() {
			So(firstLine("  hello  "), ShouldEqual, "hello")
		})

		Convey("truncates long lines at 200 chars", func() {
			long := make([]byte, 300)
			for i := range long {
				long[i] = 'x'
			}
			result := firstLine(string(long))
			So(len(result), ShouldEqual, 203) // 200 + "..."
			So(result[len(result)-3:], ShouldEqual, "...")
		})
	})
}

// --- test helpers ---

type testSection struct {
	name    string
	index   int
	content string
}

func (s *testSection) Name() string                                  { return s.name }
func (s *testSection) Index() int                                    { return s.index }
func (s *testSection) BuildContent(_ stdctx.Context) (string, error) { return s.content, nil }

type errSection struct{}

func (s *errSection) Name() string { return "err" }
func (s *errSection) Index() int   { return 0 }
func (s *errSection) BuildContent(_ stdctx.Context) (string, error) {
	return "", stdctx.DeadlineExceeded
}

type mockReader struct {
	index string
	err   error
}

func (m *mockReader) Tree() (string, error) {
	return m.index, m.err
}

type mockSkillStore struct {
	skills []skill.Skill
	err    error
}

func (m *mockSkillStore) List(_ stdctx.Context) ([]skill.Skill, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.skills, nil
}

func (m *mockSkillStore) Get(_ stdctx.Context, name string) (*skill.Skill, error) {
	return nil, nil
}

func (m *mockSkillStore) Save(_ stdctx.Context, sk skill.Skill) error {
	return nil
}

func (m *mockSkillStore) Delete(_ stdctx.Context, name string) error {
	return nil
}

func (m *mockSkillStore) Search(_ stdctx.Context, query string) ([]skill.Skill, error) {
	return nil, nil
}

func TestWorkflowSection(t *testing.T) {
	Convey("Workflow section", t, func() {
		Convey("Name returns 'workflow'", func() {
			s := &Workflow{}
			So(s.Name(), ShouldEqual, "workflow")
		})

		Convey("Index returns 8", func() {
			s := &Workflow{}
			So(s.Index(), ShouldEqual, 8)
		})

		Convey("BuildContent returns workflow instructions", func() {
			s := &Workflow{}
			content, err := s.BuildContent(stdctx.Background())
			So(err, ShouldBeNil)
			So(content, ShouldContainSubstring, "workflow")
			So(content, ShouldContainSubstring, "brain_write")
			So(content, ShouldContainSubstring, "run_workflow")
		})
	})
}
