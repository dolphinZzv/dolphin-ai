package resource

// Sampler collects system resource usage statistics.
type Sampler interface {
	// CPU returns the CPU usage as a percentage (0-100).
	CPU() (float64, error)

	// Memory returns the memory usage as a percentage (0-100).
	Memory() (float64, error)

	// Disk returns the disk usage percentage (0-100) for the given path.
	Disk(path string) (float64, error)

	// Network returns the current receive and transmit bytes/sec rates.
	Network() (rxRate, txRate float64, err error)

	// Close releases any resources held by the sampler.
	Close()
}
