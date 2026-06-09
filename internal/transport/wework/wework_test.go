package wework

import (
	"context"
	"testing"

	transport "dolphin/internal/transport"
)

func TestWeWorkValOr(t *testing.T) {
	t.Run("returns value when present", func(t *testing.T) {
		cfg := map[string]any{"key": "value"}
		if got := valOr(cfg, "key", "default"); got != "value" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default when missing", func(t *testing.T) {
		cfg := map[string]any{}
		if got := valOr(cfg, "missing", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default when empty string", func(t *testing.T) {
		cfg := map[string]any{"key": ""}
		if got := valOr(cfg, "key", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("joins []any values", func(t *testing.T) {
		cfg := map[string]any{"key": []any{"a", "b"}}
		if got := valOr(cfg, "key", ""); got != "a,b" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default for non-string non-array", func(t *testing.T) {
		cfg := map[string]any{"key": 42}
		if got := valOr(cfg, "key", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})
}

func TestWeWorkID(t *testing.T) {
	w := &WeWork{id: "wework"}
	if w.ID() != "wework" {
		t.Errorf("ID = %q", w.ID())
	}
}

func TestWeWorkStart(t *testing.T) {
	w := &WeWork{}
	if err := w.Start(context.Background()); err != nil {
		t.Errorf("Start returned error: %v", err)
	}
}

func TestWeWorkContext(t *testing.T) {
	w := &WeWork{}
	if ctx := w.Context(); ctx == "" {
		t.Error("expected non-empty context string")
	}
}

func TestWeWorkToolsWithDirectStruct(t *testing.T) {
	t.Run("returns nil when BotID empty", func(t *testing.T) {
		w := &WeWork{cfg: WeWorkConfig{Secret: "secret"}}
		if tools := w.Tools(); tools != nil {
			t.Errorf("expected nil, got %d tools", len(tools))
		}
	})

	t.Run("returns nil when Secret empty", func(t *testing.T) {
		w := &WeWork{cfg: WeWorkConfig{BotID: "bot"}}
		if tools := w.Tools(); tools != nil {
			t.Errorf("expected nil, got %d tools", len(tools))
		}
	})
}

func TestWeWorkCapability(t *testing.T) {
	w := &WeWork{}
	c := w.Capability()
	if c.Interactive {
		t.Error("expected Interactive=false")
	}
	if c.Streamable {
		t.Error("expected Streamable=false")
	}
	if c.NestRead {
		t.Error("expected NestRead=false")
	}
}

func TestWeWorkRequestPermission(t *testing.T) {
	w := &WeWork{}
	result, err := w.RequestPermission(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if result != transport.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %d", result)
	}
}

func TestWeWorkFlush(t *testing.T) {
	w := &WeWork{}
	if err := w.Flush(); err != nil {
		t.Errorf("Flush returned error: %v", err)
	}
}

func TestWeWorkUserID(t *testing.T) {
	w := &WeWork{}
	// Initially empty
	if id := w.UserID(); id != "" {
		t.Errorf("expected empty, got %q", id)
	}
	// Set via internal field
	w.stateMu.Lock()
	w.lastUserID = "user001"
	w.stateMu.Unlock()
	if id := w.UserID(); id != "user001" {
		t.Errorf("expected 'user001', got %q", id)
	}
}

func TestWeWorkUserNick(t *testing.T) {
	w := &WeWork{}
	if nick := w.UserNick(); nick != "" {
		t.Errorf("expected empty, got %q", nick)
	}
}

func TestWeWorkIsSenderAllowed(t *testing.T) {
	t.Run("empty whitelist denies all", func(t *testing.T) {
		w := &WeWork{allowUsers: nil}
		if w.isSenderAllowed("anyone") {
			t.Error("expected false")
		}
	})

	t.Run("exact match", func(t *testing.T) {
		w := &WeWork{allowUsers: []string{"alice", "bob"}}
		if !w.isSenderAllowed("alice") {
			t.Error("expected true")
		}
	})

	t.Run("non-matching", func(t *testing.T) {
		w := &WeWork{allowUsers: []string{"alice"}}
		if w.isSenderAllowed("mallory") {
			t.Error("expected false")
		}
	})

	t.Run("glob match", func(t *testing.T) {
		w := &WeWork{allowUsers: []string{"*@example.com"}}
		if !w.isSenderAllowed("user@example.com") {
			t.Error("expected true")
		}
		if w.isSenderAllowed("user@other.com") {
			t.Error("expected false")
		}
	})
}

func TestWeWorkStripAtMention(t *testing.T) {
	t.Run("strips leading @mention", func(t *testing.T) {
		if got := stripAtMention("@bot /cmd", "bot"); got != "/cmd" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("handles no leading @", func(t *testing.T) {
		if got := stripAtMention("/cmd", "bot"); got != "/cmd" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("returns original when only @mention and no trailing space", func(t *testing.T) {
		if got := stripAtMention("@bot", "bot"); got != "@bot" {
			t.Errorf("got %q", got)
		}
	})
}

func TestWeWorkStripImageMarkdown(t *testing.T) {
	t.Run("removes image markdown", func(t *testing.T) {
		input := "Hello ![alt](http://example.com/img.png) world"
		expected := "Hello world"
		if got := stripImageMarkdown(input); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("removes image markdown with https", func(t *testing.T) {
		input := "![alt](https://example.com/img.png)"
		if got := stripImageMarkdown(input); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("removes relative image markdown", func(t *testing.T) {
		input := "![alt](image.png)"
		if got := stripImageMarkdown(input); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("preserves regular text", func(t *testing.T) {
		input := "Hello [link](http://example.com) world"
		expected := "Hello [link](http://example.com) world"
		if got := stripImageMarkdown(input); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("handles string without images", func(t *testing.T) {
		input := "just plain text"
		if got := stripImageMarkdown(input); got != input {
			t.Errorf("got %q, want %q", got, input)
		}
	})

	t.Run("handles empty string", func(t *testing.T) {
		if got := stripImageMarkdown(""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
