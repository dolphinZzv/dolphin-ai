package resource

import (
	"testing"
)

var testThresholds = []float64{20, 40, 60, 80}

func TestBracketIndex(t *testing.T) {
	tests := []struct {
		pct  float64
		want int
	}{
		{0, -1},
		{10, -1},
		{19.9, -1},
		{20, 0},
		{30, 0},
		{39.9, 0},
		{40, 1},
		{59.9, 1},
		{60, 2},
		{79.9, 2},
		{80, 3},
		{90, 3},
		{100, 3},
	}
	for _, tt := range tests {
		got := bracketIndex(tt.pct, testThresholds)
		if got != tt.want {
			t.Errorf("bracketIndex(%v, _) = %d, want %d", tt.pct, got, tt.want)
		}
	}
}

func TestBracketIndexCustomThresholds(t *testing.T) {
	custom := []float64{10, 50, 90}
	tests := []struct {
		pct  float64
		want int
	}{
		{0, -1},
		{5, -1},
		{10, 0},
		{30, 0},
		{50, 1},
		{75, 1},
		{90, 2},
		{95, 2},
		{100, 2},
	}
	for _, tt := range tests {
		got := bracketIndex(tt.pct, custom)
		if got != tt.want {
			t.Errorf("bracketIndex(%v, custom) = %d, want %d", tt.pct, got, tt.want)
		}
	}
}

func TestThresholdAt(t *testing.T) {
	tests := []struct {
		idx  int
		want float64
	}{
		{-1, 0},
		{0, 20},
		{1, 40},
		{2, 60},
		{3, 80},
		{4, 0},
		{10, 0},
	}
	for _, tt := range tests {
		got := thresholdAt(tt.idx, testThresholds)
		if got != tt.want {
			t.Errorf("thresholdAt(%d, _) = %v, want %v", tt.idx, got, tt.want)
		}
	}
}

func TestThresholdAtCustomThresholds(t *testing.T) {
	custom := []float64{25, 50, 75}
	if got := thresholdAt(-1, custom); got != 0 {
		t.Errorf("thresholdAt(-1, custom) = %v, want 0", got)
	}
	if got := thresholdAt(0, custom); got != 25 {
		t.Errorf("thresholdAt(0, custom) = %v, want 25", got)
	}
	if got := thresholdAt(2, custom); got != 75 {
		t.Errorf("thresholdAt(2, custom) = %v, want 75", got)
	}
	if got := thresholdAt(5, custom); got != 0 {
		t.Errorf("thresholdAt(5, custom) = %v, want 0", got)
	}
}

func TestResourceTypeToEventType(t *testing.T) {
	if TypeCPU.ToEventType() != "resource:cpu" {
		t.Errorf("TypeCPU.ToEventType() = %q", TypeCPU.ToEventType())
	}
	if TypeMemory.ToEventType() != "resource:memory" {
		t.Errorf("TypeMemory.ToEventType() = %q", TypeMemory.ToEventType())
	}
	if TypeDisk.ToEventType() != "resource:disk" {
		t.Errorf("TypeDisk.ToEventType() = %q", TypeDisk.ToEventType())
	}
	if TypeNetwork.ToEventType() != "resource:network" {
		t.Errorf("TypeNetwork.ToEventType() = %q", TypeNetwork.ToEventType())
	}
}
