package limit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStoreIncrementGet(t *testing.T) {
	s := NewMemoryStore()
	if v, err := s.Get("foo"); err != nil || v != 0 {
		t.Fatalf("expected 0, got %d err=%v", v, err)
	}
	if v, err := s.Increment("foo", 1); err != nil || v != 1 {
		t.Fatalf("expected 1, got %d err=%v", v, err)
	}
	if v, _ := s.Increment("foo", 2); v != 3 {
		t.Fatalf("expected 3, got %d", v)
	}
}

func TestMemoryStoreResetPrefix(t *testing.T) {
	s := NewMemoryStore()
	s.Increment("a.b", 10)
	s.Increment("a.c", 20)
	s.Increment("b.x", 99)
	if err := s.Reset("a."); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("a.b"); v != 0 {
		t.Fatalf("a.b not reset, got %d", v)
	}
	if v, _ := s.Get("a.c"); v != 0 {
		t.Fatalf("a.c not reset, got %d", v)
	}
	if v, _ := s.Get("b.x"); v != 99 {
		t.Fatalf("b.x should be untouched, got %d", v)
	}
}

func TestMemoryStoreGetAll(t *testing.T) {
	s := NewMemoryStore()
	s.Increment("k1", 1)
	s.Increment("k2", 2)
	all, _ := s.GetAll()
	if len(all) != 2 || all["k1"] != 1 || all["k2"] != 2 {
		t.Fatalf("unexpected GetAll result: %v", all)
	}
}

func TestMemoryStoreResetAllWithEmptyPrefix(t *testing.T) {
	s := NewMemoryStore()
	s.Increment("x", 1)
	s.Increment("y", 2)
	if err := s.Reset(""); err != nil {
		t.Fatal(err)
	}
	all, _ := s.GetAll()
	if len(all) != 0 {
		t.Fatalf("expected empty store after reset all, got %v", all)
	}
}

func TestMemoryStoreLastReset(t *testing.T) {
	s := NewMemoryStore()
	if !s.LastReset("k").IsZero() {
		t.Fatal("LastReset should be zero before any reset")
	}
	if err := s.Reset("k"); err != nil {
		t.Fatal(err)
	}
	if s.LastReset("k").IsZero() {
		t.Fatal("LastReset should be set after reset")
	}
}

func TestFileStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := store.Get("foo"); err != nil || v != 0 {
		t.Fatalf("expected 0, got %d err=%v", v, err)
	}
	if v, err := store.Increment("foo", 5); err != nil || v != 5 {
		t.Fatalf("increment: expected 5, got %d err=%v", v, err)
	}
	if v, _ := store.Increment("foo", -2); v != 3 {
		t.Fatalf("expected 3, got %d", v)
	}
	all, _ := store.GetAll()
	if all["foo"] != 3 {
		t.Fatalf("GetAll wrong: %v", all)
	}
	if err := store.Reset("fo"); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.Get("foo"); v != 0 {
		t.Fatalf("expected 0 after reset, got %d", v)
	}
}

func TestFileStoreResetAll(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	store.Increment("a", 1)
	store.Increment("b", 2)
	store.Increment("c", 3)
	if err := store.Reset(""); err != nil {
		t.Fatal(err)
	}
	all, _ := store.GetAll()
	if len(all) != 0 {
		t.Fatalf("expected empty, got %v", all)
	}
}

func TestFileStoreSetLastReset(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	when := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := store.SetLastReset(when); err != nil {
		t.Fatal(err)
	}
	if !store.LastReset().Equal(when) {
		t.Fatalf("expected %v, got %v", when, store.LastReset())
	}
	store2, _ := NewFileStore(dir)
	if !store2.LastReset().Equal(when) {
		t.Fatalf("expected persisted lastReset %v, got %v", when, store2.LastReset())
	}
}

func TestFileStoreSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	store.Increment("llm.requests", 42)
	store2, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := store2.Get("llm.requests")
	if v != 42 {
		t.Fatalf("expected persisted 42, got %d", v)
	}
}

func TestFileStoreCorruptFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "counters.json"), []byte("not-json{"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := store.Get("anything")
	if v != 0 {
		t.Fatalf("expected 0 on corrupt file, got %d", v)
	}
}

func TestFileStoreReadNonexistentReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	all, _ := store.GetAll()
	if len(all) != 0 {
		t.Fatalf("expected empty, got %v", all)
	}
}

func TestFileStoreMkdirFailure(t *testing.T) {
	if _, err := NewFileStore("/this/path/cannot/be/created/\x00bad"); err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestHasPrefixEdges(t *testing.T) {
	cases := []struct {
		s, prefix string
		want      bool
	}{
		{"llm.requests", "llm.", true},
		{"llm.requests", "llmx", false},
		{"anything", "", true},
		{"ab", "abc", false},
		{"abc", "abc", true},
		{"abcdef", "abc", true},
		{"abcdef", "abcdef", true},
	}
	for _, c := range cases {
		if got := hasPrefix(c.s, c.prefix); got != c.want {
			t.Errorf("hasPrefix(%q, %q) = %v, want %v", c.s, c.prefix, got, c.want)
		}
	}
}
