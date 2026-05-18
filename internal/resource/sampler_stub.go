//go:build !linux

package resource

import "fmt"

// stubSampler provides a fallback on non-Linux platforms.
type stubSampler struct{}

// NewSampler creates a stub sampler for non-Linux platforms.
func NewSampler() Sampler {
	return &stubSampler{}
}

func (s *stubSampler) CPU() (float64, error) {
	return 0, fmt.Errorf("resource monitor: /proc not available on this platform")
}

func (s *stubSampler) Memory() (float64, error) {
	return 0, fmt.Errorf("resource monitor: /proc not available on this platform")
}

func (s *stubSampler) Disk(path string) (float64, error) {
	return 0, fmt.Errorf("resource monitor: /proc not available on this platform")
}

func (s *stubSampler) Network() (float64, float64, error) {
	return 0, 0, fmt.Errorf("resource monitor: /proc not available on this platform")
}

func (s *stubSampler) Close() {}
