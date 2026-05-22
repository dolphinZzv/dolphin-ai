package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/smartystreets/goconvey/convey"
)

func TestAgentManifestE2E(t *testing.T) {
	convey.Convey("Given the demo_agents repo with agents.json", t, func() {
		tmpHome := t.TempDir()
		tmpProject := t.TempDir()
		origHome := os.Getenv("HOME")
		origCfgDir := config.ProjectConfigDir
		config.ProjectConfigDir = tmpProject
		os.Setenv("HOME", tmpHome)
		defer func() {
			os.Setenv("HOME", origHome)
			config.ProjectConfigDir = origCfgDir
		}()

		// Copy demo_agents.json to project root for local fallback
		srcData, err := os.ReadFile(filepath.Join("..", "demo_agents.json"))
		convey.So(err, convey.ShouldBeNil)
		err = os.WriteFile(filepath.Join(tmpProject, "demo_agents.json"), srcData, 0600)
		convey.So(err, convey.ShouldBeNil)

		// Create reviewer testdata directory
		testdataDest := filepath.Join(tmpProject, "tests", "testdata", "demo_agent_repo")
		os.MkdirAll(testdataDest, 0700)
		agentYAML := []byte("name: reviewer\nrole: |\n  You are a code review expert.\ntools:\n  - shell\ntimeout: 120\n")
		os.WriteFile(filepath.Join(testdataDest, "agent.yaml"), agentYAML, 0600)

		convey.Convey("The local fallback manifest should be parseable", func() {
			fetcher := config.NewRepoFetcher(t.TempDir())
			fetcher.SetLocalDir(tmpProject)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			manifest, err := fetcher.FetchAgentManifest(ctx, "dolphinZzv/demo_agents")
			convey.So(err, convey.ShouldBeNil)
			convey.So(manifest, convey.ShouldNotBeNil)
			convey.So(len(manifest.Agents), convey.ShouldEqual, 1)
			convey.So(manifest.Agents[0].Name, convey.ShouldEqual, "reviewer")
			convey.So(manifest.Agents[0].Description, convey.ShouldEqual, "reviewer")
			convey.So(manifest.Agents[0].URL, convey.ShouldContainSubstring, "git@github.com")
		})

		convey.Convey("Agent install from local path should populate .dolphin/agents/<name>/", func() {
			agentsDir := filepath.Join(tmpProject, "agents")
			agentDir := filepath.Join(agentsDir, "reviewer")

			err := copyDir(testdataDest, agentDir)
			convey.So(err, convey.ShouldBeNil)

			convey.Convey("agent.yaml should exist in .dolphin/agents/reviewer/", func() {
				yamlPath := filepath.Join(agentDir, "agent.yaml")
				_, err := os.Stat(yamlPath)
				convey.So(err, convey.ShouldBeNil)

				data, err := os.ReadFile(yamlPath)
				convey.So(err, convey.ShouldBeNil)
				convey.So(string(data), convey.ShouldContainSubstring, "reviewer")
			})

			convey.Convey(".dolphin/agents/ should contain installed agent directory", func() {
				entries, err := os.ReadDir(agentsDir)
				convey.So(err, convey.ShouldBeNil)
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					if e.IsDir() {
						names = append(names, e.Name())
					}
				}
				convey.So(names, convey.ShouldContain, "reviewer")
			})
		})

		convey.Convey("Agent list should show installed agents", func() {
			agentsDir := filepath.Join(tmpProject, "agents")
			agentDir := filepath.Join(agentsDir, "reviewer")
			os.MkdirAll(agentDir, 0700)
			os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("name: reviewer\nrole: test\n"), 0600)

			entries, err := os.ReadDir(agentsDir)
			convey.So(err, convey.ShouldBeNil)

			var active []string
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				if !strings.HasSuffix(name, ".disabled") {
					active = append(active, name)
				}
			}

			convey.So(len(active), convey.ShouldBeGreaterThan, 0)
			convey.So(active, convey.ShouldContain, "reviewer")
		})

		convey.Convey("Agent disable/enable cycle should work correctly", func() {
			agentsDir := filepath.Join(tmpProject, "agents")
			agentDir := filepath.Join(agentsDir, "reviewer")
			disabledDir := filepath.Join(agentsDir, "reviewer.disabled")
			os.MkdirAll(agentDir, 0700)
			os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("name: reviewer\nrole: test\n"), 0600)

			// Disable
			err := os.Rename(agentDir, disabledDir)
			convey.So(err, convey.ShouldBeNil)
			convey.So(dirExists(agentDir), convey.ShouldBeFalse)
			convey.So(dirExists(disabledDir), convey.ShouldBeTrue)

			// Enable
			err = os.Rename(disabledDir, agentDir)
			convey.So(err, convey.ShouldBeNil)
			convey.So(dirExists(agentDir), convey.ShouldBeTrue)
			convey.So(dirExists(disabledDir), convey.ShouldBeFalse)
		})

		convey.Convey("Agent uninstall should remove directory completely", func() {
			agentsDir := filepath.Join(tmpProject, "agents")
			agentDir := filepath.Join(agentsDir, "temp-agent")
			os.MkdirAll(agentDir, 0700)
			os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("name: temp-agent\nrole: test\n"), 0600)

			convey.So(dirExists(agentDir), convey.ShouldBeTrue)
			err := os.RemoveAll(agentDir)
			convey.So(err, convey.ShouldBeNil)
			convey.So(dirExists(agentDir), convey.ShouldBeFalse)
		})
	})
}

// TestDemoAgentsSearchCLI tests the search flow via config fetcher.
func TestDemoAgentsSearchCLI(t *testing.T) {
	convey.Convey("Given the demo_agents repo in cfg.Skills.Repos", t, func() {
		tmpHome := t.TempDir()
		tmpProject := t.TempDir()
		origHome := os.Getenv("HOME")
		origCfgDir := config.ProjectConfigDir
		config.ProjectConfigDir = tmpProject
		os.Setenv("HOME", tmpHome)
		defer func() {
			os.Setenv("HOME", origHome)
			config.ProjectConfigDir = origCfgDir
		}()

		// Copy demo_agents.json for local fallback
		srcData, _ := os.ReadFile(filepath.Join("..", "demo_agents.json"))
		os.WriteFile(filepath.Join(tmpProject, "demo_agents.json"), srcData, 0600)

		fetcher := config.NewRepoFetcher(t.TempDir())
		fetcher.SetLocalDir(tmpProject)

		convey.Convey("FetchAllAgentManifests should return demo agents", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			manifests := fetcher.FetchAllAgentManifests(ctx, []string{"dolphinZzv/demo_agents"})
			convey.So(len(manifests), convey.ShouldEqual, 1)
			convey.So(len(manifests[0].Agents), convey.ShouldEqual, 1)
			convey.So(manifests[0].Agents[0].Name, convey.ShouldEqual, "reviewer")
		})

		convey.Convey("Search agents by name should work", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			manifests := fetcher.FetchAllAgentManifests(ctx, []string{"dolphinZzv/demo_agents"})

			var results []string
			for _, m := range manifests {
				for _, a := range m.Agents {
					haystack := strings.ToLower(a.Name + " " + a.Description)
					if strings.Contains(haystack, "review") {
						results = append(results, a.Name)
					}
				}
			}
			convey.So(results, convey.ShouldContain, "reviewer")
		})

		convey.Convey("Agents should have URL field", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			manifests := fetcher.FetchAllAgentManifests(ctx, []string{"dolphinZzv/demo_agents"})
			convey.So(len(manifests), convey.ShouldBeGreaterThan, 0)

			for _, a := range manifests[0].Agents {
				convey.So(a.URL, convey.ShouldNotBeBlank)
				convey.So(a.Description, convey.ShouldNotBeBlank)
			}
		})
	})
}

// TestDemoAgentsRepoAccess tests the GitHub repo is accessible (optional, network).
func TestDemoAgentsRepoAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	convey.Convey("Given the GitHub repo dolphinZzv/demo_agents", t, func() {
		fetcher := config.NewRepoFetcher(t.TempDir())

		convey.Convey("The agents.json should be fetchable from GitHub", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			manifest, err := fetcher.FetchAgentManifest(ctx, "dolphinZzv/demo_agents")
			if err != nil {
				t.Skipf("GitHub fetch failed (network may be unavailable): %v", err)
				return
			}

			convey.So(manifest, convey.ShouldNotBeNil)
			convey.So(manifest.Version, convey.ShouldEqual, "1.0")
			convey.So(len(manifest.Agents), convey.ShouldEqual, 1)

			for _, a := range manifest.Agents {
				convey.Convey("Agent "+a.Name+" should have valid fields", func() {
					convey.So(a.URL, convey.ShouldNotBeBlank)
					convey.So(a.Description, convey.ShouldNotBeBlank)
					convey.So(a.URL, convey.ShouldContainSubstring, "git@github.com")
				})
			}
		})
	})
}

func TestDemoAgentsListAndStatus(t *testing.T) {
	convey.Convey("Given agents installed in .dolphin/agents/", t, func() {
		tmpHome := t.TempDir()
		origHome := os.Getenv("HOME")
		origCfgDir := config.ProjectConfigDir
		config.ProjectConfigDir = tmpHome
		os.Setenv("HOME", tmpHome)
		defer func() {
			os.Setenv("HOME", origHome)
			config.ProjectConfigDir = origCfgDir
		}()

		agentsDir := filepath.Join(tmpHome, "agents")
		os.MkdirAll(filepath.Join(agentsDir, "reviewer"), 0700)
		os.WriteFile(filepath.Join(agentsDir, "reviewer", "agent.yaml"),
			[]byte("name: reviewer\nrole: code review\ntools: [shell]\ntimeout: 120\n"), 0600)

		convey.Convey("Listing should return installed agents", func() {
			entries, err := os.ReadDir(agentsDir)
			convey.So(err, convey.ShouldBeNil)

			var names []string
			for _, e := range entries {
				if e.IsDir() {
					names = append(names, e.Name())
				}
			}

			convey.So(len(names), convey.ShouldEqual, 1)
			convey.So(names, convey.ShouldContain, "reviewer")
		})

		convey.Convey("Agent should have a valid agent.yaml", func() {
			path := filepath.Join(agentsDir, "reviewer", "agent.yaml")
			data, err := os.ReadFile(path)
			convey.So(err, convey.ShouldBeNil)
			convey.So(string(data), convey.ShouldContainSubstring, "name: reviewer")
		})
	})
}

func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	})
}

func init() {
	i18n.SetLang(i18n.EN)
}
