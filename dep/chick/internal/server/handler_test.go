package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// setupTestDist creates a temporary ui/dist directory for testing.
// Returns a cleanup function.
func setupTestDist(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	distDir := filepath.Join(dir, "ui", "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "app.js"), []byte("console.log(1)"), 0644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "style.css"), []byte("body{}"), 0644); err != nil {
		t.Fatalf("write style.css: %v", err)
	}
	return dir
}

func TestSPAHandler_ServesIndexForRoot(t *testing.T) {
	resetDist()

	origWd, _ := os.Getwd()
	dir := setupTestDist(t)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	handler := SPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "<html></html>" {
		t.Errorf("expected index.html content, got %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected text/html, got %s", ct)
	}
}

func TestSPAHandler_ServesIndexForSPAFallback(t *testing.T) {
	resetDist()

	origWd, _ := os.Getwd()
	dir := setupTestDist(t)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	handler := SPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/projects/123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// SPA fallback should serve index.html for unknown paths
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for SPA fallback, got %d", rec.Code)
	}
	if rec.Body.String() != "<html></html>" {
		t.Errorf("expected index.html content, got %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected text/html, got %s", ct)
	}
}

func TestSPAHandler_ServesAsset(t *testing.T) {
	resetDist()

	origWd, _ := os.Getwd()
	dir := setupTestDist(t)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	handler := SPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for asset, got %d", rec.Code)
	}
	if rec.Body.String() != "console.log(1)" {
		t.Errorf("expected JS content, got %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/javascript" {
		t.Errorf("expected application/javascript, got %s", ct)
	}
}

func TestSPAHandler_ServesCSS(t *testing.T) {
	resetDist()

	origWd, _ := os.Getwd()
	dir := setupTestDist(t)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	handler := SPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/assets/style.css", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for CSS, got %d", rec.Code)
	}
	if rec.Body.String() != "body{}" {
		t.Errorf("expected CSS content, got %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css" {
		t.Errorf("expected text/css, got %s", ct)
	}
}

func TestSPAHandler_NotFoundWhenNoDist(t *testing.T) {
	resetDist()

	origWd, _ := os.Getwd()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	handler := SPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when no dist, got %d", rec.Code)
	}
	if rec.Body.String() != `{"error":"frontend not built, use 'cd ui && npm run build'"}` {
		t.Errorf("unexpected error message: %s", rec.Body.String())
	}
}
