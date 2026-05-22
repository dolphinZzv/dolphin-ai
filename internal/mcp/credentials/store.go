package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"dolphin/internal/config"
)

type Store interface {
	Search(query string, credType string, limit int) ([]Credential, error)
	Get(name string) (*Credential, error)
	List() ([]Credential, error)
	Add(cred *Credential) error
	Delete(name string) error
}

type FileStore struct {
	cfg  *config.CredentialsConfig
	path string
	mu   sync.RWMutex
	data *CredentialFile
}

func NewFileStore(cfg *config.CredentialsConfig) *FileStore {
	path := cfg.Path
	if !filepath.IsAbs(path) {
		home, _ := os.UserHomeDir()
		if home != "" {
			path = filepath.Join(home, path)
		}
	}

	fs := &FileStore{
		cfg:  cfg,
		path: path,
	}
	fs.load()
	return fs
}

func (fs *FileStore) load() {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		fs.data = &CredentialFile{Credentials: []Credential{}}
		return
	}

	var cf CredentialFile
	if err := json.Unmarshal(data, &cf); err != nil {
		fs.data = &CredentialFile{Credentials: []Credential{}}
		return
	}
	fs.data = &cf
}

func (fs *FileStore) Persist() {
	data, err := json.MarshalIndent(fs.data, "", "  ")
	if err != nil {
		return
	}

	dir := filepath.Dir(fs.path)
	os.MkdirAll(dir, 0700)

	tmpPath := fs.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return
	}
	os.Rename(tmpPath, fs.path)
}

func (fs *FileStore) Search(query string, credType string, limit int) ([]Credential, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	var results []Credential
	for _, cred := range fs.data.Credentials {
		if credType != "" && cred.Type != credType {
			continue
		}

		if query != "" {
			lowerQuery := strings.ToLower(query)
			match := false
			if strings.Contains(strings.ToLower(cred.Name), lowerQuery) {
				match = true
			} else if strings.Contains(strings.ToLower(cred.URL), lowerQuery) {
				match = true
			} else if strings.Contains(strings.ToLower(cred.Username), lowerQuery) {
				match = true
			} else if strings.Contains(strings.ToLower(cred.Comment), lowerQuery) {
				match = true
			}
			if !match {
				continue
			}
		}

		safeCred := Credential{
			Name:     cred.Name,
			Type:     cred.Type,
			URL:      cred.URL,
			Username: cred.Username,
			Comment:  cred.Comment,
		}
		results = append(results, safeCred)

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func (fs *FileStore) Get(name string) (*Credential, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	for _, cred := range fs.data.Credentials {
		if cred.Name == name {
			return &cred, nil
		}
	}
	return nil, nil
}

func (fs *FileStore) List() ([]Credential, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.data.Credentials, nil
}

func (fs *FileStore) Add(cred *Credential) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for i, existing := range fs.data.Credentials {
		if existing.Name == cred.Name {
			fs.data.Credentials[i] = *cred
			fs.Persist()
			return nil
		}
	}

	fs.data.Credentials = append(fs.data.Credentials, *cred)
	fs.Persist()
	return nil
}

func (fs *FileStore) Delete(name string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for i, cred := range fs.data.Credentials {
		if cred.Name == name {
			fs.data.Credentials = append(fs.data.Credentials[:i], fs.data.Credentials[i+1:]...)
			fs.Persist()
			return nil
		}
	}
	return nil
}
