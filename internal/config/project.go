package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ProjectInfo describes a detected project from the working directory.
type ProjectInfo struct {
	Languages   []string // ["go", "typescript"]
	Frameworks  []string // ["react", "express"]
	PackageFile string   // "go.mod", "package.json", etc.
	HasDocker   bool
	HasK8s      bool
	HasCI       bool
}

// DetectProject scans the given directory for known project files and returns
// the detected languages, frameworks, and infrastructure markers.
func DetectProject(workDir string) *ProjectInfo {
	info := &ProjectInfo{}

	// Go
	if fileExists(workDir, "go.mod") {
		info.Languages = append(info.Languages, "go")
		info.PackageFile = firstSet(info.PackageFile, "go.mod")
	}

	// Node.js / TypeScript
	if fileExists(workDir, "package.json") {
		info.Languages = append(info.Languages, "typescript", "javascript")
		info.PackageFile = firstSet(info.PackageFile, "package.json")

		// Check for frameworks in package.json
		if frameworks := detectNodeFrameworks(workDir); len(frameworks) > 0 {
			info.Frameworks = append(info.Frameworks, frameworks...)
		}
	}

	// Python
	if fileExists(workDir, "requirements.txt") || fileExists(workDir, "pyproject.toml") || fileExists(workDir, "setup.py") {
		info.Languages = append(info.Languages, "python")
		info.PackageFile = firstSet(info.PackageFile, "requirements.txt", "pyproject.toml", "setup.py")
	}

	// Rust
	if fileExists(workDir, "Cargo.toml") {
		info.Languages = append(info.Languages, "rust")
		info.PackageFile = firstSet(info.PackageFile, "Cargo.toml")
	}

	// Java / Kotlin
	if fileExists(workDir, "pom.xml") || fileExists(workDir, "build.gradle") || fileExists(workDir, "build.gradle.kts") {
		info.Languages = append(info.Languages, "java")
		info.PackageFile = firstSet(info.PackageFile, "pom.xml", "build.gradle", "build.gradle.kts")
	}

	// Swift
	if fileExists(workDir, "Package.swift") || hasFilesWithExt(workDir, ".xcodeproj") {
		info.Languages = append(info.Languages, "swift")
	}

	// Docker
	if fileExists(workDir, "Dockerfile") || fileExists(workDir, "docker-compose.yml") || fileExists(workDir, "docker-compose.yaml") {
		info.HasDocker = true
	}

	// Kubernetes
	if info.HasK8s = detectK8s(workDir); !info.HasK8s {
		// Also check if any yaml files in root mention k8s resources
		if hasFilesWithExt(workDir, ".yaml", ".yml") {
			info.HasK8s = true
		}
	}

	// CI/CD
	if dirExists(workDir, ".github/workflows") || dirExists(workDir, ".github") ||
		fileExists(workDir, ".gitlab-ci.yml") || fileExists(workDir, "Jenkinsfile") {
		info.HasCI = true
	}

	return info
}

// IsEmpty returns true if no project was detected.
func (p *ProjectInfo) IsEmpty() bool {
	return len(p.Languages) == 0 && !p.HasDocker && !p.HasK8s && !p.HasCI
}

// Keywords returns search keywords derived from the project info for matching tools.
func (p *ProjectInfo) Keywords() []string {
	var kw []string
	kw = append(kw, p.Languages...)
	kw = append(kw, p.Frameworks...)
	if p.HasDocker {
		kw = append(kw, "docker")
	}
	if p.HasK8s {
		kw = append(kw, "kubernetes")
	}
	if p.HasCI {
		kw = append(kw, "ci-cd")
	}
	return kw
}

// detectK8s checks for Kubernetes-related directories that contain yaml files.
func detectK8s(workDir string) bool {
	k8sDirs := []string{"k8s", "kubernetes", "deploy", "manifests"}
	for _, d := range k8sDirs {
		if hasFilesWithExt(filepath.Join(workDir, d), ".yaml", ".yml") {
			return true
		}
	}
	return false
}

// detectNodeFrameworks reads package.json and looks for known framework dependencies.
func detectNodeFrameworks(workDir string) []string {
	data, err := os.ReadFile(filepath.Join(workDir, "package.json"))
	if err != nil {
		return nil
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	frameworkMap := map[string]string{
		"react":         "react",
		"vue":           "vue",
		"next":          "nextjs",
		"nuxt":          "nuxt",
		"@angular/core": "angular",
		"svelte":        "svelte",
		"express":       "express",
		"fastify":       "fastify",
		"@nestjs/core":  "nestjs",
		"tailwindcss":   "tailwind",
		"electron":      "electron",
		"react-native":  "react-native",
		"expo":          "expo",
	}

	seen := make(map[string]bool)
	var frameworks []string
	for dep, fw := range frameworkMap {
		if seen[fw] {
			continue
		}
		if _, ok := pkg.Dependencies[dep]; ok {
			frameworks = append(frameworks, fw)
			seen[fw] = true
		} else if _, ok := pkg.DevDependencies[dep]; ok {
			frameworks = append(frameworks, fw)
			seen[fw] = true
		}
	}
	return frameworks
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func dirExists(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.IsDir()
}

func hasFilesWithExt(dir string, exts ...string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		for _, ext := range exts {
			if strings.HasSuffix(e.Name(), ext) {
				return true
			}
		}
	}
	return false
}

func firstSet(current string, candidates ...string) string {
	if current != "" {
		return current
	}
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}
