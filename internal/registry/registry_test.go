package registry

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRegisterAndGet(t *testing.T) {
	r := New()
	cmd := &cobra.Command{Use: "test", Short: "a test command"}
	spec := &CommandSpec{Cobra: cmd, Category: CatSystem}
	r.Register(spec)

	got := r.Get("test")
	if got == nil {
		t.Fatal("expected spec, got nil")
	}
	if got.Cobra != cmd {
		t.Error("returned wrong cobra command")
	}
	if got.Category != CatSystem {
		t.Error("returned wrong category")
	}
}

func TestRegisterNil(t *testing.T) {
	r := New()
	r.Register(nil)            // should not panic
	r.Register(&CommandSpec{}) // nil Cobra should not panic
}

func TestGetUnknown(t *testing.T) {
	r := New()
	if got := r.Get("nonexistent"); got != nil {
		t.Fatal("expected nil for unknown command")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r := New()
	r.Register(&CommandSpec{Cobra: &cobra.Command{Use: "dup"}})
	r.Register(&CommandSpec{Cobra: &cobra.Command{Use: "dup"}})
}

func TestList(t *testing.T) {
	r := New()
	r.Register(&CommandSpec{Cobra: &cobra.Command{Use: "beta"}, Category: CatSystem})
	r.Register(&CommandSpec{Cobra: &cobra.Command{Use: "alpha"}, Category: CatConfig})

	all := r.List()
	if len(all) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(all))
	}
	// Should preserve insertion order.
	if all[0].Cobra.Name() != "beta" {
		t.Errorf("expected first beta, got %s", all[0].Cobra.Name())
	}
	if all[1].Cobra.Name() != "alpha" {
		t.Errorf("expected second alpha, got %s", all[1].Cobra.Name())
	}
}

func TestListByCategory(t *testing.T) {
	r := New()
	r.Register(&CommandSpec{Cobra: &cobra.Command{Use: "sys"}, Category: CatSystem})
	r.Register(&CommandSpec{Cobra: &cobra.Command{Use: "cfg"}, Category: CatConfig})
	r.Register(&CommandSpec{Cobra: &cobra.Command{Use: "sys2"}, Category: CatSystem})

	byCat := r.ListByCategory()
	if len(byCat[CatSystem]) != 2 {
		t.Errorf("expected 2 system commands, got %d", len(byCat[CatSystem]))
	}
	if len(byCat[CatConfig]) != 1 {
		t.Errorf("expected 1 config command, got %d", len(byCat[CatConfig]))
	}
}
