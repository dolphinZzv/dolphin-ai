package server

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var dist struct {
	once   sync.Once
	index  []byte
	assets map[string][]byte
}

// resetDist clears the cached dist state for testing.
func resetDist() {
	dist = struct {
		once   sync.Once
		index  []byte
		assets map[string][]byte
	}{}
}

func loadDist() {
	dist.once.Do(func() {
		candidates := []string{}

		exe, err := os.Executable()
		if err == nil {
			candidates = append(candidates, filepath.Join(filepath.Dir(exe), "ui", "dist"))
		}

		cwd, err := os.Getwd()
		if err == nil {
			candidates = append(candidates, filepath.Join(cwd, "ui", "dist"))
		}

		for _, dir := range candidates {
			indexPath := filepath.Join(dir, "index.html")
			data, err := os.ReadFile(indexPath)
			if err != nil {
				continue
			}
			dist.index = data
			dist.assets = make(map[string][]byte)
			filepath.Walk(dir, func(p string, fi os.FileInfo, _ error) error {
				if fi == nil || fi.IsDir() || fi.Name() == "index.html" {
					return nil
				}
				rel, _ := filepath.Rel(dir, p)
				dist.assets[rel], _ = os.ReadFile(p)
				return nil
			})
			return
		}
	})
}

// SPAHandler serves the built frontend from ui/dist/ with SPA fallback.
// Static files are loaded into memory once at startup.
func SPAHandler() http.Handler {
	loadDist()

	if dist.index == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"frontend not built, use 'cd ui && npm run build'"}`))
		})
	}

	mime := map[string]string{
		".js":   "application/javascript",
		".css":  "text/css",
		".html": "text/html; charset=utf-8",
		".md":   "text/markdown; charset=utf-8",
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Serve any cached file (js, css, md, etc.)
		if data, ok := dist.assets[path]; ok {
			if ct, ok := mime[filepath.Ext(path)]; ok {
				w.Header().Set("Content-Type", ct)
			}
			w.Write(data)
			return
		}

		// SPA fallback: serve index.html for all other paths
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dist.index)
	})
}

// DevSPAHandler proxies to the Vite dev server (localhost:5173) for development.
// No frontend build needed — just run `cd ui && npm run dev`.
func DevSPAHandler(target string) http.Handler {
	proxyURL, err := url.Parse(target)
	if err != nil {
		panic("DevSPAHandler: invalid target: " + err.Error())
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := &http.Client{}
		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, proxyURL.String()+r.URL.Path, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for k, v := range r.Header {
			proxyReq.Header[k] = v
		}
		proxyReq.Header.Set("X-Forwarded-Host", r.Host)

		resp, err := proxy.Do(proxyReq)
		if err != nil {
			http.Error(w, "Vite dev server not running on "+target, http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
}
