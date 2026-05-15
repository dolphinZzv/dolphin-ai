package update

import (
	"context"
	"testing"
	"time"
)

func TestStartChecker_DevVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- StartChecker(ctx, CheckerConfig{
			Enabled:       true,
			CheckInterval: time.Hour,
			Channel:       "stable",
			AutoInstall:   false,
		}, "dev")
	}()

	// Cancel and ensure it exits quickly.
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("StartChecker with dev version should return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartChecker with dev version did not exit after cancel")
	}
}

func TestStartChecker_StopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Use a long interval; the checker should not call GitHub API.
	done := make(chan error, 1)
	go func() {
		done <- StartChecker(ctx, CheckerConfig{
			Enabled:       true,
			CheckInterval: 24 * time.Hour,
			Channel:       "stable",
			AutoInstall:   false,
		}, "dev")
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartChecker did not exit after cancel")
	}
}

func TestCheckerConfig_Defaults(t *testing.T) {
	cfg := CheckerConfig{
		Enabled:       true,
		CheckInterval: 24 * time.Hour,
		Channel:       "stable",
		AutoInstall:   false,
	}

	if !cfg.Enabled {
		t.Error("expected enabled=true")
	}
	if cfg.CheckInterval != 24*time.Hour {
		t.Error("expected 24h interval")
	}
	if cfg.Channel != "stable" {
		t.Error("expected stable channel")
	}
	if cfg.AutoInstall {
		t.Error("expected auto_install=false")
	}
}
