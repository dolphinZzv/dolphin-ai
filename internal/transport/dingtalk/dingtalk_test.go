package dingtalk

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/config"
)

func loadDingTalkConfig(t *testing.T) *config.DingTalkConfig {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Skipf("skip dingtalk integration test: config load failed: %v", err)
	}
	if !cfg.Transport.DingTalk.Enabled {
		t.Skip("skip dingtalk integration test: transport.dingtalk.enabled=false")
	}
	if cfg.Transport.DingTalk.ClientID == "" {
		t.Skip("skip dingtalk integration test: client_id not set")
	}
	return &cfg.Transport.DingTalk
}

func TestDingTalkOnConfigChangeCredentialChange(t *testing.T) {
	dt := &DingTalkTransport{
		cfg:         &config.DingTalkConfig{ClientID: "old_id", ClientSecret: "old_secret"},
		msgCh:       make(chan string, 1024),
		closeCh:     make(chan struct{}),
		reconnectCh: make(chan struct{}, 1),
	}

	oldCfg := &config.Config{}
	oldCfg.Transport.DingTalk = config.DingTalkConfig{ClientID: "old_id", ClientSecret: "old_secret"}

	newCfg := &config.Config{}
	newCfg.Transport.DingTalk = config.DingTalkConfig{ClientID: "new_id", ClientSecret: "new_secret"}

	dt.OnConfigChange(oldCfg, newCfg)

	// Verify the config pointer was updated
	if dt.cfg.ClientID != "new_id" {
		t.Errorf("cfg.ClientID = %q, want new_id", dt.cfg.ClientID)
	}
	if dt.cfg.ClientSecret != "new_secret" {
		t.Errorf("cfg.ClientSecret = %q, want new_secret", dt.cfg.ClientSecret)
	}

	// Verify reconnect signal was sent
	select {
	case <-dt.reconnectCh:
		// expected
	default:
		t.Error("expected reconnectCh signal after credential change")
	}
}

func TestDingTalkOnConfigChangeNoChange(t *testing.T) {
	dt := &DingTalkTransport{
		cfg:         &config.DingTalkConfig{ClientID: "same_id", ClientSecret: "same_secret"},
		msgCh:       make(chan string, 1024),
		closeCh:     make(chan struct{}),
		reconnectCh: make(chan struct{}, 1),
	}

	oldCfg := &config.Config{}
	oldCfg.Transport.DingTalk = config.DingTalkConfig{ClientID: "same_id", ClientSecret: "same_secret"}

	newCfg := &config.Config{}
	newCfg.Transport.DingTalk = config.DingTalkConfig{ClientID: "same_id", ClientSecret: "same_secret"}

	dt.OnConfigChange(oldCfg, newCfg)

	// Verify no reconnect signal was sent
	select {
	case <-dt.reconnectCh:
		t.Error("expected NO reconnectCh signal when credentials unchanged")
	default:
		// expected
	}
}

func TestDingTalkStreamConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping dingtalk integration test in short mode")
	}
	cfg := loadDingTalkConfig(t)
	dt := &DingTalkTransport{
		cfg:         cfg,
		msgCh:       make(chan string, 1024),
		closeCh:     make(chan struct{}),
		reconnectCh: make(chan struct{}, 1),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	go func() {
		if err := dt.Start(ctx); err != nil {
			t.Logf("Start ended: %v", err)
		}
	}()

	time.Sleep(3 * time.Second)

	t.Log("send a message to the bot in DingTalk within 120s...")
	msg, err := dt.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	t.Logf("received: %s", msg)

	reply := "Received: " + msg
	if err := dt.WriteLine(reply); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	t.Logf("replied: %s", reply)
}
