package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProjectGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	info := DetectProject(dir)
	if !contains(info.Languages, "go") {
		t.Error("expected go language detected")
	}
	if info.PackageFile != "go.mod" {
		t.Errorf("PackageFile = %q, want go.mod", info.PackageFile)
	}
}

func TestDetectProjectNode(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"dependencies":{"react":"^18.0.0","express":"^4.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0644)

	info := DetectProject(dir)
	if !contains(info.Languages, "typescript") {
		t.Error("expected typescript language detected")
	}
	if !contains(info.Frameworks, "react") {
		t.Error("expected react framework detected")
	}
	if !contains(info.Frameworks, "express") {
		t.Error("expected express framework detected")
	}
}

func TestDetectProjectPython(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask"), 0644)

	info := DetectProject(dir)
	if !contains(info.Languages, "python") {
		t.Error("expected python language detected")
	}
}

func TestDetectProjectRust(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0644)

	info := DetectProject(dir)
	if !contains(info.Languages, "rust") {
		t.Error("expected rust language detected")
	}
}

func TestDetectProjectDocker(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM ubuntu"), 0644)

	info := DetectProject(dir)
	if !info.HasDocker {
		t.Error("expected HasDocker = true")
	}
}

func TestDetectProjectCI(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := filepath.Join(dir, ".github", "workflows")
	os.MkdirAll(workflowsDir, 0755)
	os.WriteFile(filepath.Join(workflowsDir, "ci.yml"), []byte("name: CI"), 0644)

	info := DetectProject(dir)
	if !info.HasCI {
		t.Error("expected HasCI = true")
	}
}

func TestDetectProjectEmpty(t *testing.T) {
	dir := t.TempDir()
	info := DetectProject(dir)
	if !info.IsEmpty() {
		t.Error("expected empty project")
	}
}

func TestDetectProjectMultiLanguage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM golang"), 0644)
	k8sDir := filepath.Join(dir, "k8s")
	os.MkdirAll(k8sDir, 0755)
	os.WriteFile(filepath.Join(k8sDir, "deploy.yaml"), []byte("kind: Deployment"), 0644)

	info := DetectProject(dir)
	if !contains(info.Languages, "go") {
		t.Error("expected go language")
	}
	if !info.HasDocker {
		t.Error("expected HasDocker = true")
	}
	if !info.HasK8s {
		t.Error("expected HasK8s = true")
	}
}

func TestProjectInfoKeywords(t *testing.T) {
	info := &ProjectInfo{
		Languages:  []string{"go", "typescript"},
		Frameworks: []string{"react"},
		HasDocker:  true,
		HasCI:      true,
	}

	kw := info.Keywords()
	if !contains(kw, "go") {
		t.Error("expected go in keywords")
	}
	if !contains(kw, "react") {
		t.Error("expected react in keywords")
	}
	if !contains(kw, "docker") {
		t.Error("expected docker in keywords")
	}
	if !contains(kw, "ci-cd") {
		t.Error("expected ci-cd in keywords")
	}
}

func TestDetectProjectDevDependencies(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"devDependencies":{"tailwindcss":"^3.0.0","vitest":"^1.0.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0644)

	info := DetectProject(dir)
	if !contains(info.Frameworks, "tailwind") {
		t.Error("expected tailwind in dev dependencies")
	}
}

func TestDetectProjectJava(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project></project>"), 0644)

	info := DetectProject(dir)
	if !contains(info.Languages, "java") {
		t.Error("expected java language detected")
	}
}

func TestDetectProjectK8sDir(t *testing.T) {
	dir := t.TempDir()
	k8sDir := filepath.Join(dir, "kubernetes")
	os.MkdirAll(k8sDir, 0755)
	os.WriteFile(filepath.Join(k8sDir, "deployment.yaml"), []byte("kind: Deployment"), 0644)

	info := DetectProject(dir)
	if !info.HasK8s {
		t.Error("expected HasK8s = true for kubernetes/ dir")
	}
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
