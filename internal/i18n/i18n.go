package i18n

import (
	"fmt"
	"strings"
	"sync"
)

// Dict maps message keys to translated strings.
type Dict map[string]string

var (
	mu    sync.RWMutex
	lang  string
	store = map[string]map[string]Dict{} // pkg -> lang -> Dict
)

// SetLang sets the current language (e.g. "en", "zh").
func SetLang(l string) {
	mu.Lock()
	defer mu.Unlock()
	lang = l
}

// Lang returns the current language.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// T translates key using the current language.
// Key format: "pkg.name" where pkg matches the package prefix used in Register.
// Falls back to English, then to the key itself.
func T(key string, args ...any) string {
	mu.RLock()
	defer mu.RUnlock()

	pkg, name := splitKey(key)
	if pkg == "" {
		return key
	}

	// Try current language
	if langs, ok := store[pkg]; ok {
		if dict, ok := langs[lang]; ok {
			if tmpl, ok := dict[name]; ok {
				return format(tmpl, args)
			}
		}
		// Fallback to English
		if dict, ok := langs["en"]; ok {
			if tmpl, ok := dict[name]; ok {
				return format(tmpl, args)
			}
		}
	}
	return key
}

// Register adds translations for a package across languages.
// Call from package-level init() functions.
func Register(pkg string, dicts ...any) {
	mu.Lock()
	defer mu.Unlock()

	if store[pkg] == nil {
		store[pkg] = make(map[string]Dict)
	}

	for i := 0; i+1 < len(dicts); i += 2 {
		l, ok := dicts[i].(string)
		if !ok {
			continue
		}
		d, ok := dicts[i+1].(Dict)
		if !ok {
			continue
		}
		if store[pkg][l] == nil {
			store[pkg][l] = make(Dict)
		}
		for k, v := range d {
			store[pkg][l][k] = v
		}
	}
}

func splitKey(key string) (string, string) {
	idx := strings.IndexByte(key, '.')
	if idx < 0 {
		return "", key
	}
	return key[:idx], key[idx+1:]
}

func format(tmpl string, args []any) string {
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}
