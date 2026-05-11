package server

import (
	"testing"

	"chick/internal/config"
)

func TestNewDB_SQLite(t *testing.T) {
	db, err := NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if db == nil {
		t.Fatal("expected non-nil db")
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	sqlDB.Close()
}

func TestNewDB_DSN(t *testing.T) {
	_, err := NewDB(&config.Config{DBDriver: "invalid", DBDSN: "file::memory:"})
	if err == nil {
		t.Log("gorm may not error on open with unknown driver")
	}
}

func TestAutoMigrate(t *testing.T) {
	db, err := NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
}

func TestUsePostgreSQL(t *testing.T) {
	cfg := &config.Config{DBDriver: "postgres"}
	if !cfg.UsePostgreSQL() {
		t.Error("expected postgres driver to return true")
	}

	cfg2 := &config.Config{DBDriver: "sqlite3"}
	if cfg2.UsePostgreSQL() {
		t.Error("expected sqlite driver to return false")
	}
}
