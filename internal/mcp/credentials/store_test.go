package credentials

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"dolphin/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStore_Add_and_Get(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}

	cred := &Credential{
		Name:     "test-api",
		Type:     "api_key",
		URL:      "https://api.example.com",
		Username: "user123",
		Secret:   "secret-key-123",
		Comment:  "Test credential",
	}

	err := store.Add(cred)
	require.NoError(t, err)

	got, err := store.Get("test-api")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "test-api", got.Name)
	assert.Equal(t, "api_key", got.Type)
	assert.Equal(t, "secret-key-123", got.Secret)
}

func TestFileStore_Add_updates_existing(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}

	cred1 := &Credential{Name: "test-key", Type: "api_key", Secret: "secret1"}
	err := store.Add(cred1)
	require.NoError(t, err)

	cred2 := &Credential{Name: "test-key", Type: "api_key", Secret: "secret2"}
	err = store.Add(cred2)
	require.NoError(t, err)

	got, err := store.Get("test-key")
	require.NoError(t, err)
	assert.Equal(t, "secret2", got.Secret)
}

func TestFileStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}

	cred := &Credential{Name: "to-delete", Type: "api_key", Secret: "secret"}
	err := store.Add(cred)
	require.NoError(t, err)

	err = store.Delete("to-delete")
	require.NoError(t, err)

	got, err := store.Get("to-delete")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestFileStore_Delete_nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}

	err := store.Delete("nonexistent")
	assert.NoError(t, err)
}

func TestFileStore_List(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}

	store.Add(&Credential{Name: "key1", Type: "api_key", Secret: "s1"})
	store.Add(&Credential{Name: "key2", Type: "password", Secret: "s2"})

	creds, err := store.List()
	require.NoError(t, err)
	assert.Len(t, creds, 2)
}

func TestFileStore_Search(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}

	store.Add(&Credential{Name: "github-api", Type: "api_key", URL: "https://api.github.com", Secret: "secret1"})
	store.Add(&Credential{Name: "aws-prod", Type: "aws_access_key", URL: "https://aws.amazon.com", Secret: "secret2"})
	store.Add(&Credential{Name: "jane-password", Type: "password", Username: "jane", Secret: "secret3"})

	results, err := store.Search("github", "", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "github-api", results[0].Name)

	results, err = store.Search("", "api_key", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	results, err = store.Search("jane", "", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "jane-password", results[0].Name)
}

func TestFileStore_Search_does_not_return_secrets(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}

	store.Add(&Credential{Name: "secret-key", Type: "api_key", Secret: "super-secret"})

	results, err := store.Search("secret-key", "", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Empty(t, results[0].Secret)
}

func TestFileStore_Persist_and_reload(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	store1 := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}
	store1.Add(&Credential{Name: "persisted", Type: "api_key", Secret: "secret"})

	store2 := &FileStore{
		cfg:  &config.CredentialsConfig{Path: path},
		path: path,
		mu:   sync.RWMutex{},
		data: &CredentialFile{Credentials: []Credential{}},
	}
	store2.load()

	got, err := store2.Get("persisted")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "secret", got.Secret)
}

func TestNewFileStore(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.json")

	require.NoError(t, os.WriteFile(path, []byte(`{"credentials":[{"name":"existing","type":"api_key","secret":"abc"}]}`), 0600))

	store := NewFileStore(&config.CredentialsConfig{Path: path})

	got, err := store.Get("existing")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "abc", got.Secret)
}
